# gowheels

Package Go binaries as Python wheels or npm packages and publish them to PyPI or the npm registry. A single static binary compiled with `CGO_ENABLED=0` is wrapped in a thin Python or Node.js launcher, giving `pip` and `npm` users a zero-dependency install experience on every supported platform.

```sh
pip install mytool          # installs the right binary for the user's OS and CPU
npm install -g mytool       # same idea, npm side
```

## Quick start

```sh
# 1. Install
go install github.com/StevenACoffman/gowheels@latest

# 2. Build wheels from a GoReleaser GitHub release and upload to PyPI
gowheels pypi --name mytool --repo owner/mytool --upload

# 3. Or preview what would happen without touching anything
gowheels pypi --name mytool --repo owner/mytool --upload --dry-run
```

## Installation

```sh
go install github.com/StevenACoffman/gowheels@latest
```

Requires Go 1.23 or later.

## Prerequisites

**Binaries must be statically linked.** gowheels sets `CGO_ENABLED=0` when using `--mode build`. If you supply pre-built binaries via `--artifact`, ensure they were also built with `CGO_ENABLED=0`, otherwise the wheels will fail on systems with different glibc versions or no glibc at all (Alpine, musl-based containers).

## Global flags

These flags are accepted by all subcommands:

| Flag | Description |
|------|-------------|
| `--dry-run` | Print what would happen without writing files or uploading |
| `--debug` | Enable debug-level structured logging |

---

## pypi — publish to PyPI

