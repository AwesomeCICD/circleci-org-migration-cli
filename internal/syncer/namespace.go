package syncer

import (
	"context"
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	apirunner "github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
)

// Same-namespace handoff
//
// An orb namespace belongs to exactly ONE org. When a migration keeps the same
// namespace name on both sides (the common cross-type case, e.g. "bcbsm"), the
// namespace cannot be recreated or republished in the destination: it can only
// be MOVED by CircleCI support running the staff command
//
//	change-namespace-owner! "<ns>" "<dest-org-slug>"
//
// (owner slug accepts gh/<name>, github/<name>, circleci/<short-id>); the orbs
// move with it and config references need no change. `rename-namespace` must
// not be used (it does not free the old name).
//
// Runner resource classes are named "<namespace>/<class>" but each class row
// stores its OWN org attribution, set at creation. It does NOT follow a
// namespace transfer, so after the handoff the classes are still attributed to
// the source org; the destination cannot create a same-named class (HTTP 409)
// until the source copy is deleted. The syncer therefore never guesses: it
// reports a manual step in every ambiguous case.

// nsOwner is who owns a namespace relative to the migration.
type nsOwner int

const (
	nsOwnerUnknown nsOwner = iota // could not be determined
	nsOwnerSource                 // still owned by the source org
	nsOwnerDest                   // owned by the destination org (transfer done, or same org)
)

// orgOrbLister lists the orbs whose namespace is owned by an org. The org client
// implements it; it is asserted optionally so test fakes need not.
type orgOrbLister interface {
	GetOrgOrbs(ctx context.Context, orgUUID string) ([]org.OrgOrb, error)
}

// orgRunnerClassLister lists the runner resource classes attributed to an org.
// The runner client implements it; asserted optionally.
type orgRunnerClassLister interface {
	ListResourceClassesByOrg(ctx context.Context, orgID string) ([]apirunner.OrgResourceClass, error)
}

// destOrgIDFor resolves the destination org UUID from opts.DestOrgSlug. It
// returns "" when no destination slug was supplied or resolution fails.
func (s *Syncer) destOrgIDFor(ctx context.Context, opts Options) string {
	if opts.DestOrgSlug == "" || s.Org == nil {
		return ""
	}
	id, err := s.Org.ResolveOrgID(ctx, opts.DestOrgSlug)
	if err != nil {
		clog.Debugf("could not resolve destination org %q for namespace ownership: %v", opts.DestOrgSlug, err)
		return ""
	}
	return id
}

// detectNamespaceOwner determines which org owns namespace ns. The only
// reliable signal is the org-scoped orb list (GET /api/private/orb?org-id=),
// which returns the orbs whose namespace is OWNED by that org, each carrying its
// namespace_name. (Runner class attribution is deliberately NOT used: it does
// not follow a namespace transfer.) A namespace with no orbs therefore reports
// nsOwnerUnknown.
func (s *Syncer) detectNamespaceOwner(ctx context.Context, ns, srcOrgID, destOrgID string) nsOwner {
	if srcOrgID != "" && srcOrgID == destOrgID {
		return nsOwnerDest // same org: nothing to hand off
	}
	lister, ok := s.Org.(orgOrbLister)
	if !ok {
		return nsOwnerUnknown
	}
	owns := func(orgID string) bool {
		if orgID == "" {
			return false
		}
		orbs, err := lister.GetOrgOrbs(ctx, orgID)
		if err != nil {
			clog.Debugf("could not list orbs for org %s (namespace ownership): %v", orgID, err)
			return false
		}
		for _, o := range orbs {
			if o.NamespaceName == ns {
				return true
			}
		}
		return false
	}
	// Check the destination first: once the transfer is done it is authoritative.
	if owns(destOrgID) {
		return nsOwnerDest
	}
	if owns(srcOrgID) {
		return nsOwnerSource
	}
	return nsOwnerUnknown
}

// handoffCommand is the exact staff command (as a support-ticket line) that
// moves ns to the destination org.
func handoffCommand(ns string, opts Options) string {
	dest := opts.DestOrgSlug
	if dest == "" {
		dest = "<dest-org-slug>"
	}
	return fmt.Sprintf("change-namespace-owner! %q %q", ns, dest)
}

// namespaceHandoffDetail is the shared "source still owns it" explanation.
func namespaceHandoffDetail(kind, ns string, opts Options) string {
	moves := "the orbs move with the namespace"
	if kind == "runner" {
		moves = "runner resource classes do NOT move with it (see the class notes)"
	}
	return fmt.Sprintf(
		"namespace %q is still owned by the SOURCE org and a namespace can belong to only one org, so it was NOT recreated/republished. "+
			"Ask CircleCI support to run `%s` (never `rename-namespace`, which does not free the old name); %s. "+
			"Because the namespace name is unchanged, every config reference (`%s/...`) needs no change.",
		ns, handoffCommand(ns, opts), moves, ns)
}

// isNamespaceNotOwnedErr reports whether err looks like the runner API refusing
// an operation because the caller's org does not own the namespace.
func isNamespaceNotOwnedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "403") || strings.Contains(msg, "404") ||
		strings.Contains(msg, "forbidden") || strings.Contains(msg, "not found") ||
		strings.Contains(msg, "unauthorized")
}
