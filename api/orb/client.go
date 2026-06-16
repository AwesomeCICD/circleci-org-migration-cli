// Package orb provides a client for the CircleCI orb API (v3), which lives on
// app.circleci.com under the /api/v3/ path prefix. Auth uses the
// "Authorization: Bearer <token>" header rather than the Circle-Token header
// used by the main v2 API.
package orb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

const (
	// OrbHost is the base URL for the CircleCI orb API.
	OrbHost = "https://app.circleci.com"
	// orbAPIBase is the path prefix for the v3 API.
	orbAPIBase = "api/v3/"
)

// OrbPackage represents one orb registered under a namespace.
type OrbPackage struct {
	// ID is the UUID assigned by CircleCI to this orb package.
	ID string
	// Name is the short orb name (e.g. "my-orb", without the namespace prefix).
	Name string
	// IsPrivate reports whether the orb is private (visible only to the owning org).
	IsPrivate bool
	// IsListed reports whether the orb is listed in the public orb registry.
	IsListed bool
	// NamespaceID is the UUID of the owning namespace.
	NamespaceID string
}

// OrbVersion represents one published (stable/released) version of an orb.
type OrbVersion struct {
	// ID is the UUID assigned by CircleCI to this version record.
	ID string
	// Version is the semver string (e.g. "1.2.3").
	Version string
}

// Client is a CircleCI orb API v3 client.
type Client struct {
	rest *rest.Client
}

// NewClient constructs a Client using the default orb API host (app.circleci.com).
// cfg provides the HTTP client (timeout, transport); token is the API token.
func NewClient(cfg *settings.Config, token string) (*Client, error) {
	return NewClientWithBase(OrbHost, token, cfg)
}

// NewClientWithBase constructs a Client pointed at baseHost instead of the
// default app.circleci.com. It is exported so that tests can substitute an
// httptest.Server URL. cfg supplies the HTTP client.
func NewClientWithBase(baseHost, token string, cfg *settings.Config) (*Client, error) {
	var httpClient *http.Client
	if cfg != nil {
		httpClient = cfg.HTTPClient
	}
	return newClientWithHTTP(baseHost, token, httpClient)
}

// newClientWithHTTP is the low-level constructor used internally.
func newClientWithHTTP(baseHost, token string, httpClient *http.Client) (*Client, error) {
	base, err := url.Parse(baseHost)
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("orb: invalid host URL %q: %w", baseHost, err)
	}
	apiBase, err := base.Parse(orbAPIBase)
	if err != nil {
		return nil, fmt.Errorf("orb: building API base URL: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{rest: rest.NewBearer(apiBase, token, httpClient)}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// JSON envelope types (internal; not exported)
// ─────────────────────────────────────────────────────────────────────────────

// namespaceResponse is the shape returned by GET /api/v3/namespaces?filter[name]=<name>.
type namespaceResponse struct {
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			Name string `json:"name"`
		} `json:"attributes"`
	} `json:"data"`
}

// orbPackageItem is one item in the orb list response.
type orbPackageItem struct {
	ID         string `json:"id"`
	Attributes struct {
		Name      string `json:"name"`
		IsPrivate bool   `json:"is_private"`
		IsListed  bool   `json:"is_listed"`
	} `json:"attributes"`
	References struct {
		Namespace struct {
			ID string `json:"id"`
		} `json:"namespace"`
	} `json:"references"`
}

// orbPackageListResponse is the paginated response for listing orb packages.
type orbPackageListResponse struct {
	Data      []orbPackageItem `json:"data"`
	Next      string           `json:"next"`            // cursor for next page (JSON:API style)
	NextToken string           `json:"next_page_token"` // alternative cursor key
}

// orbVersionItem is one item in the version list response.
type orbVersionItem struct {
	ID         string `json:"id"`
	Attributes struct {
		Version   string `json:"version"`
		CreatedAt string `json:"created_at"`
	} `json:"attributes"`
}

// orbVersionListResponse is the paginated response for listing orb versions.
type orbVersionListResponse struct {
	Data      []orbVersionItem `json:"data"`
	Next      string           `json:"next"`
	NextToken string           `json:"next_page_token"`
}

// orbVersionRefResponse is the response for resolving a version by ref.
type orbVersionRefResponse struct {
	Data []orbVersionItem `json:"data"`
}

// createOrbRequest is the POST body for creating (reserving) an orb.
type createOrbRequest struct {
	Data createOrbData `json:"data"`
}

