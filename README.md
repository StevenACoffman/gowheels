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

| Flag | Env var | Description |
|------|---------|-------------|
| `--dry-run` | `GOWHEELS_DRY_RUN` | Print what would happen without writing files or uploading |
| `--debug` | `GOWHEELS_DEBUG` | Enable debug-level structured logging |

## Environment variables

Every flag can be set via an environment variable. The mapping rule is: prepend `GOWHEELS_`, uppercase the flag name, and replace dashes with underscores. Flags supplied on the command line always take precedence over environment variables.

The most commonly useful env vars:

| Env var | Flag | Notes |
|---------|------|-------|
| `GOWHEELS_PYPI_TOKEN` | `--pypi-token` | PyPI API token; preferred over OIDC for local runs |
| `GOWHEELS_GITHUB_TOKEN` | `--github-token` | GitHub token; avoids API rate limits in release mode. `GITHUB_TOKEN` is also read as a fallback (set automatically by GitHub Actions) |
| `GOWHEELS_PYPI_URL` | `--pypi-url` | Override the PyPI upload endpoint (e.g. TestPyPI) |
| `GOWHEELS_VERSION` | `--version` | Useful in CI to avoid running `git describe` |
| `GOWHEELS_DRY_RUN` | `--dry-run` | Set to any non-empty value to enable dry-run mode |
| `GOWHEELS_DEBUG` | `--debug` | Set to any non-empty value to enable debug logging |

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

### Metadata auto-population from GitHub

When `--repo owner/name` is set, gowheels calls the GitHub API before building wheels and uses the repository metadata to fill in fields that were not provided explicitly:

| Wheel field | Source | When applied |
|-------------|--------|--------------|
| `Summary` | GitHub repository description | Only if `--summary` is not set |
| `License-Expression` | GitHub detected SPDX identifier | Only if `--license-expr` is not set |
| `Project-URL: Repository` | GitHub `html_url` | Only if `--url` is not set and not found in `go.mod` |
| `Keywords` | GitHub repository topics | Always (no override flag for `pypi`) |

The GitHub API call is best-effort — a failure (network error, rate limit, empty description) is logged as a warning and does not abort the build. Pass `--debug` to see the full API response.

**README auto-detection:** gowheels reads the first of `README.md`, `README.rst`, `README.txt`, `README` found in the current working directory and embeds it as the wheel's long description. Run from the repository root, or pass `--readme <path>` explicitly.

> **Important:** PyPI does not allow updating metadata after a version is published. If a wheel was uploaded with an empty summary or no README, the only options are to delete the release on PyPI and re-upload, or to publish a new version. Run `gowheels pypidiff` to compare local metadata against what is live on PyPI before and after uploading.

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

1. **`--pypi-token` flag or `GOWHEELS_PYPI_TOKEN` env var** — a [PyPI API token](https://pypi.org/manage/account/token/) with upload scope for the package. The env var is read automatically; no `--pypi-token` flag is needed when `GOWHEELS_PYPI_TOKEN` is set.
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
      id-token: write   # enables PyPI OIDC — no GOWHEELS_PYPI_TOKEN secret required
      contents: read
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: stable }
      - run: go install github.com/StevenACoffman/gowheels@latest
      - name: Publish wheels to PyPI
        env:
          GOWHEELS_GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
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
  GOWHEELS_PYPI_TOKEN: ${{ secrets.PYPI_TOKEN }}
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
| `--summary` | GitHub description | One-line package description (auto-populated from GitHub when `--repo` is set) |
| `--license-expr` | GitHub SPDX / `MIT` | SPDX license expression (auto-populated from GitHub when `--repo` is set) |
| `--license` | | Path to license file; bundled as `dist-info/licenses/LICENSE.txt` |
| `--readme` | auto-detect | Path to README (auto-detects `README.md`, `.rst`, `.txt` in current directory) |
| `--no-readme` | | Disable README auto-detection |
| `--url` | go.mod / GitHub | Repository URL added as `Project-URL` in METADATA |

**Build and output**

| Flag | Default | Description |
|------|---------|-------------|
| `--platforms` | all | Comma-separated `os/arch` filter, e.g. `linux/amd64,darwin/arm64` |
| `--output` | `dist` | Output directory for `.whl` files |
| `--upload` | | Upload wheels to PyPI after building |
| `--pypi-url` | PyPI | PyPI upload endpoint (for TestPyPI: `https://test.pypi.org/legacy/`); env var: `GOWHEELS_PYPI_URL` |
| `--pypi-token` | | PyPI API token; env var: `GOWHEELS_PYPI_TOKEN` (preferred in CI) |
| `--mode` | inferred | Binary source: `release`, `local`, or `build` |

