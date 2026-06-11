module github.com/AwesomeCICD/circleci-org-migration-cli

go 1.26

// Pin a vuln-free patch toolchain (latest stdlib security fixes; see govulncheck).
toolchain go1.26.4

require (
	filippo.io/age v1.3.1
	github.com/spf13/cobra v1.10.2
	golang.org/x/crypto v0.53.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	filippo.io/hpke v0.4.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.6 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/sys v0.46.0 // indirect
)
