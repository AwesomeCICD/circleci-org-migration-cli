// Package cciurl builds CircleCI web-app settings URLs from host + org/project
// slug coordinates. It is shared between internal/report (export audit) and
// cmd/sync (sync plan renderer).
package cciurl

import (
	"fmt"
	"strings"
)

// AppHost returns the CircleCI app host for building settings URLs.
// For circleci.com sources the canonical app is app.circleci.com; for server
// deployments the source host itself is used.
func AppHost(sourceHost string) string {
	if sourceHost == "https://circleci.com" || sourceHost == "http://circleci.com" ||
		strings.TrimRight(sourceHost, "/") == "https://circleci.com" {
		return "https://app.circleci.com"
	}
	if sourceHost == "" {
		return "https://app.circleci.com"
	}
	return strings.TrimRight(sourceHost, "/")
}

// ProjectSettingsURL returns the CircleCI web-app URL for a project's settings
// root. For VCS-slug projects (gh/<org>/<repo> or bb/<org>/<repo>) it uses the
// documented /settings/project/:vcs/:org/:repo pattern. For standalone App
// projects (circleci/<orgUUID>/<projUUID>) it links to the project settings
// using the UUID segments.
//
// The tab parameter, when non-empty, appends "/<tab>" to the URL (e.g. "ssh",
// "env-vars", "webhooks", "advanced"). If the deep-link path for a specific tab
// is uncertain, pass "" and name the tab in prose instead.
func ProjectSettingsURL(sourceHost, slug, tab string) string {
	base := AppHost(sourceHost)
	parts := strings.SplitN(slug, "/", 3)
	if len(parts) != 3 {
		// Malformed slug — fall back to the projects list.
		return base + "/projects"
	}
	prefix, orgPart, repoPart := parts[0], parts[1], parts[2]

	var u string
	switch prefix {
	case "gh", "bb", "github", "bitbucket":
		// VCS OAuth slug: /settings/project/<vcs>/<org>/<repo>
		vcs := prefix
		if vcs == "github" {
			vcs = "gh"
		} else if vcs == "bitbucket" {
			vcs = "bb"
		}
		u = fmt.Sprintf("%s/settings/project/%s/%s/%s", base, vcs, orgPart, repoPart)
	default:
		// Standalone / App slug: circleci/<orgUUID>/<projUUID>
		// Use the UUID-based settings path.
		u = fmt.Sprintf("%s/settings/project/circleci/%s/%s", base, orgPart, repoPart)
	}

	if tab != "" {
		u += "/" + tab
	}
	return u
}

// OrgSettingsURL returns the CircleCI web-app URL for the org settings root (or
// a named tab). For VCS-slug orgs (gh/<org>, bb/<org>) it uses
// /settings/organization/<vcs>/<org>. For standalone (circleci/<uuid>) orgs it
// uses /settings/organization/circleci/<uuid>. The tab parameter, when
// non-empty, appends "/<tab>" (e.g. "contexts", "security"). Budget and
// usage-controls live under /plan not /settings, so callers should pass "" and
// name the tab in prose for those.
func OrgSettingsURL(sourceHost, orgSlug, tab string) string {
	base := AppHost(sourceHost)
	parts := strings.SplitN(orgSlug, "/", 2)
	if len(parts) != 2 {
		return base + "/settings/organization"
	}
	prefix, name := parts[0], parts[1]
	vcs := prefix
	if vcs == "github" {
		vcs = "gh"
	} else if vcs == "bitbucket" {
		vcs = "bb"
	}
	u := fmt.Sprintf("%s/settings/organization/%s/%s", base, vcs, name)
	if tab != "" {
		u += "/" + tab
	}
	return u
}
