// Package secrets implements the in-pipeline extraction of secret VALUES that
// CircleCI's API never exposes. Inside a running job, a context's (or project's)
// environment variables are injected as ordinary env vars, so given the variable
// NAMES from an export manifest we can read their values from the environment
// and record them in a SecretBundle.
package secrets

import (
	"fmt"

	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// Result reports which variables were captured and which were absent from the
// environment (e.g. not injected into this job).
type Result struct {
	Found   []string
	Missing []string
}

// Lookup mirrors os.LookupEnv: it returns the value and whether it was set.
// It is injected so extraction is testable without touching the real env.
type Lookup func(name string) (string, bool)

// ExtractContext records the values of the named context's variables (read from
// lookup) into bundle. The job invoking this must reference exactly that context
// so its variables are injected into the environment.
func ExtractContext(m *manifest.Manifest, bundle *manifest.SecretBundle, contextName string, lookup Lookup) (*Result, error) {
	c := findContext(m, contextName)
	if c == nil {
		return nil, fmt.Errorf("context %q not found in manifest", contextName)
	}
	res := &Result{}
	for _, v := range c.EnvVars {
		if val, ok := lookup(v.Name); ok {
			bundle.SetContextSecret(contextName, v.Name, val)
			res.Found = append(res.Found, v.Name)
		} else {
			res.Missing = append(res.Missing, v.Name)
		}
	}
	return res, nil
}

// ExtractProject records the values of the named project's variables (read from
// lookup) into bundle. The job invoking this must run within that project so its
// variables are injected into the environment.
func ExtractProject(m *manifest.Manifest, bundle *manifest.SecretBundle, projectSlug string, lookup Lookup) (*Result, error) {
	p := findProject(m, projectSlug)
	if p == nil {
		return nil, fmt.Errorf("project %q not found in manifest", projectSlug)
	}
	res := &Result{}
	for _, v := range p.EnvVars {
		if val, ok := lookup(v.Name); ok {
			bundle.SetProjectSecret(projectSlug, v.Name, val)
			res.Found = append(res.Found, v.Name)
		} else {
			res.Missing = append(res.Missing, v.Name)
		}
	}
	return res, nil
}

func findContext(m *manifest.Manifest, name string) *manifest.Context {
	for i := range m.Contexts {
		if m.Contexts[i].Name == name {
			return &m.Contexts[i]
		}
	}
	return nil
}

func findProject(m *manifest.Manifest, slug string) *manifest.Project {
	for i := range m.Projects {
		if m.Projects[i].Slug == slug {
			return &m.Projects[i]
		}
	}
	return nil
}
