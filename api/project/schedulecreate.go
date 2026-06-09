package project

import (
	"fmt"
)

// createScheduleRequest is the wire format for POST /api/v2/project/{slug}/schedule.
//
// JSON shape confirmed from:
//   - https://circleci.com/docs/api/v2/index.html (createSchedule request body)
//
// attribution-actor is always "system" because the source actor (user token)
// will not exist on the destination org.
type createScheduleRequest struct {
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	AttributionActor string         `json:"attribution-actor"`
	Timetable        map[string]any `json:"timetable"`
	Parameters       map[string]any `json:"parameters"`
}

// CreateSchedule creates a scheduled pipeline for the given project slug.
//
// Endpoint: POST /api/v2/project/{slug}/schedule
// Request body:
//
//	{"name":"...","description":"...","attribution-actor":"system",
//	 "timetable":{...},"parameters":{...}}
//
// attribution-actor is always "system" — the source actor won't exist on the
// destination.  Timetable and parameters are passed through as-is from the
// captured manifest.
//
// NOTE: This endpoint only works for OAuth/Bitbucket projects (where the
// pipeline definition is stored in the repo).  For GitHub App ("circleci/"
// provider) projects, schedules must be created via the Trigger API (a future
// milestone) and this method should NOT be called.  The caller is responsible
// for checking the destination provider before calling.
func (c *Client) CreateSchedule(destSlug, name, description, _ string, timetable, parameters map[string]any) error {
	if destSlug == "" {
		return fmt.Errorf("project: CreateSchedule requires destSlug")
	}

	u, err := slugSubresource(destSlug, "schedule")
	if err != nil {
		return fmt.Errorf("project: CreateSchedule: %w", err)
	}

	body := createScheduleRequest{
		Name:             name,
		Description:      description,
		AttributionActor: "system",
		Timetable:        timetable,
		Parameters:       parameters,
	}

	req, err := c.v2.NewRequest("POST", u, &body)
	if err != nil {
		return fmt.Errorf("project: CreateSchedule: build request: %w", err)
	}

	var ignored map[string]any
	if _, err := c.v2.DoRequest(req, &ignored); err != nil {
		return fmt.Errorf("project: CreateSchedule %q/%q: %w", destSlug, name, err)
	}
	return nil
}
