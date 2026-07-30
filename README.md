# 7331 CLI

`7331` uploads JPEG, PNG, WebP, and GIF images to
[7331.cloud](https://7331.cloud) from macOS, Linux, or Windows. It is a single,
dependency-free Go binary with no account, telemetry, background checks, or
automatic updates.

## Install

Homebrew on macOS or Linux:

```bash
brew tap 1337lean/tap
brew trust --cask 1337lean/tap/7331
brew install --cask 7331
```

Scoop on Windows:

```powershell
scoop bucket add 7331 https://github.com/1337lean/scoop-bucket
scoop install 7331
```

Go 1.25 or newer:

```bash
go install github.com/1337lean/7331-cli/cmd/7331@latest
```

Direct download on macOS or Linux, which needs no package manager:

```bash
VERSION=0.1.0
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -fsSL "https://github.com/1337lean/7331-cli/releases/download/v${VERSION}/7331_${VERSION}_${OS}_${ARCH}.tar.gz" | tar -xz 7331
sudo install 7331 /usr/local/bin/7331
```

Versioned archives and SHA-256 checksums are available on
[GitHub Releases](https://github.com/1337lean/7331-cli/releases), and every
archive carries a GitHub build provenance attestation:

```bash
gh attestation verify 7331_${VERSION}_${OS}_${ARCH}.tar.gz --repo 1337lean/7331-cli
```

### macOS Gatekeeper

Release binaries are not signed with an Apple Developer certificate. The
`arm64` builds carry the ad-hoc signature that the Go linker applies, which is
what Apple Silicon requires to execute them, but they are not notarized.

Homebrew quarantines cask downloads, so the cask clears the attribute in a
post-install hook; `brew trust --cask` above is what authorizes that hook to
run. Archives fetched with `curl` are never quarantined in the first place, so
the direct download above needs no extra step.

## Use

```bash
7331 upload
7331 upload image.png
7331 upload one.png two.jpg --expires 1h
7331 upload image.png --url-only
7331 upload image.png --json

7331 info PUBLIC_ID
7331 info https://i.7331.cloud/PUBLIC_ID.png --json

7331 delete PUBLIC_ID
7331 delete 'https://7331.cloud/d/PUBLIC_ID#token=SECRET'
7331 delete PUBLIC_ID --yes

7331 list
7331 list --json

7331 version
```

Upload accepts one to five files of at most 25 MiB each. Retention can be `5m`,
`10m`, `30m`, `1h`, `6h`, `12h`, or `24h`; the default is `24h`. File type is
detected from the image bytes, not its extension.

When `7331 upload` is run without file arguments in an interactive terminal,
it prompts you to drag and drop one to five images into the terminal and press
Enter. The terminal inserts their paths; the CLI then uploads them normally.

When stdout is piped, `upload` prints one direct URL per successful upload.
Status and failures go to stderr. Files that cannot be read are reported
individually and do not cancel the rest of the batch. `--json` emits a
versioned envelope intended for scripts.

## Exit codes

| Code | Meaning                                                              |
| ---- | -------------------------------------------------------------------- |
| 0    | Success                                                              |
| 1    | The service or local state failed, including a partially failed batch |
| 2    | Invalid usage, or no argument was usable                             |

## Deletion credentials

Each upload returns a capability credential that can permanently delete it.
Unless `--no-save` is used, `7331` stores one owner-only JSON record per upload
in the platform state directory. A public ID passed to `7331 delete` uses that
saved credential. A complete deletion URL can be used on another machine; its
fragment is parsed locally and is never included in the HTTP request URL. A
deletion URL is refused when its host is not the server being contacted, so a
credential is never handed to a service that did not issue it.

`7331 list` shows the uploads this machine can still delete. Records for
uploads that have already expired are removed automatically by `list` and by
`upload`, so stored credentials do not outlive what they control. Neither
`list` nor its `--json` output includes deletion tokens unless
`--show-delete-url` is passed.

Treat deletion URLs and `--json` upload output as secrets: both contain the
deletion capability. Do not paste them into logs, issue trackers, chat, or shell
scripts that other users can read.

## Development

The module targets Go 1.25 source compatibility. Releases and CI use Go 1.26.5.
The CLI uses only the standard library.

```bash
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
```

For a local or self-hosted service:

```bash
7331 --server http://127.0.0.1:3000 upload image.png
```

`_7331_SERVER` provides the same override. The leading underscore is required
because a shell identifier cannot begin with a digit; `7331_SERVER` is still
read for compatibility, but it can only be set through `env(1)`. HTTPS is
required except for loopback addresses.

Anonymous CLI uploads are governed by the 7331.cloud acceptable-use policy and
share the service's per-IP allowance with browser uploads: 10 authorized images
and 100 MiB per UTC day.

### Maintainer release setup

The release repository needs a `PACKAGE_REPOS_TOKEN` Actions secret containing
a fine-grained GitHub token with contents write access only to
`1337lean/homebrew-tap` and `1337lean/scoop-bucket`. The normal `GITHUB_TOKEN`
publishes CLI releases. Tags containing a prerelease indicator, such as
`v0.1.0-rc.1`, intentionally skip both package repositories.

## License

[MIT](LICENSE)
