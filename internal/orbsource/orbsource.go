// Package orbsource fetches the YAML source of a published CircleCI orb via
// the CircleCI GraphQL API. The REST API has no equivalent endpoint for orb
// source, so a standalone GraphQL POST is used.
//
// Authentication: the Authorization header carries the raw token (NOT prefixed
// with "Bearer ") because that is what the CircleCI GraphQL endpoint expects.
package orbsource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/CircleCI-Public/circleci-org-migration-cli/version"
)

const (
	graphQLPath    = "/graphql-unstable"
	defaultTimeout = 30 * time.Second
)

// graphQLQuery is the query used to retrieve an orb version's YAML source.
const graphQLQuery = `query($r:String!){orbVersion(orbVersionRef:$r){version source}}`

// graphQLRequest is the request body sent to the GraphQL endpoint.
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// graphQLResponse is the top-level envelope returned by the GraphQL endpoint.
type graphQLResponse struct {
	Data   *orbVersionData `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

// orbVersionData holds the data.orbVersion field of the response.
type orbVersionData struct {
	OrbVersion *orbVersionPayload `json:"orbVersion"`
}

// orbVersionPayload holds the resolved orb version and its YAML source.
type orbVersionPayload struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

// graphQLError represents one error object in the top-level errors array.
type graphQLError struct {
	Message string `json:"message"`
}

// FetchOrbSource retrieves the YAML source of the orb identified by orbRef
// (e.g. "myns/myorb@1.2.3") from the CircleCI GraphQL API.
//
// If orbRef has no "@version" suffix, "@volatile" is appended so the API
// resolves the latest published version.
//
// host is the CircleCI server URL (e.g. "https://circleci.com"). token is the
// raw personal API token — NOT prefixed with "Bearer ".
func FetchOrbSource(host, token, orbRef string) (string, error) {
	return fetchOrbSource(http.DefaultClient, host, token, orbRef)
}

// fetchOrbSource is the internal implementation that accepts an *http.Client
// so tests can inject a fake server.
func fetchOrbSource(httpClient *http.Client, host, token, orbRef string) (string, error) {
	if !strings.Contains(orbRef, "@") {
		orbRef += "@volatile"
	}

	body := graphQLRequest{
		Query:     graphQLQuery,
		Variables: map[string]any{"r": orbRef},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("orbsource: encoding request: %w", err)
	}

	endpoint := strings.TrimRight(host, "/") + graphQLPath
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", fmt.Errorf("orbsource: building request: %w", err)
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())

	if httpClient.Timeout == 0 {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	resp, err := httpClient.Do(req) //nolint:bodyclose
	if err != nil {
		return "", fmt.Errorf("orbsource: HTTP request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("orbsource: server returned HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var gqlResp graphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return "", fmt.Errorf("orbsource: decoding response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return "", fmt.Errorf("orbsource: GraphQL errors for %q: %s", orbRef, strings.Join(msgs, "; "))
	}

	if gqlResp.Data == nil || gqlResp.Data.OrbVersion == nil {
		return "", fmt.Errorf("orbsource: orb %q not found (null orbVersion)", orbRef)
	}

	return gqlResp.Data.OrbVersion.Source, nil
}
