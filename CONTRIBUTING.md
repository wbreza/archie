# Contributing to Archie

[Overview](README.md) | [Getting started](docs/getting-started.md) | [CLI contract](docs/contract-v1.md)

Contributions to documentation, examples, tests, and the CLI are welcome.
For a bug report, include the Archie version, OS, command, exit code, and a
minimal example with sensitive repository data removed. For a substantial
behavior change, describe the problem and proposed scope in an
[issue](https://github.com/wbreza/archie/issues) before building it.

## Build from source

Requires Go 1.25+ and Git. Clone the repository and enter it:

```sh
git clone https://github.com/wbreza/archie.git
cd archie
```

Then, in PowerShell:

```powershell
go build -o archie.exe ./cmd/archie
.\archie.exe version
```

In a POSIX shell, use `go build -o archie ./cmd/archie` and `./archie version`.
An unstamped source build reports `0.1.0-dev`, even at a release tag; see
[version reporting](docs/versioning.md#what-archie-version-reports).
Keep build output out of commits.

## Find your way around

Archie documents its own architecture in the root
[`archie.yaml`](https://github.com/wbreza/archie/blob/main/archie.yaml) and
component records beside the implementation. With a built or installed `archie`
on `PATH`, from the repository root:

```sh
archie context --root . --query "architecture of Archie CLI and bounded query/evidence pipeline" --max-records 5 --max-bytes 32768
archie get --root . --id query
archie discover --root .
```

Inspect coverage and continuation: a bounded packet is not necessarily the full
map. Follow source and test links before changing behavior. The main boundaries
are the CLI adapter, query pipeline, repository loader, shared Go contracts,
embedded JSON schemas, and the separate archive packager.

The [public contract](docs/contract-v1.md) defines command behavior, JSON,
resource limits, filesystem safety, and compatibility. Keep it and the schemas
aligned with changes; do not treat record descriptions as a substitute for it.

## Check a change

Format changed Go files with `gofmt`, then build, vet, and run tests:

```sh
go build ./...
go vet ./...
go test ./...
archie validate --root .
```

Tests use `ARCHIE_TEST_SCRATCH` when set to an existing directory for isolated
fixtures. Otherwise they create and remove fixture directories beneath each
test package, never the system temporary directory. On Windows, symlink tests
may require Developer Mode or elevation; unsupported filesystem cases report
explicit skips.

Add regression coverage for behavior changes. For documentation changes, check
examples against the CLI and check links from both the repository and distributed
archives. The packager explicitly lists bundled docs; update that list when
adding linked guides.

## Submit a focused pull request

Explain the user-visible change, link the relevant issue, and describe how you
verified it. Keep unrelated cleanup separate. Update affected documentation and
architecture records alongside the implementation, preserving record IDs and
grounding descriptions in the source.

Changes to public field meanings, required fields, or response shapes need an
explicit compatibility decision. Local source changes do not authorize a
release or a new tag.

For packaging, reproducibility caveats, and opt-in performance workloads, see
[build and measurement](docs/build-and-measure.md). Maintainers should follow
[versioning and manual releases](docs/versioning.md), including approval of the
exact release version and commit.