Builds [PEP 427](https://peps.python.org/pep-0427/) `.whl` files for each supported platform and optionally uploads them to PyPI. Without `--upload`, wheels are written to `--output` (default: `dist/`) and nothing is published — useful for inspecting the output before a real release.

### Examples

**Download from a GitHub release and publish (most common):**

```sh
gowheels pypi \
  --name mytool \
  --repo owner/mytool \
  --upload
```

**Use pre-built local binaries (e.g. from `goreleaser build --snapshot`):**

```sh
gowheels pypi \
  --name mytool \
  --artifact linux/amd64:dist/mytool_Linux_x86_64/mytool \
  --artifact linux/arm64:dist/mytool_Linux_arm64/mytool \
  --artifact darwin/amd64:dist/mytool_Darwin_x86_64/mytool \
  --artifact darwin/arm64:dist/mytool_Darwin_arm64/mytool \
  --artifact windows/amd64:dist/mytool_Windows_x86_64/mytool.exe \
  --upload
```

The `os/arch` key on the left of `:` is a Go platform identifier; the path on the right is arbitrary and just points to the binary file.

**Cross-compile from source and publish in one step:**

```sh
gowheels pypi \
  --name mytool \
  --mode build \
  --package ./cmd/mytool \
  --version 1.2.3 \
  --summary "My awesome tool" \
  --upload
```

**Separate package name and entry point from the binary name:**

Use `--package-name` when the PyPI package name must differ from the binary name (e.g. the simple name is taken), and `--entry-point` to control the `pip`-installed command name:

```sh
gowheels pypi \
  --name mytool \
  --package-name mytool-bin \
  --entry-point mytool \
  --repo owner/mytool \
  --upload
```

This publishes to PyPI as `mytool-bin` but installs a `mytool` command.

**Build wheels locally without uploading:**

```sh
gowheels pypi --name mytool --repo owner/mytool
# Wheels written to dist/; nothing uploaded.
```

### Binary source modes

gowheels infers the mode from whichever flags are set. Pass `--mode` explicitly only when the inference is ambiguous.

| Mode | Auto-detected when | What it does |
|------|--------------------|--------------|
| `release` | `--repo` is set | Downloads archives from a GitHub Release, extracts binaries |
| `local` | `--artifact` is set | Reads pre-built binaries from local paths |
| `build` | `--package` or `--mod-dir` is set | Cross-compiles with `go build` (`CGO_ENABLED=0`) |

### Supported platforms

| Platform | Wheel tag(s) |
|----------|--------------|
| Linux x86-64 | `manylinux_2_17_x86_64.manylinux2014_x86_64`, `musllinux_1_2_x86_64` |
| Linux arm64 | `manylinux_2_17_aarch64.manylinux2014_aarch64`, `musllinux_1_2_aarch64` |
| macOS x86-64 | `macosx_10_9_x86_64` |
| macOS arm64 | `macosx_11_0_arm64` |
| Windows x86-64 | `win_amd64` |
| Windows arm64 | `win_arm64` |

A single static Linux binary produces **both** a manylinux and a musllinux wheel — glibc and musl users are both covered without a second compilation. Use `--platforms linux/amd64,darwin/arm64` to restrict which platforms are built.

### PyPI authentication

When `--upload` is set, gowheels authenticates using the first available credential:

1. **`PYPI_TOKEN` environment variable** — a [PyPI API token](https://pypi.org/manage/account/token/) with upload scope for the package
2. **GitHub Actions OIDC** — requires `id-token: write` permission in the workflow and a [trusted publisher](https://docs.pypi.org/trusted-publishers/) configured on PyPI (no token needed)

### GitHub Actions workflow

```yaml
on:
  push:
    tags: ['v*']

jobs:
  publish-pypi:
    runs-on: ubuntu-latest
    permissions:
      id-token: write   # enables PyPI OIDC — no PYPI_TOKEN secret required
      contents: read
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: stable }
      - run: go install github.com/StevenACoffman/gowheels@latest
      - name: Publish wheels to PyPI
        run: |
          gowheels pypi \
            --name mytool \
            --repo ${{ github.repository }} \
            --version ${{ github.ref_name }} \
            --summary "My awesome tool" \
            --upload
```

To use an API token instead of OIDC, remove `id-token: write` and add:
```yaml
env:
  PYPI_TOKEN: ${{ secrets.PYPI_TOKEN }}
```

### All flags

**Package identity**

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | *(required)* | Binary name; used as the PyPI package name |
| `--package-name` | `--name` | PyPI package name when it must differ from the binary name |
| `--entry-point` | `--name` | `console_scripts` key installed by pip |

**Metadata**

| Flag | Default | Description |
|------|---------|-------------|
| `--version` | git tag | Version in semver or PEP 440 format |
| `--py-version` | `--version` | Override the Python package version independently |
| `--summary` | | One-line package description |
| `--license-expr` | `MIT` | SPDX license expression |
| `--license` | | Path to license file; bundled as `dist-info/licenses/LICENSE.txt` |
| `--readme` | auto-detect | Path to README (auto-detects `README.md`, `.rst`, `.txt`) |
| `--no-readme` | | Disable README auto-detection |
| `--url` | | Repository URL added as `Project-URL` in METADATA |

**Build and output**

| Flag | Default | Description |
|------|---------|-------------|
| `--platforms` | all | Comma-separated `os/arch` filter, e.g. `linux/amd64,darwin/arm64` |
| `--output` | `dist` | Output directory for `.whl` files |
| `--upload` | | Upload wheels to PyPI after building |
| `--pypi-url` | PyPI | PyPI upload endpoint (for TestPyPI: `https://test.pypi.org/legacy/`) |
| `--mode` | inferred | Binary source: `release`, `local`, or `build` |

**Release mode** (`--repo` infers this mode)

| Flag | Default | Description |
|------|---------|-------------|
| `--repo` | | GitHub repository in `owner/name` format |
| `--assets` | | Comma-separated explicit asset name overrides |
| `--cache` | `$XDG_CACHE_HOME/gowheels` | Local cache directory for downloaded archives |

**Local mode** (`--artifact` infers this mode)

| Flag | Default | Description |
|------|---------|-------------|
| `--artifact` | | `os/arch:path` mapping, repeatable |

**Build mode** (`--package` or `--mod-dir` infers this mode)

| Flag | Default | Description |
|------|---------|-------------|
| `--package` | `.` | Go package path to build |
| `--mod-dir` | `.` | Directory containing `go.mod` |
| `--ldflags` | `-s` | Go linker flags |

---

## npm — publish to npm

Builds platform-specific npm packages wrapping Go binaries and publishes them via the `npm` CLI. The publish model follows the pattern used by [esbuild](https://www.npmjs.com/package/esbuild):

- One **platform package** per binary (`@org/name-linux-x64`, `@org/name-darwin-arm64`, etc.) with `os` and `cpu` fields so npm installs the right one automatically.
- One **root coordinator package** (`name`) listing all platform packages as `optionalDependencies`, plus a Node.js wrapper that resolves and `execFileSync`s the correct binary.

### Examples

**Publish all platforms:**

```sh
gowheels npm \
  --name mytool \
  --org myorg \
  --artifact linux/amd64:dist/mytool_Linux_x86_64/mytool \
  --artifact linux/arm64:dist/mytool_Linux_arm64/mytool \
  --artifact darwin/amd64:dist/mytool_Darwin_x86_64/mytool \
  --artifact darwin/arm64:dist/mytool_Darwin_arm64/mytool \
  --artifact windows/amd64:dist/mytool_Windows_x86_64/mytool.exe \
  --artifact windows/arm64:dist/mytool_Windows_arm64/mytool.exe \
  --summary "My awesome tool" \
  --license MIT \
  --version 1.2.3
```

This publishes seven packages:

```
@myorg/mytool-linux-x64
@myorg/mytool-linux-arm64
@myorg/mytool-darwin-x64
@myorg/mytool-darwin-arm64
@myorg/mytool-win32-x64
@myorg/mytool-win32-arm64
mytool               ← root coordinator, installed by: npm install -g mytool
```

After the platform packages are published, gowheels polls the registry concurrently to confirm propagation before publishing the root package.

### npm authentication

`npm` must be installed and on `PATH`. Authenticate before running gowheels:

- **`NODE_AUTH_TOKEN` environment variable** — an [npm access token](https://docs.npmjs.com/creating-and-viewing-access-tokens) with publish permission for the org. Set this in CI.
- **`npm login`** — interactive login that writes credentials to `~/.npmrc`. Use this locally.

Publishing to an org scope (`--org`) requires the token to have write access to that org, or for the package to already exist with your account as a maintainer.

### GitHub Actions workflow

```yaml
on:
  push:
    tags: ['v*']

jobs:
  publish-npm:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: stable }
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          registry-url: 'https://registry.npmjs.org'
      - run: go install github.com/StevenACoffman/gowheels@latest
      - name: Publish to npm
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
        run: |
          gowheels npm \
            --name mytool \
            --org myorg \
            --artifact linux/amd64:dist/mytool_Linux_x86_64/mytool \
            --artifact linux/arm64:dist/mytool_Linux_arm64/mytool \
            --artifact darwin/amd64:dist/mytool_Darwin_x86_64/mytool \
            --artifact darwin/arm64:dist/mytool_Darwin_arm64/mytool \
            --artifact windows/amd64:dist/mytool_Windows_x86_64/mytool.exe \
            --version ${{ github.ref_name }}
```

### All flags

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | *(required)* | Binary name and root package name |
| `--org` | *(required)* | npm org scope; produces `@org/name-linux-x64` etc. |
| `--artifact` | *(required)* | `os/arch:path` mapping, repeatable |
| `--version` | git tag | Package version |
| `--summary` | | One-line description for `package.json` |
| `--license` | | License identifier, e.g. `MIT` |
| `--tag` | `latest` | npm dist-tag to publish under |
| `--provenance` | | Publish with npm provenance attestation (requires CI) |
| `--repository` | | Repository URL for `package.json` |
| `--readme` | | Path to README copied into the root package |
| `--no-readme` | | Disable README inclusion |

---

## Version strings

Both subcommands accept semver or PEP 440 strings via `--version`. A leading `v` is stripped automatically, including from git tags. Semver pre-release labels are converted to their PEP 440 equivalents:

| Input | Normalized output |
|-------|-------------------|
| `v1.2.3` | `1.2.3` |
| `v1.2.3-alpha.1` | `1.2.3a1` |
| `v1.2.3-beta.2` | `1.2.3b2` |
| `v1.2.3-rc.1` | `1.2.3rc1` |
| `v1.2.3-dev.0` | `1.2.3.dev0` |
| `1.2.3a1` | `1.2.3a1` *(passed through)* |

When `--version` is omitted, gowheels runs `git describe --tags --exact-match` and normalizes the result. This fails (with a helpful error) if the current commit is not exactly on a tag — pass `--version` explicitly in that case.

---

## Troubleshooting

**`go install` gives "no Go files in …"**
You need Go 1.23+. Run `go version` to check.

**PyPI upload fails with 403 Forbidden**
The package name may already be registered by another account, or your token doesn't have upload scope for it. Check [pypi.org/manage/account/token](https://pypi.org/manage/account/token/).

**PyPI upload fails with 400 / "invalid wheel filename"**
This usually means the version string is malformed. Run with `--debug` or `--dry-run` to see the normalized version, then check the [version strings](#version-strings) table above.

**npm publish fails with E403**
Your `NODE_AUTH_TOKEN` doesn't have write access to the org scope. Verify the token has the `read and write` permission for the `@myorg` scope on npmjs.com.

**npm root package fails because platform packages aren't visible yet**
gowheels polls the registry for each platform package before publishing the root, with a 2-minute timeout. If the poll times out, re-run the command — npm publish is idempotent for the platform packages (version-already-exists errors are treated as success) and the root package will be retried.

**`--mode` is required / "cannot infer --mode"**
You must supply exactly one of: `--repo` (release mode), `--artifact` (local mode), or `--package`/`--mod-dir` (build mode). Supplying flags from multiple modes at once is an error.

---

## How the Python wheel works

Each wheel is a ZIP archive with Store compression (no deflate, as required by the wheel spec for non-library wheels) structured as:

```
{name}/
  __init__.py       # launcher: resolves binary, fixes chmod, os.execv on Unix / subprocess.run on Windows
  __main__.py       # enables: python -m {name}
  bin/
    {name}          # the Go binary, mode 0755
{name}-{version}.dist-info/
  METADATA          # Metadata-Version 2.4, License-Expression (SPDX), Requires-Python >=3.9
  WHEEL             # Wheel-Version 1.0, Root-Is-Purelib: false
  entry_points.txt  # [console_scripts] {name} = {name}:main
  RECORD            # sha256 hashes of every entry, in alphabetical order
```

On Unix, the launcher calls `os.execv` (replaces the Python process entirely — no wrapper overhead, signals work correctly). On Windows it uses `subprocess.run`. If the executable bit was stripped during installation, the launcher restores it before exec.

## How the npm package works

The root package's `bin/{name}` is a Node.js script that uses `require.resolve` to find the binary in whichever platform package npm installed, then calls `execFileSync` with `stdio: 'inherit'`. This means the process behaves identically to running the binary directly — exit codes, signals, and stdio all pass through unchanged.

Platform packages carry only `bin/{name}[.exe]` and a `package.json` with `os` and `cpu` fields. npm's optional dependency resolution installs exactly one per machine and skips the rest silently.
