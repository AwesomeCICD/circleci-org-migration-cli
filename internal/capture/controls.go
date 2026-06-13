package capture

import (
	"context"
	"fmt"
	"io"
	"strings"

	apicontext "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// prepareRestrictionRemoval fetches live restriction IDs for mc, deletes only
// the project and expression restrictions (skipping ALL group restrictions),
// and returns a restore function that re-creates the same project/expression
// restrictions from the manifest.
//
// Org-type restriction matrix (CircleCI API v2):
//
//	restriction_type | GitHub OAuth ("gh/…") | Standalone ("circleci/…") | Bitbucket
//	-----------------+----------------------+---------------------------+----------
//	project          | supported            | supported                 | supported
//	expression       | supported            | supported                 | supported
//	group            | supported            | NOT SUPPORTED             | NOT SUPPORTED
//
// Group restrictions are managed by CircleCI / VCS and are tied to VCS team
// IDs that are org-specific. Attempting to create a group restriction on a
// non-OAuth org fails with "This is only supported for OAuth orgs." To avoid
// permanently breaking context access on any org type, this function NEVER
// removes or recreates group restrictions — not even on GitHub OAuth orgs.
// The default "All members" group (type=="group", value==orgID) must especially
// never be deleted. Any non-default group restrictions in the manifest are
// surfaced to the operator as a manual follow-up notice.
//
// The caller MUST immediately defer the returned restore function. It runs
// even if extraction later fails or panics.
//
// If any DELETE fails, the error is returned and no restore func is registered
// (nothing was removed yet, nothing needs restoring).
// If a RESTORE fails, a prominent WARNING is printed naming exactly which
// restriction (context, type, value) must be manually re-added.
func prepareRestrictionRemoval(ctx context.Context, stderr io.Writer, client ContextRestrictionManager, mc *manifest.Context, orgID string) (restoreFn func(), err error) {
	// Fetch live restrictions to get their IDs for deletion.
	live, listErr := client.ListRestrictions(ctx, mc.SourceID)
	if listErr != nil {
		return func() {}, fmt.Errorf("listing live restrictions for context %q: %w", mc.Name, listErr)
	}

	// Filter live restrictions: only touch project and expression types.
	// ALL group restrictions (including the default "All members" group) are
	// left completely untouched. Group restrictions are org-type-specific:
	// they can only be created via the API on GitHub OAuth orgs, not on
	// standalone or Bitbucket orgs. We never delete a group restriction
	// because we might not be able to recreate it.
	var liveToDelete []apicontext.Restriction
	for _, lr := range live {
		if lr.Type == "group" {
			// Group restriction — managed by CircleCI/VCS; never modified by capture.
			fmt.Fprintf(stderr,
				"NOTICE: group restriction on context %q (value=%q) is managed by CircleCI/VCS and is not modified.\n",
				mc.Name, lr.Value,
			)
			continue
		}
		liveToDelete = append(liveToDelete, lr)
	}

	// The restore set comes from the manifest's recorded restrictions, filtered
	// to only project and expression types. Group restrictions are never
	// re-created — they must be managed manually on the destination.
	var restoreFrom []manifest.Restriction
	for _, r := range mc.Restrictions {
		if isGroupRestriction(r) {
			// Skip group restrictions: not removable/recreatable via API on all org types.
			continue
		}
		if isDefaultAllMembersGroup(r, orgID) {
			// Belt-and-suspenders: skip the All-members default explicitly.
			continue
		}
		restoreFrom = append(restoreFrom, r)
	}

	fmt.Fprintf(stderr,
		"NOTICE: temporarily removing %d project/expression restriction(s) from context %q for extraction.\n",
		len(liveToDelete), mc.Name,
	)
	for _, lr := range liveToDelete {
		if delErr := client.DeleteRestriction(ctx, mc.SourceID, lr.ID); delErr != nil {
			return func() {}, fmt.Errorf("deleting restriction %q from context %q: %w", lr.ID, mc.Name, delErr)
		}
	}

	// Build the restore closure. Re-creates only project/expression restrictions
	// from the manifest. Group restrictions are never touched.
	restore := func() {
		fmt.Fprintf(stderr,
			"NOTICE: restoring %d project/expression restriction(s) on context %q.\n",
			len(restoreFrom), mc.Name,
		)
		for _, r := range restoreFrom {
			if createErr := client.CreateRestriction(ctx, mc.SourceID, r.Type, r.Value); createErr != nil {
				fmt.Fprintf(stderr,
					"WARNING: failed to restore restriction on context %q "+
						"(type=%q value=%q): %v — you must re-add this restriction manually.\n",
					mc.Name, r.Type, r.Value, createErr,
				)
			}
		}
	}
	return restore, nil
}

// OrgTriggerAlreadyEnabled reports whether any of the known key shapes for
// allow_api_trigger_with_config is present and true in the feature-flag map.
// It normalises keys by stripping a trailing "?" (standalone API quirk).
//
// Exported so cmd-layer callers can perform a pre-flight read without going
// through MaybeEnableOrgTriggerFlag (which unconditionally enables the flag).
func OrgTriggerAlreadyEnabled(flags map[string]bool) bool {
	return orgTriggerAlreadyEnabled(flags)
}

// orgTriggerAlreadyEnabled is the unexported implementation; internal callers
// use this to avoid the extra indirection.
func orgTriggerAlreadyEnabled(flags map[string]bool) bool {
	for k, v := range flags {
		k = strings.TrimSuffix(k, "?")
		if (k == OrgAPITriggerKey || k == orgAPITriggerKeyStandalone) && v {
			return true
		}
	}
	return false
}

// MaybeEnableOrgTriggerFlag reads the org-level allow_api_trigger_with_config
// flag. If it is off, it enables it and returns a restore func that must be
// called (typically via defer) to set it back to false. If it was already on,
// the restore func is a no-op. On read failure the error is treated as
// best-effort: a WARNING is printed and a no-op restore func is returned so
// the caller can proceed; the per-project flag is the primary gate.
func MaybeEnableOrgTriggerFlag(ctx context.Context, stderr io.Writer, mgr OrgFlagManager, vcsType, orgName string) func() {
	flags, err := mgr.GetFeatureFlags(ctx, vcsType, orgName)
	if err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not read org-level feature flags (%s/%s): %v — proceeding\n",
			vcsType, orgName, err)
		return func() {} // no-op restore
	}

	// Bug 6: tolerate both key shapes (OAuth and standalone) and normalise away
	// the trailing "?" that the standalone endpoint sometimes appends.
	if orgTriggerAlreadyEnabled(flags) {
		clog.Infof("org-level allow_api_trigger_with_config already enabled for %s/%s — skipping enable step", vcsType, orgName)
		return func() {} // already on, nothing to restore
	}

	fmt.Fprintf(stderr, "Enabling org-level allow_api_trigger_with_config for %s/%s…\n", vcsType, orgName)
	if uerr := mgr.UpdateFeatureFlags(ctx, vcsType, orgName, map[string]bool{OrgAPITriggerKey: true}); uerr != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not enable org-level allow_api_trigger_with_config for %s/%s: %v — proceeding\n",
			vcsType, orgName, uerr)
		return func() {} // failed to enable, nothing to restore
	}

	// Return a restore func that the caller must defer.
	return func() {
		fmt.Fprintf(stderr, "Restoring org-level allow_api_trigger_with_config=false for %s/%s…\n", vcsType, orgName)
		if rerr := mgr.UpdateFeatureFlags(ctx, vcsType, orgName, map[string]bool{OrgAPITriggerKey: false}); rerr != nil {
			fmt.Fprintf(stderr,
				"WARNING: failed to restore org-level allow_api_trigger_with_config for %s/%s: %v\n",
				vcsType, orgName, rerr)
		}
	}
}

