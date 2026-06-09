module github.com/CircleCI-Public/circleci-org-migration-cli

go 1.26

// Pin a vuln-free patch toolchain (latest stdlib security fixes; see govulncheck).
toolchain go1.26.4

require github.com/spf13/cobra v1.10.2

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)
