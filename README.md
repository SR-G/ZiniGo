# ZiniGo

A tool written in Go for saving (legally purchased) magazines from Zinio as DRM-free PDFs. 

## Precompiled binaries

ZiniGo can be downloaded for Windows and Linux at https://github.com/TheAxeDude/ZiniGo/tree/master/built

## Usage

./zinigo -u=Username -p=Password
./zinigo --email=Username --password=Password

Options can also be set with `ZINIGO_*` environment variables or a JSON/TOML config file. Pass `--config=/path/to/config.json` to select a file explicitly; otherwise ZiniGo looks for a config file named after the executable in the working directory.

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
<!-- weavehelp:end -->

You can use `-playwright=true` to use Playwright to run the tests. It'll download a browser in the background.

## Requirements

No specific dependencies need to be installed, however a Chrome/Chromium executable is required to render pages to PDF.

If using a pre-installed Chrome, the command `google-chrome` should work.

## How it works
ZiniGo logs into Zinio, and pulls a list of all issues purchased. 

Each page is available as an SVG, which is then injected into an HTML page (based on template.html).

PlayWright (or google-chrome) is then used to print the page to PDF, and all pages are combined into a single PDF.

## Building
Build for linux & windows on windows using the powershell script in buildscripts
Build for linux & windows on macos using the darwin.sh script in buildscripts

