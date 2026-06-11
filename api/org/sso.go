package org

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
)

// ssoEnforcedResponse mirrors GET /private/ciam/orgs/{orgID}/sso/enforced.
//
// Confirmed live against app.circleci.com:
//
//	{"id","vcs_type","name","display_name","enforced":bool}
type ssoEnforcedResponse struct {
	Enforced bool `json:"enforced"`
}

// GetSSOEnforced reports whether SSO login is enforced for the org.
//
// Endpoint: GET https://app.circleci.com/private/ciam/orgs/{orgID}/sso/enforced
//
// Like the other private CIAM endpoints (see groups.go) this is served by
// app.circleci.com, not circleci.com; the org client's app base URL handles the
// host rewrite and the token travels in the Circle-Token header.
func (c *Client) GetSSOEnforced(ctx context.Context, orgID string) (bool, error) {
	u, err := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/sso/enforced")
	if err != nil {
		return false, fmt.Errorf("GetSSOEnforced: build URL: %w", err)
	}

	req, err := c.app.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return false, fmt.Errorf("GetSSOEnforced: build request: %w", err)
	}

	var raw ssoEnforcedResponse
	if _, err := c.app.DoRequest(req, &raw); err != nil {
		return false, fmt.Errorf("GetSSOEnforced %s: %w", orgID, err)
	}
	return raw.Enforced, nil
}

// GetSSOConnection returns the org's SSO (SAML) connection body.
//
// Endpoint: GET https://app.circleci.com/private/ciam/orgs/{orgID}/sso/connection
//
// The endpoint returns the connection body (including "realm" and IdP fields per
// the web-ui SSOConnection shape) when SSO is configured, or HTTP 404
// {"message":"connection not found"} when none is. A 404 is NOT an error — it is
// the normal "no SSO" case — so it returns (nil, false, nil). On 200 it returns
// (body, true, nil). Any other failure is returned as an error.
func (c *Client) GetSSOConnection(ctx context.Context, orgID string) (connection map[string]any, found bool, err error) {
	u, perr := url.Parse("private/ciam/orgs/" + url.PathEscape(orgID) + "/sso/connection")
	if perr != nil {
		return nil, false, fmt.Errorf("GetSSOConnection: build URL: %w", perr)
	}

	req, rerr := c.app.NewRequest(ctx, "GET", u, nil)
	if rerr != nil {
		return nil, false, fmt.Errorf("GetSSOConnection: build request: %w", rerr)
	}

	var body map[string]any
	if _, derr := c.app.DoRequest(req, &body); derr != nil {
		var httpErr *rest.HTTPError
		if errors.As(derr, &httpErr) && httpErr.Code == http.StatusNotFound {
			// No SSO connection configured — not an error.
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("GetSSOConnection %s: %w", orgID, derr)
	}
	return body, true, nil
}
