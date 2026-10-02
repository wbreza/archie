# Getting started with Archie

[Overview](../README.md) | [Usage guide](usage.md) | [Contributing](../CONTRIBUTING.md)

This guide gets you from an installed CLI to your first architecture record.
Archie reads knowledge you author; installing it does not scan source code and
generate an architecture for you.

## Install a release binary

1. Open the [releases page](https://github.com/wbreza/archie/releases) and choose
   a release. These examples use the published **v0.2.0** release.
2. Download the archive matching your OS and CPU: Windows, Linux, or macOS;
   `amd64` for x86-64, or `arm64` for ARM64 (including Apple silicon).
3. Compare the archive's SHA-256 hash with its entry in the release's
   `SHA256SUMS` file, then extract it to a directory you choose.
4. Run the extracted executable directly or add that directory to your `PATH`.
   Choose a fresh directory if you want to keep an existing install.

For example, in PowerShell, use `Get-FileHash -Algorithm SHA256` on the downloaded
archive. On Linux use `sha256sum`; on macOS use `shasum -a 256`. These commands
print the hash to compare with `SHA256SUMS`.

From the extracted directory on Windows:

```powershell
.\archie.exe version
```

On Linux or macOS:

```sh
./archie version
```

The `version` command returns JSON. A packaged v0.2.0 binary reports
`data.version: "0.2.0"` and `api_version: "1"`. The API generation is separate
from the CLI release version.

Go is **not required** for these binaries. Archives are built for all six
OS/architecture combinations; cross-compilation alone does not establish native
execution on every target. See the release notes for platform verification.

## Install with Go instead

Requires **Go 1.25 or later** and Git. The repository is public; no GitHub login
or private-module configuration is required for a normal public install.

```sh
go install github.com/wbreza/archie/cmd/archie@v0.2.0
```

Go writes the executable to `GOBIN` when set, otherwise the `bin` directory under
`GOPATH`. Check these settings with `go env GOBIN GOPATH`. Add the resulting
directory to your `PATH`, or invoke its executable by its full path. Installation
may replace an existing `archie` binary in that directory.

With the directory on `PATH`:

```sh
archie version
```

This tagged Go install reports `data.version: "v0.2.0"`, including the `v`,
and `api_version: "1"`. If another version appears, check which executable your
shell resolves (`Get-Command archie` in PowerShell or `command -v archie` in a
POSIX shell).

Pin a release when upgrading rather than treating `@main` or `@latest` as a
stability promise. Read its release notes for compatibility changes. See
[versioning](versioning.md) for build provenance and
[contributing](../CONTRIBUTING.md) to build from a checkout.

## Create your first record

The following commands work in PowerShell or a POSIX shell with `archie` on
`PATH`. Start in an **existing demo directory without an `archie.yaml`**:

```sh
archie scaffold --root . --file archie.yaml --id reports --name "Report service" --summary "Generates and delivers customer reports."
archie context --root . --query "customer reports"
archie validate --root .
```

Scaffolding creates one file with these fields:

```yaml
schema_version: "1"
id: reports
name: Report service
summary: Generates and delivers customer reports.
```

`context` returns that record inside `data.records` in its JSON response.
`validate` checks the metadata and file references. The minimal record has no
file references, so its reference count is zero; it is not a description of
your whole implementation yet.

For a real application, replace the example ID, name, and summary with
descriptions grounded in your code. Add records for important components and
link them to existing source files, tests, and decisions as shown in the
[usage guide](usage.md#describe-a-component).

## Use an existing architecture map

If your project already contains records, do not recreate them. From its root:

```sh
archie discover --root .
archie context --root . --query "customer reports"
archie get --root . --id reports
```

Replace the query and ID with a topic and an ID from your project.
`discover` enumerates records; `get` retrieves one record in detail.

`--root` is the application boundary, not a request to search upward for a
repository root. It defaults to the current directory. Commands that read
metadata require `archie.yaml` at that boundary, even if other records exist
in subdirectories.

## Common first-run problems

| Symptom | What to check |
| --- | --- |
| `archie` is not recognized | Add its binary directory to `PATH`, or invoke the executable by its full path. PowerShell requires `.\archie.exe` for an executable in the current directory. |
| Root metadata is missing | Use the correct `--root`, or scaffold a root record if the project has not adopted Archie. |
| Scaffold refuses to create a file | It never overwrites a file or creates parent directories. Use an existing root/parent and a new discoverable filename; non-root records also require valid existing metadata and an unused ID. |
| Query returns no relevant component | Search words present in the authored records, or use a known `--id` or `--path`. No match does not mean no implementation exists. |
| Evidence says `unknown` / `no_baseline` | No explicit Git commit was supplied for comparison. This is expected, not a claim that the file is unchanged. |
| Command returns partial results | Inspect `coverage`, `diagnostics`, and `continuation`. Follow the [usage guide](usage.md#read-json-results-and-partial-coverage), rather than treating partial output as complete. |

For complete flag, exit-code, filesystem, and output rules, use the
[CLI contract](contract-v1.md).