**Release mode** (`--repo` infers this mode)

| Flag | Default | Description |
|------|---------|-------------|
| `--repo` | | GitHub repository in `owner/name` format |
| `--assets` | | Comma-separated explicit asset name overrides |
| `--cache` | `$XDG_CACHE_HOME/gowheels` | Local cache directory for downloaded archives |
| `--github-token` | | GitHub personal access token; env vars: `GOWHEELS_GITHUB_TOKEN`, `GITHUB_TOKEN` (fallback) |

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

## pypidiff — compare local metadata against PyPI

Assembles the same metadata that `gowheels pypi` would write, fetches the current metadata from PyPI's JSON API, and reports any field-level differences. Useful for verifying a release before uploading, diagnosing why a PyPI project page looks wrong, or confirming that metadata was correctly updated after re-publishing.

Exit codes: `0` no differences, `1` at least one field differs, `2` invocation error.

### Examples

**Check whether the current directory's metadata matches the latest PyPI release:**

```sh
gowheels pypidiff --name mytool --repo owner/mytool
```

**Compare against a specific version:**

```sh
gowheels pypidiff --name mytool --repo owner/mytool --version 1.2.3
```

**When the PyPI package name differs from the binary name:**

```sh
gowheels pypidiff --name mytool --package-name mytool-bin --repo owner/mytool
```

**Use in CI to assert metadata is correct after publishing:**

```sh
gowheels pypi --name mytool --repo owner/mytool --upload
gowheels pypidiff --name mytool --repo owner/mytool   # exits 1 if anything differs
```

### Fields compared

| Field | Local source | Notes |
|-------|-------------|-------|
| `Summary` | `--summary` or GitHub description | |
| `License-Expression` | `--license-expr` or GitHub SPDX | |
| `Keywords` | GitHub topics | |
| `Requires-Python` | hardcoded `>=3.9` | |
| `Classifiers` | auto-generated (Development Status, Environment, Programming Language) | OS classifiers excluded — they depend on build targets and cannot be predicted |
| `Project-URLs` | go.mod + GitHub; key comparison is case-insensitive | |
| `Description` | README file (presence, content-type, byte-count) | Full content is not diffed — use a dedicated diff tool if exact content matters |

### All flags

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | *(required)* | Binary name / PyPI package name |
| `--package-name` | `--name` | Python package name on PyPI when it differs from binary name |
| `--summary` | GitHub description | One-line description (auto-populated from GitHub when `--repo` is set) |
| `--license-expr` | GitHub SPDX | SPDX license expression |
| `--url` | go.mod / GitHub | Project URL |
| `--readme` | auto-detect | Path to README for description comparison |
| `--no-readme` | | Treat local long description as absent |
| `--version` | latest | PyPI release version to compare against |
| `--repo` | | GitHub repo (`owner/name`) for metadata auto-population |
| `--github-token` | | GitHub personal access token; env vars: `GOWHEELS_GITHUB_TOKEN`, `GITHUB_TOKEN` |
| `--mod-dir` | `.` | Directory containing `go.mod` for URL auto-detection |

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
| `--readme` | auto-detect | Path to README copied into the root package |
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

**Summary, README, or keywords are empty on the PyPI project page**

These fields are written into the wheel at build time — they cannot be updated after a version is published. Common causes:

- `--summary` was not set and `--repo` was not set (or the GitHub repository description is blank). Run with `--debug` to see which fields were auto-populated.
- The README file was not found. gowheels looks for `README.md`, `README.rst`, `README.txt`, and `README` in the *current working directory* when the command is run. Run from the repository root, or use `--readme <path>`.
- A previous upload of the same version number was made without the metadata. PyPI does not allow re-uploading the same version. Delete the release at `https://pypi.org/manage/project/<name>/releases/` and re-upload, or bump the version.

Use `gowheels pypidiff --name <name> --repo owner/name` to compare what would be uploaded against what is currently on PyPI.

**PyPI upload fails with 409 / "version already exists"**
The exact version has already been published. PyPI does not allow overwriting a release or updating its metadata in place. Options: delete the release via the PyPI web interface and re-upload, or publish a new version number.

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

**GitHub API rate limit hit**
Without a token, the GitHub API allows 60 requests per hour per IP. Set `GOWHEELS_GITHUB_TOKEN` or `GITHUB_TOKEN` to a personal access token (no special scopes needed for public repos) to raise the limit to 5,000 per hour.

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

## Other commands

**`gowheels man`** — prints the man page for gowheels in roff format to stdout. Pipe to the pager with `gowheels man | man -l -`. Use `--section N` to set the man page section (default: 1).

**`gowheels version`** — prints the gowheels version and the Go toolchain version it was built with.