// ApplyArtifactRetentionControl reads the current org storage-retention controls,
// then sets artifact retention to targetDays (keeping cache/workspace unchanged).
//
// The prior artifact-retention value is logged via clog so the operator knows
// what value to restore if needed. This function deliberately does NOT
// auto-restore: keeping artifact retention low is the safe default when
// secrets may be present in pipeline artifacts (there is no delete-artifact API).
//
// A clear note is printed to stderr with the prior value and restore instructions.
func ApplyArtifactRetentionControl(ctx context.Context, stderr io.Writer, mgr StorageRetentionManager, orgUUID string, targetDays int) {
	current, err := mgr.GetStorageRetention(ctx, orgUUID)
	if err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not read current artifact-retention for org %s: %v — skipping retention control\n",
			orgUUID, err)
		clog.Warnf("ApplyArtifactRetentionControl: GetStorageRetention(%s): %v", orgUUID, err)
		return
	}

	priorDays := current.Controls.ArtifactDays
	clog.Infof("artifact-retention safety: current artifact_days=%d, setting to %d for org %s",
		priorDays, targetDays, orgUUID)

	newControls := org.StorageRetentionControls{
		CacheDays:     current.Controls.CacheDays,
		WorkspaceDays: current.Controls.WorkspaceDays,
		ArtifactDays:  targetDays,
	}
	if err := mgr.SetStorageRetention(ctx, orgUUID, newControls); err != nil {
		fmt.Fprintf(stderr,
			"WARNING: could not set artifact-retention to %d days for org %s: %v — skipping retention control\n",
			targetDays, orgUUID, err)
		clog.Warnf("ApplyArtifactRetentionControl: SetStorageRetention(%s): %v", orgUUID, err)
		return
	}

	clog.Infof("artifact-retention set to %d day(s) for org %s (was %d)", targetDays, orgUUID, priorDays)
	fmt.Fprintf(stderr,
		"NOTICE: artifact-retention set to %d day(s) for org %s (was %d day(s)).\n"+
			"  Secrets landing in build artifacts will expire sooner.\n"+
			"  This value is NOT auto-restored. To restore, run:\n"+
			"    POST https://app.circleci.com/private/orgs/%s/storage-retention-controls\n"+
			"    body: {\"retention_days_artifact\":%d,\"retention_days_cache\":%d,\"retention_days_workspace\":%d}\n",
		targetDays, orgUUID, priorDays,
		orgUUID, priorDays, current.Controls.CacheDays, current.Controls.WorkspaceDays,
	)
}
