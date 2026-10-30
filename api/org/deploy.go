package org

import (
	"context"
	"fmt"
	"net/url"
)

// The public v3 Deploys (release tracker) API on app.circleci.com is
// EXPERIMENTAL and read-only: it exposes environments, components, and
// per-project deploy settings but no create/update endpoints. These methods
// therefore only read; the syncer reports deploy resources as manual steps.
// Reference: release-tracker docs/apispec/public/deploy-{environments,components,settings}.md

// DeployLabel is one key/value label on a deploy environment or component.
type DeployLabel struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// DeployEnvironment is a deploy environment.
type DeployEnvironment struct {
	ID          string
	Name        string
	Description string
	// Kind is the environment integration type (e.g. "custom"). Only returned
	// by the single-environment GET, not by the list.
	Kind   string
	Labels []DeployLabel
}

// DeployComponent is a deploy component (a deployable unit).
type DeployComponent struct {
	ID   string
	Name string
	// ProjectID is the UUID of the owning CircleCI project; empty when the
	// component is not tied to a project.
	ProjectID  string
	ArchivedAt string
	Labels     []DeployLabel
}

// DeploySettings is a project's deploy settings.
type DeploySettings struct {
	ProjectID string
	// PipelineIDs are the deploy/rollback pipeline definitions. They are
	// source-org-specific UUIDs and are never replayed to a destination.
	PipelineIDs []string
}

type deployEnvItem struct {
	ID         string `json:"id"`
	Attributes struct {
		Name        string        `json:"name"`
		Description string        `json:"description"`
		Kind        string        `json:"kind"`
		Labels      []DeployLabel `json:"labels"`
	} `json:"attributes"`
}

type deployPage struct {
	Next string `json:"next"`
}

type deployEnvList struct {
	Data []deployEnvItem `json:"data"`
	Page deployPage      `json:"page"`
}

type deployEnvGet struct {
	Data deployEnvItem `json:"data"`
}

type deployComponentList struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			Name       string        `json:"name"`
			ArchivedAt string        `json:"archived_at"`
			Labels     []DeployLabel `json:"labels"`
		} `json:"attributes"`
		References struct {
			Project struct {
				ID string `json:"id"`
			} `json:"project"`
		} `json:"references"`
	} `json:"data"`
	Page deployPage `json:"page"`
}

type deploySettingsGet struct {
	Data struct {
		References struct {
			Project struct {
				ID string `json:"id"`
			} `json:"project"`
			Pipelines []struct {
				ID string `json:"id"`
			} `json:"pipelines"`
		} `json:"references"`
	} `json:"data"`
}

// deployListURL builds api/v3/deploy/<resource>?filter[org_id]=<id>[&page[cursor]=<c>].
func deployListURL(resource, orgID, cursor string) *url.URL {
	u := &url.URL{Path: "api/v3/deploy/" + resource}
	q := u.Query()
	q.Set("filter[org_id]", orgID)
	if cursor != "" {
		q.Set("page[cursor]", cursor)
	}
	u.RawQuery = q.Encode()
	return u
}

// ListDeployEnvironments returns every deploy environment in the org
// (cursor-paginated).
//
// GET https://app.circleci.com/api/v3/deploy/environments?filter[org_id]=<uuid>
func (c *Client) ListDeployEnvironments(ctx context.Context, orgUUID string) ([]DeployEnvironment, error) {
	var out []DeployEnvironment
	cursor := ""
	for {
		req, err := c.app.NewRequest(ctx, "GET", deployListURL("environments", orgUUID, cursor), nil)
		if err != nil {
			return nil, fmt.Errorf("ListDeployEnvironments: build request: %w", err)
		}
		var resp deployEnvList
		if _, err := c.app.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListDeployEnvironments %s: %w", orgUUID, err)
		}
		for _, it := range resp.Data {
			out = append(out, DeployEnvironment{
				ID:          it.ID,
				Name:        it.Attributes.Name,
				Description: it.Attributes.Description,
				Kind:        it.Attributes.Kind,
				Labels:      it.Attributes.Labels,
			})
		}
		if resp.Page.Next == "" || len(resp.Data) == 0 {
			break
		}
		cursor = resp.Page.Next
	}
	return out, nil
}

// GetDeployEnvironment returns one environment including kind and description.
//
// GET https://app.circleci.com/api/v3/deploy/environments/{id}
func (c *Client) GetDeployEnvironment(ctx context.Context, id string) (*DeployEnvironment, error) {
	u := &url.URL{Path: "api/v3/deploy/environments/" + url.PathEscape(id)}
	req, err := c.app.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetDeployEnvironment: build request: %w", err)
	}
	var resp deployEnvGet
	if _, err := c.app.DoRequest(req, &resp); err != nil {
		return nil, fmt.Errorf("GetDeployEnvironment %s: %w", id, err)
	}
	it := resp.Data
	return &DeployEnvironment{
		ID:          it.ID,
		Name:        it.Attributes.Name,
		Description: it.Attributes.Description,
		Kind:        it.Attributes.Kind,
		Labels:      it.Attributes.Labels,
	}, nil
}

// ListDeployComponents returns every deploy component in the org
// (cursor-paginated).
//
// GET https://app.circleci.com/api/v3/deploy/components?filter[org_id]=<uuid>
func (c *Client) ListDeployComponents(ctx context.Context, orgUUID string) ([]DeployComponent, error) {
	var out []DeployComponent
	cursor := ""
	for {
		req, err := c.app.NewRequest(ctx, "GET", deployListURL("components", orgUUID, cursor), nil)
		if err != nil {
			return nil, fmt.Errorf("ListDeployComponents: build request: %w", err)
		}
		var resp deployComponentList
		if _, err := c.app.DoRequest(req, &resp); err != nil {
			return nil, fmt.Errorf("ListDeployComponents %s: %w", orgUUID, err)
		}
		for _, it := range resp.Data {
			out = append(out, DeployComponent{
				ID:         it.ID,
				Name:       it.Attributes.Name,
				ProjectID:  it.References.Project.ID,
				ArchivedAt: it.Attributes.ArchivedAt,
				Labels:     it.Attributes.Labels,
			})
		}
		if resp.Page.Next == "" || len(resp.Data) == 0 {
			break
		}
		cursor = resp.Page.Next
	}
	return out, nil
}

// GetDeploySettings returns a project's deploy settings. A project that never
// saved deploy settings yields empty settings, not an error.
//
// GET https://app.circleci.com/api/v3/deploy/settings?filter[project_id]=<uuid>
func (c *Client) GetDeploySettings(ctx context.Context, projectUUID string) (*DeploySettings, error) {
	u := &url.URL{Path: "api/v3/deploy/settings"}
	q := u.Query()
	q.Set("filter[project_id]", projectUUID)
	u.RawQuery = q.Encode()

	req, err := c.app.NewRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("GetDeploySettings: build request: %w", err)
	}
	var resp deploySettingsGet
	if _, err := c.app.DoRequest(req, &resp); err != nil {
		return nil, fmt.Errorf("GetDeploySettings %s: %w", projectUUID, err)
	}
	s := &DeploySettings{ProjectID: resp.Data.References.Project.ID}
	for _, p := range resp.Data.References.Pipelines {
		s.PipelineIDs = append(s.PipelineIDs, p.ID)
	}
	return s, nil
}
