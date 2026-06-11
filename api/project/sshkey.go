package project

import (
	"fmt"
	"net/url"
)

// SSHKeyMeta is the public metadata for one additional SSH key on a project.
// The private key is NEVER returned by the CircleCI API; it is intentionally
// excluded from this struct (and from the manifest).
//
// The field names match the JSON tags of the v1.1 settings ssh_keys array so
// that the struct can also be used as the JSON decode target directly (the
// struct tags align on-wire → in-memory without a separate intermediate type).
type SSHKeyMeta struct {
	// Hostname is the target host this key is scoped to (may be empty for
	// global additional SSH keys).
	Hostname string `json:"hostname"`
	// PublicKey is the SSH public-key material (e.g. "ssh-rsa AAAA... user@host").
	PublicKey string `json:"public_key"`
	// Fingerprint is the SHA256 fingerprint of the key without the "SHA256:"
	// prefix (e.g. "Cv1Bb...=").
	Fingerprint string `json:"fingerprint"`
}

// v11SSHKeySettings mirrors the ssh_keys portion of the
// GET /api/v1.1/project/{slug}/settings?ssh-key-digest=sha256 response.
type v11SSHKeySettings struct {
	SSHKeys []SSHKeyMeta `json:"ssh_keys"`
}

// ListAdditionalSSHKeys returns the public metadata for every additional SSH
// key configured on a project. Private key material is never returned by the
// API and is therefore never present in the result.
//
// Endpoint: GET /api/v1.1/project/{slug}/settings?ssh-key-digest=sha256
//
// The slug format follows the same conventions as other v1.1 calls:
//   - GitHub OAuth: "gh/<org>/<repo>"
//   - Standalone/App: "circleci/<org-uuid>/<proj-uuid>"
//
// On a 404 or other API error the caller should treat the result as
// non-fatal and record a manifest warning rather than aborting the export.
func (c *Client) ListAdditionalSSHKeys(slug string) ([]SSHKeyMeta, error) {
	u, err := slugSubresource(slug, "settings")
	if err != nil {
		return nil, fmt.Errorf("ListAdditionalSSHKeys: %w", err)
	}

	// Request SHA256-format fingerprints so they are consistent with the
	// fingerprint format used by the rest of the export.
	q := url.Values{}
	q.Set("ssh-key-digest", "sha256")
	u.RawQuery = q.Encode()

	req, err := c.v11.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("ListAdditionalSSHKeys: build request: %w", err)
	}

	var raw v11SSHKeySettings
	if _, err := c.v11.DoRequest(req, &raw); err != nil {
		return nil, fmt.Errorf("ListAdditionalSSHKeys %q: %w", slug, err)
	}

	if len(raw.SSHKeys) == 0 {
		return nil, nil
	}
	return raw.SSHKeys, nil
}
