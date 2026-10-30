package cmd

// White-box tests for resolving GitHub App destination projects by name in
// 'secrets transfer' / 'migrate --transfer-secrets'.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
)

func TestIsAppDestOrg(t *testing.T) {
	app := &fakeOrgGetter{org: &org.Organization{VCSType: "circleci"}}
	gh := &fakeOrgGetter{org: &org.Organization{VCSType: "github"}}
	boom := &fakeOrgGetter{err: errors.New("boom")}

	cases := []struct {
		name     string
		g        orgGetter
		slug     string
		wantApp  bool
		wantWarn bool
	}{
		{"circleci slug", boom, "circleci/0cbd", true, false},
		{"gh slug", app, "gh/acme", false, false},
		{"bb slug", app, "bb/acme", false, false},
		{"bare UUID, App org", app, "", true, false},
		{"bare UUID, github org", gh, "", false, false},
		{"bare UUID, lookup fails", boom, "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var errW bytes.Buffer
			got := isAppDestOrg(context.Background(), tc.g, tc.slug, "org-uuid", &errW)
			if got != tc.wantApp {
				t.Errorf("isAppDestOrg = %v, want %v", got, tc.wantApp)
			}
			if warned := strings.Contains(errW.String(), "WARNING"); warned != tc.wantWarn {
				t.Errorf("warning printed = %v, want %v (%q)", warned, tc.wantWarn, errW.String())
			}
		})
	}
}
