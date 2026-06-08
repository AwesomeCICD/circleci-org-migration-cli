package version

import "fmt"

// These vars are set by goreleaser via -ldflags -X.
var (
	// Version is the current Git tag (v prefix stripped) or snapshot name.
	Version = "dev"
	// Commit is the current git commit SHA.
	Commit = "none"
)

// UserAgent returns the User-Agent header value for HTTP requests.
func UserAgent() string {
	return fmt.Sprintf("circleci-migrate/%s", Version)
}
