# ZiniGo

A tool written in Go for saving (legally purchased) magazines from Zinio as DRM-free PDFs. 

## Precompiled binaries

Download the latest precompiled binaries for Windows, Linux, and macOS from the [GitHub Releases](https://github.com/SR-G/ZiniGo/releases) page.

## Usage

./zinigo -u=Username -p=Password
./zinigo --email=Username --password=Password

Options can also be set with `ZINIGO_*` environment variables or a JSON/TOML config file. Pass `--config /path/to/config.json` to select a file explicitly; otherwise ZiniGo looks for a config file named after the executable in the working directory.

Example of output : 

```bash
🔐 Logging in...
📚 Fetching library...
✓ Found 70 issues

[1/70] XXX - December 2026 OK (102 pages)
[2/70] XXX - November 2026 OK (exists)
[3/70] XXX - October 2026 OK (exists)
(...)
[70/70] XXX - August 2021 OK (exists)

==================================================
Summary: 70 OK  0 ERR
```

## Parameters

<!-- weavedoc:start -->
| Group | Type | Long | Short | Aliases | Positional | Env | Required | Default | Requires | Conflicts | Behavior | Description |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| - | bool | `--unlimited` | - | - | No | `ZINIGO_UNLIMITED` | No | - | - | - | - | Use Zinio Unlimited |
| - | string | `--email` | `-u` | - | No | `ZINIGO_EMAIL` | Yes | - | - | - | - | Zinio email |
| - | string | `--password` | `-p` | - | No | `ZINIGO_PASSWORD` | Yes | - | - | - | secret | Zinio password |
| - | string | `--fingerprint` | - | - | No | `ZINIGO_FINGERPRINT` | No | `abcd123` | - | - | - | Device fingerprint |
| - | bool | `--keep-temp` | - | - | No | `ZINIGO_KEEP_TEMP` | No | - | - | - | - | Keep per-page PDFs instead of deleting them |
| - | bool | `--verbose` | `-v` | - | No | `ZINIGO_VERBOSE` | No | - | - | - | - | Enable verbose logs |
| - | bool | `--debug-http` | - | - | No | `ZINIGO_DEBUG_HTTP` | No | - | - | - | - | Dump HTTP traffic |
| - | bool | `--analyze` | - | - | No | `ZINIGO_ANALYZE` | No | - | - | - | - | Only analyze |
| - | int | `--issue` | - | - | No | `ZINIGO_ISSUE` | No | - | - | - | - | Specific issue ID |
| - | string | `--output` | - | - | No | `ZINIGO_OUTPUT` | No | `issues` | - | - | - | Output directory |
| - | bool | `--thumbs` | - | - | No | `ZINIGO_THUMBS` | No | - | - | - | - | Download page thumbnails |
| - | bool | `--stripmenus` | - | - | No | `ZINIGO_STRIPMENUS` | No | - | - | - | - | Strip small TL badge via qpdf and awk |
| - | bool | `--debug-stripmerge` | - | - | No | `ZINIGO_DEBUG_STRIPMERGE` | No | - | - | - | - | Enable debug logs around strip and merge |
| - | string | `--debug-dir` | - | - | No | `ZINIGO_DEBUG_DIR` | No | `debug` | - | - | - | Directory for per-page debug dumps |
<!-- weavedoc:end -->

## Help

<!-- weavehelp:start -->
```bash
ZiniGo

USAGE:
    ZiniGo [OPTIONS]

OPTIONS:
        --analyze                    Only analyze [env: ZINIGO_ANALYZE]
        --debug-dir <DEBUG_DIR>      Directory for per-page debug dumps [env: ZINIGO_DEBUG_DIR] [default: debug]
        --debug-http                 Dump HTTP traffic [env: ZINIGO_DEBUG_HTTP]
        --debug-stripmerge           Enable debug logs around strip and merge [env: ZINIGO_DEBUG_STRIPMERGE]
        --fingerprint <FINGERPRINT>  Device fingerprint [env: ZINIGO_FINGERPRINT] [default: abcd123]
        --issue <ISSUE>              Specific issue ID [env: ZINIGO_ISSUE]
        --keep-temp                  Keep per-page PDFs instead of deleting them [env: ZINIGO_KEEP_TEMP]
        --output <OUTPUT>            Output directory [env: ZINIGO_OUTPUT] [default: issues]
        --stripmenus                 Strip small TL badge via qpdf and awk [env: ZINIGO_STRIPMENUS]
        --thumbs                     Download page thumbnails [env: ZINIGO_THUMBS]
        --unlimited                  Use Zinio Unlimited [env: ZINIGO_UNLIMITED]
    -p, --password <PASSWORD>        Zinio password [env: ZINIGO_PASSWORD] [required] [secret]
    -u, --email <EMAIL>              Zinio email [env: ZINIGO_EMAIL] [required]
    -v, --verbose                    Enable verbose logs [env: ZINIGO_VERBOSE]
    -h, --help                       Print help information
    -V, --version                    Print version information
    -c, --config <PATH>              Path to a JSON or TOML config file (auto-detected from the binary name if omitted)
        --print-config               Print the resolved configuration as JSON
```
<!-- weavehelp:end -->

## Requirements

No browser is required. Install `qpdf` and `awk` only if you use `--stripmenus`.

## How it works
ZiniGo logs into Zinio and retrieves your library of issues.

It downloads and decrypts the issue pages as PDFs, then combines them into one PDF per issue. The optional `--stripmenus` feature uses `qpdf` and `awk` to remove the small TL badge.

## DEV

### Building

Building from source requires Go 1.27.1 or newer.

- `make build` builds `bin/zinigo` for the current platform.
- `make test` runs the Go tests.
- `make distribution` builds binaries for Windows, Linux, and macOS in `distribution/`.

### Release

Pushing a `v`-prefixed tag triggers the release workflow:

```bash
VERSION="0.1.0"
git switch master
git pull --ff-only origin master
git tag -a "v${VERSION}" -m "Release v${VERSION}"
git push origin "v${VERSION}"
```