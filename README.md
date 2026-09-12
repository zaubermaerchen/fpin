# fpin

`fpin` turns standard input into a file and prints the file's absolute path to standard output when it succeeds.

## Usage

```text
fpin [-0|--null] [--] [FILE]
```

With no arguments, `fpin` writes standard input to a new temporary file. With `FILE`, it writes to that path, overwriting an existing file. Parent directories are not created. On success, the absolute output path followed by a newline is written to standard output.

Use `-0` or `--null` to terminate the successful output path with a single NUL byte instead of a newline. Options must precede `FILE`; `--` ends option parsing so a destination beginning with `-` can be used. Unknown or duplicate options, extra arguments, and combining `--version` with any other argument fail without reading standard input or changing a destination.

### Behavior

- A successful temporary file remains until the caller removes it.
- If writing or closing a temporary file fails, `fpin` attempts to remove it; it may remain if removal fails.
- A specified `FILE` may be truncated or partially written on failure; it is not rolled back.
- Failures produce a diagnostic on standard error and a nonzero exit status, without a usable output path on standard output.
- Returned paths are absolute, but symlink components are not canonicalized.

For paths containing spaces, check the pipeline and keep the command substitution result quoted when consuming it:

```bash
set -o pipefail
if ! path=$(producer | fpin); then
  exit 1
fi
use-command "$path"
```

To choose the output file explicitly (the parent directory must already exist):

```bash
set -o pipefail
if ! producer | fpin ./output.bin; then
  exit 1
fi
```

For arbitrary path names, use NUL mode with `xargs -0`:

```bash
records=$(mktemp) || exit 1
trap 'rm -f "$records"' EXIT
set -o pipefail
if ! producer | fpin --null >"$records"; then
  exit 1
fi
xargs -0 -n 1 use-command <"$records"
```

## Version

```bash
fpin --version
```

The default version is `dev`, so the command prints `fpin dev` unless a different version is injected. A release or other build can inject a version at build time:

```bash
go build -ldflags "-X main.version=v0.1.0" -o fpin ./cmd/fpin
```

## Releases

Published releases will be available on [GitHub Releases](https://github.com/zaubermaerchen/fpin/releases). The release workflow produces archives for Linux amd64, arm64, and armv6; macOS amd64 and arm64; and Windows amd64 and arm64. Linux and macOS archives use `.tar.gz`; Windows archives use `.zip`. Each archive contains the binary, `README.md`, and `LICENSE`, and releases include `SHA256SUMS`.

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
```

fpin is available under the [MIT License](LICENSE).
