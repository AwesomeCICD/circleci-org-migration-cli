package context

import (
	"fmt"
)

// EnvVar represents a context environment variable as returned by
// GET /api/v2/context/{id}/environment-variable.
// NOTE: The API does NOT return secret values — names only.
// JSON source: confirmed from circleci-cli api/context/context.go (EnvironmentVariable)
// and circleci-cli api/context/rest.go (ListEnvVarsWithRest).
type EnvVar struct {
	Name      string `json:"variable"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type listEnvVarsResponse struct {
	Items         []EnvVar `json:"items"`
	NextPageToken string   `json:"next_page_token"`
}

// ListEnvVars returns all environment variable names stored in a context.
// Values are never returned by the API and are not present in EnvVar.
func (c *Client) ListEnvVars(contextID string) ([]EnvVar, error) {
	if contextID == "" {
		return nil, fmt.Errorf("context: ListEnvVars requires contextID")
	}

	var all []EnvVar
	pageToken := ""

	for {
		path := fmt.Sprintf("context/%s/environment-variable", contextID)
		u, err := c.rest.BaseURL.Parse(path)
		if err != nil {
			return nil, err
		}
		if pageToken != "" {
			q := u.Query()
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.rest.NewRequest("GET", u, nil)
		if err != nil {
			return nil, err
		}

		var resp listEnvVarsResponse
		if _, err := c.rest.DoRequest(req, &resp); err != nil {
			return nil, err
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
