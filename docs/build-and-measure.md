# Local builds, packaging and measurement

[Overview](../README.md) | [Getting started](getting-started.md) | [Contributing](../CONTRIBUTING.md)

For published binaries or a pinned Go install, use
[getting started](getting-started.md). This page is for contributors building
from source, creating local archives, or running opt-in measurements.

Go 1.25+ is required. Use standard Go commands from the repository root:

```powershell
go build -trimpath -buildvcs=false -ldflags "-X github.com/wbreza/archie/internal/query.BuildVersion=0.1.0-dev" -o archie.exe ./cmd/archie
.\archie.exe version
go test ./...
go vet ./...
```

Local source builds without an override report `0.1.0-dev`. A versioned
`go install` instead reports Go's embedded module version, including its `v`
prefix; an explicit linker override always wins. `api_version` and authored
`schema_version` remain `"1"` independently of the build version. See
[Go versions and manual releases](versioning.md) for precedence, release
compatibility, conditional private-module access, and immutable tags.

## Packaging (no release or publication)

```powershell
# C:\artifacts must already exist; select an unused version/output combination.
go run ./tools/package -version 0.1.0-dev -out C:\artifacts
```

This creates a new `archie_0.1.0-dev` directory, refusing existing output. It
cross-compiles CGO-free binaries for Windows, Linux and macOS, each for amd64 and
arm64. Windows uses ZIP; Unix uses tar.gz with executable permissions. Each
archive contains the binary, README, CONTRIBUTING, getting-started and usage
guides, contract/build/versioning documentation, and three schemas. Linked
source files remain online in the repository. `SHA256SUMS` covers the archives.
Only a uniquely created `.build-*`
child is removed; interrupted/failed output is retained for inspection.

No registry tool, credentials, signing, release creation, upload or publication
is involved. Archives have fixed timestamps; binary reproducibility still
depends on matching source, dependency and Go toolchain versions. Cross-compiling
or inspecting an archive is **not native execution**. Run tests and extract/run
the corresponding archive on each supported OS/architecture before claiming
native acceptance. Windows may require Developer Mode/elevation for symlink
tests; unsupported filesystem cases report explicit skips.

## Representative full-process latency

The opt-in existing-Go test harness creates **10,000 files, including 500
descriptors**, spread across 100 application areas. Records contain descriptive
summaries, reusable links and selected source pointers. It measures actual CLI
process startup, discovery/strict parsing, selection and selected evidence checks,
not an in-process approximation. It includes a startup-only `version` comparison.

```powershell
# Use an existing, dedicated scratch directory outside application metadata.
$env:ARCHIE_TEST_SCRATCH = 'C:\artifacts\measurement'
$env:ARCHIE_MEASURE = '1'
$env:ARCHIE_MEASURE_REPORT = 'C:\artifacts\measurement\latency.json'
go test -count=1 -timeout 30m -run '^TestRepresentativeProcessWorkload$' -v ./internal/cli
```

The report path must not exist. It is retained; uniquely created fixture children
are cleaned up. The default test suite skips the expensive measurement. Each
operation records one initial invocation and 20 subsequent fresh-process
observations with min/p50/p95/max, stdout bytes and exit/coverage checks.
The first discover invocation is the first **CLI filesystem pass** over the
freshly written fixture; subsequent commands and repetitions are filesystem-warm.
Fixture creation itself warms caches. No cache eviction is attempted and none
of these measurements is called “cold cache.” The process is new on every sample.

For evidence-enabled commands, no baseline is supplied and inspected evidence is
honestly `unknown/no_baseline`. Discovery does not check evidence. Bounded
validation and discovery can be partial. Reports identify platform/toolchain,
workload and options. Results are observations, not thresholds, capacity
guarantees, comparative retrieval-quality evaluations or native cross-platform
performance claims.

## Complete-reference validation workload

The focused fixture models **37 descriptors and 493 distinct referenced files**,
including source, test, doc and ADR links plus shared references. Plain validation
checks every target without reading content or emitting a success row per link.
The regular regression suite also removes the final target and asserts failure
after the historical 256-target and detailed-output cutoffs.

```powershell
$env:ARCHIE_TEST_SCRATCH = 'C:\artifacts\measurement'
$env:ARCHIE_MEASURE_VALIDATION = '1'
go test -count=1 -run '^TestValidationProcessMeasurement$' -v ./internal/cli
go test -run '^$' -bench '^BenchmarkValidateReferences493$' -benchmem -benchtime=3x ./internal/query
```

The native harness records one first invocation and ten subsequent fresh
processes; p50/p95 use nearest rank. Fixture setup, binary build and JSON
validation are outside process timing. Fixture creation warms the filesystem;
there is no cache eviction. The in-process benchmark includes graph loading and
response generation and reports cumulative allocation, **not peak resident
memory**. Both require complete 493-target coverage, not merely successful
process startup or ID enumeration.

Observed on Windows amd64, Go 1.26.1, Intel Core i9-13900K: native first invocation
1,208 ms; subsequent min/p50/p95/max 1,163/1,202/2,026/2,026 ms; stdout 514 bytes.
The three-iteration in-process benchmark measured 1,362 ms/op and 11,270,032
allocated bytes/op. Before invocation-local directory-listing reuse, the same
in-process fixture allocated 57,606,336 bytes/op. Listings remain bounded to
20,000 entries and are never reused across invocations or observed directory
changes. These are local observations under a shared-machine workload, not
latency thresholds, peak-memory bounds or a comparison with a real application's
different directory structure.