type createOrbData struct {
	Attributes createOrbAttributes `json:"attributes"`
	References createOrbRefs       `json:"references"`
}

type createOrbAttributes struct {
	Name      string `json:"name"`
	IsPrivate bool   `json:"is_private"`
}

type createOrbRefs struct {
	Namespace createOrbNamespaceRef `json:"namespace"`
}

type createOrbNamespaceRef struct {
	ID string `json:"id"`
}

// createOrbResponse is the body returned by POST /api/v3/orb/packages on success (201).
type createOrbResponse struct {
	Data orbPackageItem `json:"data"`
}

// publishVersionRequest is the POST body for publishing a new orb version.
type publishVersionRequest struct {
	Data publishVersionData `json:"data"`
}

type publishVersionData struct {
	Attributes publishVersionAttributes `json:"attributes"`
}

type publishVersionAttributes struct {
	OrbID   string `json:"orb_id"`
	YAML    string `json:"yaml"`
	Version string `json:"version"`
}

// ─────────────────────────────────────────────────────────────────────────────
// API methods
// ─────────────────────────────────────────────────────────────────────────────

// ResolveNamespaceID resolves a namespace name (e.g. "acme") to its CircleCI
// UUID via GET /api/v3/namespaces?filter[name]=<name>.
func (c *Client) ResolveNamespaceID(ctx context.Context, name string) (string, error) {
	u, err := url.Parse("namespaces")
	if err != nil {
		return "", fmt.Errorf("orb: building URL: %w", err)
	}
	q := u.Query()
	q.Set("filter[name]", name)
	u.RawQuery = q.Encode()

	req, err := c.rest.NewRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("orb: ResolveNamespaceID: build request: %w", err)
	}

	var resp namespaceResponse
	if _, err := c.rest.DoRequest(req, &resp); err != nil {
		return "", fmt.Errorf("orb: ResolveNamespaceID %q: %w", name, err)
	}
	if resp.Data.ID == "" {
		return "", fmt.Errorf("orb: namespace %q not found", name)
	}
	return resp.Data.ID, nil
}

// ListOrbs lists all orb packages in the namespace identified by namespaceID.
// It makes TWO calls — one for public (empty visibility filter) and one for
// private (filter[visibility]=private) — and merges the results, deduplicating
// by ID. It handles pagination via the next_page_token / next cursor.
func (c *Client) ListOrbs(ctx context.Context, namespaceID string) ([]OrbPackage, error) {
	public, err := c.listOrbsByVisibility(ctx, namespaceID, "")
	if err != nil {
		return nil, err
	}
	private, err := c.listOrbsByVisibility(ctx, namespaceID, "private")
	if err != nil {
		return nil, err
	}

	// Merge, deduplicating by ID.
	seen := make(map[string]bool, len(public))
	merged := make([]OrbPackage, 0, len(public)+len(private))
	for _, p := range public {
		seen[p.ID] = true
		merged = append(merged, p)
	}
	for _, p := range private {
		if !seen[p.ID] {
			seen[p.ID] = true
			merged = append(merged, p)
		}
	}
	return merged, nil
}

// listOrbsByVisibility fetches all pages of orb packages for a namespace with
// the given visibility filter value (empty string = default/public;
// "private" = private only).
func (c *Client) listOrbsByVisibility(ctx context.Context, namespaceID, visibility string) ([]OrbPackage, error) {
	var result []OrbPackage
	cursor := ""
	for {
		u, err := url.Parse("orb/packages")
		if err != nil {
			return nil, fmt.Errorf("orb: building URL: %w", err)
		}
		q := u.Query()
		q.Set("filter[namespace_id]", namespaceID)
		q.Set("filter[visibility]", visibility)
		if cursor != "" {
			q.Set("page[token]", cursor)
		}
		u.RawQuery = q.Encode()

		req, err := c.rest.NewRequest(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("orb: listOrbsByVisibility: build request: %w", err)
		}

		var resp orbPackageListResponse
		if _, err := c.rest.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("orb: list orbs (visibility=%q): %w", visibility, err)
		}

		for _, item := range resp.Data {
			result = append(result, OrbPackage{
				ID: item.ID,
				// The API returns the FULL "<namespace>/<orb>" name; OrbPackage.Name
				// is the short name (the segment after the last "/"). Orb short names
				// never contain "/", so last-slash splitting is safe.
				Name:        shortOrbName(item.Attributes.Name),
				IsPrivate:   item.Attributes.IsPrivate,
				IsListed:    item.Attributes.IsListed,
				NamespaceID: item.References.Namespace.ID,
			})
		}

		// Advance cursor — prefer next_page_token, fall back to next.
		cursor = resp.NextToken
		if cursor == "" {
			cursor = resp.Next
		}
		if cursor == "" {
			break
		}
	}
	return result, nil
}

