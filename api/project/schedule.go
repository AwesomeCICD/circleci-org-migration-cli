package project

import (
	"fmt"
	"net/url"
)

// Schedule represents a pipeline schedule as returned by
// GET /api/v2/project/{project-slug}/schedule.
//
// JSON field names confirmed from:
//   - https://circleci.com/docs/api/v2/index.html (listSchedulesForProject response schema)
//   - github.com/CircleCI-Public/circleci-cli api/schedule.go
//
// Timetable and Parameters are kept as map[string]any because their shape is
// flexible (the API supports both days-of-week and days-of-month variants, and
// parameters can be integers, strings, or booleans).  Callers that need typed
// access can unmarshal from the map.
type Schedule struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Timetable   map[string]any `json:"timetable"`
	Parameters  map[string]any `json:"parameters"`
}

type listSchedulesResponse struct {
	Items         []Schedule `json:"items"`
	NextPageToken string     `json:"next_page_token"`
}

// ListSchedules returns all pipeline schedules for the given project slug,
// fetching all pages automatically.
//
// Endpoint: GET /api/v2/project/{project-slug}/schedule
func (c *Client) ListSchedules(slug string) ([]Schedule, error) {
	var all []Schedule
	pageToken := ""

	for {
		u, err := slugSubresource(slug, "schedule")
		if err != nil {
			return nil, fmt.Errorf("ListSchedules: %w", err)
		}
		if pageToken != "" {
			q := url.Values{}
			q.Set("page-token", pageToken)
			u.RawQuery = q.Encode()
		}

		req, err := c.v2.NewRequest("GET", u, nil)
		if err != nil {
			return nil, fmt.Errorf("ListSchedules: build request: %w", err)
		}

		var resp listSchedulesResponse
		if _, err := c.v2.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListSchedules %q: %w", slug, err)
		}

		all = append(all, resp.Items...)

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	return all, nil
}
