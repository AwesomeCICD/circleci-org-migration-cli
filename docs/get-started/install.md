# Install

Install with Homebrew on macOS or Linux, or download a signed binary for any platform. Then check the version.

## macOS and Linux

```bash
brew install AwesomeCICD/tap/circleci-migrate
circleci-migrate version
```

No Homebrew? Download the release archive for your platform:

```bash
VERSION=$(curl -sfL https://api.github.com/repos/AwesomeCICD/circleci-org-migration-cli/releases/latest \
  | grep '"tag_name"' | head -1 | cut -d'"' -f4)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -sfL "https://github.com/AwesomeCICD/circleci-org-migration-cli/releases/download/${VERSION}/circleci-migrate_${VERSION#v}_${OS}_${ARCH}.tar.gz" | tar -xz
sudo install -m 0755 circleci-migrate /usr/local/bin/
circleci-migrate version
```

Check: `version` prints the release you installed.

## Windows (PowerShell)

```powershell
$Version = (Invoke-RestMethod https://api.github.com/repos/AwesomeCICD/circleci-org-migration-cli/releases/latest).tag_name
$Arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$Zip = "circleci-migrate_$($Version.TrimStart('v'))_windows_$Arch.zip"
Invoke-WebRequest "https://github.com/AwesomeCICD/circleci-org-migration-cli/releases/download/$Version/$Zip" -OutFile $Zip
Expand-Archive $Zip -DestinationPath "$env:LOCALAPPDATA\circleci-migrate" -Force
& "$env:LOCALAPPDATA\circleci-migrate\circleci-migrate.exe" version
```

Add `%LOCALAPPDATA%\circleci-migrate` to your `PATH` to run `circleci-migrate` from anywhere. In PowerShell, set tokens with `$env:CIRCLECI_SOURCE_TOKEN = "<token>"` instead of `export`.

## With Go

```bash
go install github.com/AwesomeCICD/circleci-org-migration-cli@latest
```

This needs Go 1.26 or later.

## Verify the download

Every release archive has a `.bundle` file next to it. It holds a Sigstore signature made keylessly by CircleCI's release pipeline. Download both, then verify with [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) v3:

```bash
cosign verify-blob circleci-migrate_<version>_<os>_<arch>.tar.gz \
  --bundle circleci-migrate_<version>_<os>_<arch>.tar.gz.bundle \
  --certificate-oidc-issuer https://oidc.circleci.com \
  --certificate-identity-regexp 'https://circleci\.com/api/v2/projects/.*'
```

Check: cosign prints `Verified OK`. Do not run a binary that fails this check.

The certificate records the exact CircleCI pipeline definition that built the release, and the signature is logged on the public Rekor transparency log. To pin to that project, read the identity from the bundle, then replace `--certificate-identity-regexp` with `--certificate-identity <url>`:

```bash
jq -r '.verificationMaterial.certificate.rawBytes' circleci-migrate_<version>_<os>_<arch>.tar.gz.bundle \
  | base64 -d | openssl x509 -inform DER -noout -text | grep URI
```

The URL has the form `https://circleci.com/api/v2/projects/<project-id>/pipeline-definitions/<definition-id>`.

## Behind a proxy or allowlist

Allow these hosts for the install method you use.

| Method | Hosts |
|---|---|
| Release download | `api.github.com`, `github.com`, `release-assets.githubusercontent.com` |
| Homebrew | The hosts above, plus `formulae.brew.sh` and `raw.githubusercontent.com` |
| `go install` | `github.com`, `proxy.golang.org`, `sum.golang.org`, `filippo.io`, `golang.org`, `gopkg.in`, `go.yaml.in` |

If only `github.com` is reachable, `GOPROXY=off go install github.com/AwesomeCICD/circleci-org-migration-cli@<version>` fetches straight from the repository. Go still needs the vanity-import hosts, because it reads their `go-import` metadata over HTTPS even with the proxy off. Behind a corporate proxy, set `HTTPS_PROXY` or `GOPROXY=https://<your-proxy>` first.

Building from source (`git clone`, then `go build -o circleci-migrate .`) needs `github.com` for the clone and the same hosts for dependencies. The repository does not ship a `vendor/` directory, so `GOFLAGS=-mod=vendor` works only if you vendor the tree yourself with `go mod vendor`.

## Tokens and hosts

Each command reads these environment variables. A flag overrides the variable.

| Variable | Used for |
|---|---|
| `CIRCLECI_SOURCE_TOKEN` | Reading the source org (`--source-token`) |
| `CIRCLECI_DEST_TOKEN` | Writing the destination org (`--dest-token`) |
| `CIRCLECI_CLI_TOKEN` or `CIRCLE_TOKEN` | Fallback for both sides (`--token`) |
| `GITHUB_TOKEN` | Looking up GitHub repo IDs when needed (`--github-token`) |
| `CIRCLECI_CLI_HOST`, `CIRCLECI_HOST` or `CIRCLE_URL` | Host URL for CircleCI Server (`--host`) |

Create tokens under User Settings > Personal API Tokens. The token's user must be an admin of the org it acts on. The host defaults to `https://circleci.com`.

## As a plugin to the circleci CLI

If you use the official [circleci CLI](https://circleci.com/docs/local-cli/), run `circleci run migrate <args>`. It starts `circleci-migrate` from your `PATH` and passes your configured token and host. Bare `circleci migrate` is not supported.

Next: [choose your migration](choose.md).