// ListStableVersions lists all released (stable channel) versions of the orb
// identified by orbID, handling pagination. Results are returned in API order
// (the caller is responsible for semver sorting if needed).
//
// GET /api/v3/orb/versions?filter[orb_id]=<id>&filter[channel]=stable
func (c *Client) ListStableVersions(ctx context.Context, orbID string) ([]OrbVersion, error) {
	var result []OrbVersion
	cursor := ""
	for {
		u, err := url.Parse("orb/versions")
		if err != nil {
			return nil, fmt.Errorf("orb: building URL: %w", err)
		}
		q := u.Query()
		q.Set("filter[orb_id]", orbID)
		q.Set("filter[channel]", "stable")
		if cursor != "" {
			q.Set("page[token]", cursor)
		}
		u.RawQuery = q.Encode()

		req, err := c.rest.NewRequest(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("orb: ListStableVersions: build request: %w", err)
		}

		var resp orbVersionListResponse
		if _, err := c.rest.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("orb: ListStableVersions %q: %w", orbID, err)
		}

		for _, item := range resp.Data {
			result = append(result, OrbVersion{
				ID:      item.ID,
				Version: item.Attributes.Version,
			})
		}

		cursor = resp.NextToken
		if cursor == "" {
			cursor = resp.Next
		}
		if cursor == "" {
			break
		}
	}
	return result, nil
}

// GetSource fetches the raw YAML source for the orb version identified by
// versionID. The endpoint returns text/plain content; this method reads the
// body as a string rather than JSON-decoding it.
//
// GET /api/v3/orb/versions/{id}/source
func (c *Client) GetSource(ctx context.Context, versionID string) (string, error) {
	// Build a plain GET request; we handle the response body directly.
	u, err := url.Parse("orb/versions/" + versionID + "/source")
	if err != nil {
		return "", fmt.Errorf("orb: building URL: %w", err)
	}

	req, err := c.rest.NewRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("orb: GetSource: build request: %w", err)
	}
	// Override Accept to accept text/plain in addition to JSON, since the
	// source endpoint serves YAML as text/plain.
	req.Header.Set("Accept", "text/plain, application/json")

	resp, err := c.rest.RawDo(req)
	if err != nil {
		return "", fmt.Errorf("orb: GetSource %q: %w", versionID, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("orb: GetSource %q: HTTP %d: %s", versionID, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("orb: GetSource %q: reading body: %w", versionID, err)
	}
	return string(body), nil
}

// ResolveVersionRef resolves an orb version reference (e.g. "acme/my-orb@1.2.3")
// to an OrbVersion. Returns nil (and no error) when the version does not exist.
//
// GET /api/v3/orb/versions?filter[ref]=<ns>/<orb>@<version>
func (c *Client) ResolveVersionRef(ctx context.Context, ref string) (*OrbVersion, error) {
	u, err := url.Parse("orb/versions")
	if err != nil {
		return nil, fmt.Errorf("orb: building URL: %w", err)
	}
	q := u.Query()
	q.Set("filter[ref]", ref)
	u.RawQuery = q.Encode()

	req, err := c.rest.NewRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("orb: ResolveVersionRef: build request: %w", err)
	}

	var resp orbVersionRefResponse
	if status, err := c.rest.DoRequest(req, &resp); err != nil {
		// A missing ref comes back as 404 {"error":{"title":"not found"}} rather
		// than 200 {"data":[]}; treat that as "not found", not a hard error.
		if status == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("orb: ResolveVersionRef %q: %w", ref, err)
	}
	if len(resp.Data) == 0 {
		return nil, nil // not found — not an error
	}
	item := resp.Data[0]
	return &OrbVersion{ID: item.ID, Version: item.Attributes.Version}, nil
}

