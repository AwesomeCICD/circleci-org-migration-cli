package context

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// gqlRequest is the JSON body sent to the GraphQL endpoint.
type gqlRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// gqlResponse is the envelope returned by the GraphQL endpoint.
type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

// gqlError represents a single GraphQL error object.
type gqlError struct {
	Message string `json:"message"`
}

func (e gqlError) Error() string { return e.Message }

// doGQL posts a GraphQL request to endpoint, decodes the data field into out,
// and returns an error if any GraphQL errors are present.
func doGQL(httpClient *http.Client, endpoint *url.URL, token, query string, variables map[string]interface{}, out interface{}) error {
	body, err := json.Marshal(gqlRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("graphql: marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("graphql: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("graphql: HTTP request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var gqlResp gqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return fmt.Errorf("graphql: decode response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("graphql: %s", strings.Join(msgs, "; "))
	}

	if out != nil && gqlResp.Data != nil {
		if err := json.Unmarshal(gqlResp.Data, out); err != nil {
			return fmt.Errorf("graphql: decode data: %w", err)
		}
	}
	return nil
}
