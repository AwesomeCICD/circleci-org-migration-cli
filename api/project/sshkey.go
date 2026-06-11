package project

import (
	"fmt"
)

// SSHKey represents one additional (user-added) SSH key on a project as
// returned by the v1.1 list endpoint.
//
// These differ from checkout/deploy keys: they are used to authenticate with
// external services (e.g. private git submodules, package repos) during builds
// and are managed separately via Project Settings → SSH Keys in the UI.
//
// JSON field names are the v1.1 API wire names (snake_case, no hyphens).
type SSHKey struct {
	// Hostname is the hostname this key is scoped to (e.g. "github.com").
	// Empty string means the key is not scoped to a specific host.
	Hostname string `json:"hostname"`
	// Fingerprint is the SHA256 fingerprint of the key in the format
	// "SHA256:…" as returned by the v1.1 API.
	Fingerprint string `json:"fingerprint"`
}

// addSSHKeyRequest is the wire format for
// POST /api/v1.1/project/{slug}/ssh-key.
//
// JSON shape confirmed from the CircleCI v1.1 API:
//
//	{"hostname": "github.com", "private_key": "-----BEGIN..."}
type addSSHKeyRequest struct {
	Hostname   string `json:"hostname"`
	PrivateKey string `json:"private_key"`
}

// ListAdditionalSSHKeys returns the additional (user-added) SSH keys for the
// given project slug.
//
// Endpoint: GET /api/v1.1/project/{slug}/ssh-key
//
// The slug is encoded using the same slugSubresource convention as other v1.1
// calls (each component percent-encoded individually; literal '/' separators
// preserved).
func (c *Client) ListAdditionalSSHKeys(slug string) ([]SSHKey, error) {
	if slug == "" {
		return nil, fmt.Errorf("project: ListAdditionalSSHKeys requires slug")
	}

	u, err := slugSubresource(slug, "ssh-key")
	if err != nil {
		return nil, fmt.Errorf("project: ListAdditionalSSHKeys: %w", err)
	}

	req, err := c.v11.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("project: ListAdditionalSSHKeys: build request: %w", err)
	}

	var keys []SSHKey
	if _, err := c.v11.DoRequest(req, &keys); err != nil {
		return nil, fmt.Errorf("project: ListAdditionalSSHKeys %q: %w", slug, err)
	}
	return keys, nil
}

// AddAdditionalSSHKey adds an additional SSH key to the given project.
//
// Endpoint: POST /api/v1.1/project/{slug}/ssh-key
// Request body: {"hostname": "<hostname>", "private_key": "<pem-key>"}
//
// The hostname may be empty (the API accepts it), which creates a key not
// scoped to any particular host. The privateKey must be the plaintext
// PEM-encoded private key.
//
// The slug is encoded using the same slugSubresource convention as other v1.1
// calls. For standalone/App orgs the slug is
// "circleci/<orgUUID>/<projectUUID>"; for OAuth orgs it is "gh/<org>/<repo>".
//
// A non-2xx response is returned as an error with the HTTP status included.
func (c *Client) AddAdditionalSSHKey(slug, hostname, privateKey string) error {
	if slug == "" {
		return fmt.Errorf("project: AddAdditionalSSHKey requires slug")
	}
	if privateKey == "" {
		return fmt.Errorf("project: AddAdditionalSSHKey requires privateKey")
	}

	u, err := slugSubresource(slug, "ssh-key")
	if err != nil {
		return fmt.Errorf("project: AddAdditionalSSHKey: %w", err)
	}

	body := addSSHKeyRequest{
		Hostname:   hostname,
		PrivateKey: privateKey,
	}
	req, err := c.v11.NewRequest("POST", u, &body)
	if err != nil {
		return fmt.Errorf("project: AddAdditionalSSHKey: build request: %w", err)
	}

	// The v1.1 POST /ssh-key endpoint returns 201 with an empty body on success.
	// Pass nil to discard the response body without trying to decode it.
	if _, err := c.v11.DoRequest(req, nil); err != nil {
		return fmt.Errorf("project: AddAdditionalSSHKey %q: %w", slug, err)
	}
	return nil
}
