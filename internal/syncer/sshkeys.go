package syncer

import (
	"context"
	"fmt"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// syncProjectSSHKeys re-adds additional SSH keys from the secret bundle to
// the destination project.
//
// The flow for each key in the manifest's SSHKeys list:
//
//  1. If the manifest has SSHKeys metadata but the bundle has no entry with a
//     matching fingerprint, emit a "manual" warning — the private key was not
//     captured and must be added by hand.
//
//  2. Otherwise, check whether the key is already present on the destination
//     project (idempotency check via ListAdditionalSSHKeys). If the
//     fingerprint already exists, emit "exists" and skip.
//
//  3. In dry-run mode (opts.Apply=false), record "would add" without calling
//     the API.
//
//  4. In apply mode, call AddAdditionalSSHKey with the hostname and private
//     key from the bundle.
func (s *Syncer) syncProjectSSHKeys(ctx context.Context, report *Report, p manifest.Project, bundle *manifest.SecretBundle, dst string, opts Options) {
	if len(p.SSHKeys) == 0 {
		return
	}

	// Build an index of captured private keys keyed by fingerprint.
	bundleByFingerprint := map[string]manifest.BundleSSHKey{}
	if bundle != nil {
		// Bundle is keyed by the SOURCE slug.
		for _, k := range bundle.SSHKeys[p.Slug] {
			bundleByFingerprint[k.Fingerprint] = k
		}
	}

	// Fetch existing keys on the DESTINATION so we can skip already-present ones.
	existingFingerprints := map[string]bool{}
	if opts.Apply {
		if existing, err := s.Projects.ListAdditionalSSHKeys(ctx, dst); err == nil {
			for _, k := range existing {
				existingFingerprints[k.Fingerprint] = true
			}
		}
	}

	for _, sk := range p.SSHKeys {
		target := dst + "/ssh-key:" + sk.Fingerprint

		bundleKey, captured := bundleByFingerprint[sk.Fingerprint]
		if !captured {
			// Private key not in the bundle — operator must add it manually.
			report.add("project-ssh-key", target, "manual",
				fmt.Sprintf("SSH key %q (hostname=%q) private key not captured — "+
					"add manually or run ssh-key extraction first", sk.Fingerprint, sk.Hostname))
			continue
		}

		if existingFingerprints[sk.Fingerprint] {
			report.add("project-ssh-key", target, "exists",
				fmt.Sprintf("SSH key %q already present on destination project", sk.Fingerprint))
			continue
		}

		if !opts.Apply {
			report.add("project-ssh-key", target, "set",
				fmt.Sprintf("would add SSH key %q (hostname=%q)", sk.Fingerprint, bundleKey.Hostname))
			continue
		}

		if err := s.Projects.AddAdditionalSSHKey(ctx, dst, bundleKey.Hostname, bundleKey.PrivateKey); err != nil {
			report.add("project-ssh-key", target, "error",
				fmt.Sprintf("add SSH key %q: %v", sk.Fingerprint, err))
			continue
		}
		report.add("project-ssh-key", target, "set",
			fmt.Sprintf("added SSH key %q (hostname=%q)", sk.Fingerprint, bundleKey.Hostname))
	}
}
