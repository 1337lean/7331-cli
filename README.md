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

Versioned archives and SHA-256 checksums are available on
[GitHub Releases](https://github.com/1337lean/7331-cli/releases).

## Use

```bash
7331 upload image.png
7331 upload one.png two.jpg --expires 1h
7331 upload image.png --url-only
7331 upload image.png --json

7331 info PUBLIC_ID
7331 info https://i.7331.cloud/PUBLIC_ID.png --json

7331 delete PUBLIC_ID
7331 delete 'https://7331.cloud/d/PUBLIC_ID#token=SECRET'
7331 delete PUBLIC_ID --yes
```

Upload accepts one to five files of at most 25 MiB each. Retention can be `5m`,
`10m`, `30m`, `1h`, `6h`, `12h`, or `24h`; the default is `24h`. File type is
detected from the image bytes, not its extension.

When stdout is piped, `upload` prints one direct URL per successful upload.
Status and failures go to stderr, and a partially successful batch exits with
status 1. `--json` emits a versioned envelope intended for scripts.

## Deletion credentials

Each upload returns a capability credential that can permanently delete it.
Unless `--no-save` is used, `7331` stores one owner-only JSON record per upload
in the platform state directory. A public ID passed to `7331 delete` uses that
saved credential. A complete deletion URL can be used on another machine; its
fragment is parsed locally and is never included in the HTTP request URL.

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

`7331_SERVER` provides the same override. HTTPS is required except for loopback
addresses.

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
