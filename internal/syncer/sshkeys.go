package syncer

import (
	"fmt"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// syncProjectSSHKeys re-adds additional (user-added) SSH keys to the
// destination project from the secret bundle.
//
// Idempotency: the destination project's existing SSH keys are listed first;
// any key whose fingerprint already appears is skipped (status "exists").
//
// Missing private key: if the manifest records an SSH key (via SSHKeys) but
// no matching BundleSSHKey is present in the bundle for that fingerprint, a
// "manual" warning is emitted so the operator knows to add the key by hand or
// re-run extraction.
//
// Dry-run: when opts.Apply is false, planned additions are recorded without
// making any API calls.
//
// The p.Slug (source slug) is used as the bundle lookup key (same convention
// as project env vars). The dst slug is used for the destination API calls.
func (s *Syncer) syncProjectSSHKeys(report *Report, p manifest.Project, bundle *manifest.SecretBundle, dst string, opts Options) {
	if len(p.SSHKeys) == 0 {
		return
	}

	// Build bundle lookup: fingerprint → BundleSSHKey.
	bundleByFP := map[string]manifest.BundleSSHKey{}
	if bundle != nil {
		for _, bk := range bundle.SSHKeys[p.Slug] {
			bundleByFP[bk.Fingerprint] = bk
		}
	}

	// Fetch existing destination keys for idempotency (best-effort: if listing
	// fails we still attempt creation and let the API decide).
	existingFPs := map[string]bool{}
	if opts.Apply {
		if destKeys, err := s.Projects.ListAdditionalSSHKeys(dst); err == nil {
			for _, dk := range destKeys {
				existingFPs[dk.Fingerprint] = true
			}
		}
	}

	for _, key := range p.SSHKeys {
		target := dst + "/ssh-key:" + key.Fingerprint

		// Idempotency: skip if already present on destination.
		if existingFPs[key.Fingerprint] {
			report.add("project-ssh-key", target, "exists",
				fmt.Sprintf("SSH key %s already present on destination", key.Fingerprint))
			continue
		}

		// Look up the private key in the bundle.
		bk, hasBundleKey := bundleByFP[key.Fingerprint]
		if !hasBundleKey {
			report.add("project-ssh-key", target, "manual",
				fmt.Sprintf("SSH key %s: private key not captured — add manually or run ssh-key extraction (Part 3)", key.Fingerprint))
			continue
		}

		if !opts.Apply {
			report.add("project-ssh-key", target, "set",
				fmt.Sprintf("would add SSH key %s (hostname=%q)", key.Fingerprint, bk.Hostname))
			continue
		}

		if err := s.Projects.AddAdditionalSSHKey(dst, bk.Hostname, bk.PrivateKey); err != nil {
			report.add("project-ssh-key", target, "error",
				fmt.Sprintf("add SSH key %s: %v", key.Fingerprint, err))
			continue
		}
		report.add("project-ssh-key", target, "set",
			fmt.Sprintf("added SSH key %s (hostname=%q)", key.Fingerprint, bk.Hostname))
	}
}
