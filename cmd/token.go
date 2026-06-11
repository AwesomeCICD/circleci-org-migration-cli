package cmd

import "fmt"

// noSourceTokenError returns the canonical error returned when no source API
// token can be resolved. It covers all legs of the fallback chain so users
// are not left guessing which env var or flag to set.
//
// TODO(follow-up): export.go, sync.go, migrate.go, and secrets_capture.go
// inline an equivalent message today. Switch them to call noSourceTokenError()
// in a follow-up PR once the other in-flight branches have merged, to keep
// this PR's diff scoped to orb.go.
func noSourceTokenError() error {
	return fmt.Errorf("no source API token: set --source-token, --token, CIRCLECI_SOURCE_TOKEN, or CIRCLECI_CLI_TOKEN")
}