// CreateOrb creates (reserves) a new orb package in the destination namespace.
// It is idempotent: when the API returns a 409 / "already exists" response, the
// method resolves the existing orb by name and returns it.
//
// POST /api/v3/orb/packages
func (c *Client) CreateOrb(ctx context.Context, shortName, namespaceID string, isPrivate bool) (*OrbPackage, error) {
	u, err := url.Parse("orb/packages")
	if err != nil {
		return nil, fmt.Errorf("orb: building URL: %w", err)
	}

	body := createOrbRequest{
		Data: createOrbData{
			Attributes: createOrbAttributes{
				Name:      shortName,
				IsPrivate: isPrivate,
			},
			References: createOrbRefs{
				Namespace: createOrbNamespaceRef{ID: namespaceID},
			},
		},
	}
	req, err := c.rest.NewRequest(ctx, http.MethodPost, u, body)
	if err != nil {
		return nil, fmt.Errorf("orb: CreateOrb: build request: %w", err)
	}

	var resp createOrbResponse
	if _, err := c.rest.DoRequest(req, &resp); err != nil {
		// Idempotency: treat "already exists" (conflict) as success.
		if isAlreadyExistsErr(err) {
			return c.resolveOrbByName(ctx, shortName, namespaceID)
		}
		return nil, fmt.Errorf("orb: CreateOrb %q: %w", shortName, err)
	}
	item := resp.Data
	return &OrbPackage{
		ID:          item.ID,
		Name:        item.Attributes.Name,
		IsPrivate:   item.Attributes.IsPrivate,
		IsListed:    item.Attributes.IsListed,
		NamespaceID: item.References.Namespace.ID,
	}, nil
}

// shortOrbName returns the orb's short name from a possibly-full "<ns>/<orb>"
// name. Orb short names never contain "/", so the segment after the last "/"
// is the short name; a name with no "/" is returned unchanged.
func shortOrbName(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// resolveOrbByName looks up an existing orb by its short name within a
// namespace. It lists all orbs in the namespace and finds the matching one.
// This is the fallback path when CreateOrb returns a 409 conflict.
func (c *Client) resolveOrbByName(ctx context.Context, shortName, namespaceID string) (*OrbPackage, error) {
	orbs, err := c.ListOrbs(ctx, namespaceID)
	if err != nil {
		return nil, fmt.Errorf("orb: resolveOrbByName %q: %w", shortName, err)
	}
	for i := range orbs {
		if orbs[i].Name == shortName {
			return &orbs[i], nil
		}
	}
	return nil, fmt.Errorf("orb: resolveOrbByName: orb %q not found in namespace %q after conflict", shortName, namespaceID)
}

// PublishVersion publishes a new version of an orb. It implements the
// verify-after-write pattern required because the API spuriously returns HTTP
// 500 even when the publish commits successfully:
//
//  1. POST /api/v3/orb/versions with the orb YAML.
//  2. On any error (including 500), resolve the version ref on the destination.
//  3. If the ref resolves → the publish succeeded despite the 500; return nil.
//  4. Only if the ref does NOT resolve → treat as a genuine failure.
//
// destRef must be the fully-qualified ref "<dest-ns>/<orb-short-name>@<version>"
// used to verify the write.
func (c *Client) PublishVersion(ctx context.Context, orbID, version, yaml, destRef string) error {
	u, err := url.Parse("orb/versions")
	if err != nil {
		return fmt.Errorf("orb: building URL: %w", err)
	}

	body := publishVersionRequest{
		Data: publishVersionData{
			Attributes: publishVersionAttributes{
				OrbID:   orbID,
				YAML:    yaml,
				Version: version,
			},
		},
	}
	req, err := c.rest.NewRequest(ctx, http.MethodPost, u, body)
	if err != nil {
		return fmt.Errorf("orb: PublishVersion: build request: %w", err)
	}

	_, postErr := c.rest.DoRequest(req, nil)
	if postErr == nil {
		// Clean 2xx — success.
		return nil
	}

	// The API may return 500 even when the publish commits. Verify-after-write:
	// resolve the dest ref and treat a successful resolve as success.
	ver, verifyErr := c.ResolveVersionRef(ctx, destRef)
	if verifyErr != nil {
		// Could not verify — surface the original POST error.
		return fmt.Errorf("orb: PublishVersion %q@%s: post failed (%w) and verify failed (%v)", destRef, version, postErr, verifyErr)
	}
	if ver != nil {
		// Version exists on dest — the publish succeeded despite the spurious error.
		return nil
	}
	// Version not found on dest — genuine failure.
	return fmt.Errorf("orb: PublishVersion %q@%s: %w", destRef, version, postErr)
}

// isAlreadyExistsErr returns true when err indicates a resource-already-exists
// condition (HTTP 409 Conflict or a message containing "already exists").
func isAlreadyExistsErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "conflict")
}
