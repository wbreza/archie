# Archie v1 public contract

This is the shared contract for the W2–W4 runtime and W5 consumers. The runtime
supports read commands, explicit scaffolding, local packaging and measurements.
The approved scope is local, deterministic, read-only and cache-free, except
for explicit non-overwriting `scaffold`. No descriptor is executable instruction.

## Authority and compatibility

`schemas/record.schema.json` is the single authored-record definition.
`schemas/root.schema.json` extends it only for repository-root `archie.yaml`.
`schemas/response.schema.json` defines the separate JSON API. All use JSON
Schema draft 2020-12. IDs are stable resource identifiers, not fetched URLs;
the Go embedding compiles only bundled resources. Runtime must not download them.
Both `schema_version` and `api_version` are the string `"1"`, never numeric 1.
Unknown fields fail at every object boundary. Changing a field's meaning,
requiredness, enum or shape requires an explicit compatibility decision before
release; consumers must not maintain their own YAML schema.

Authored records require `schema_version`, `id`, `name`, `summary`. Optional:
`kind`, `tags`, `description`, `rationale`, `links`. `kind` and tags are open
descriptive strings, not a language taxonomy. Omit absent optional values:
null and blank strings are invalid. IDs are case-sensitive ASCII, 1–128
characters, starting alphanumeric, followed by alphanumerics, `.`, `_`, `-`.

Every link has exactly `relation`, `target`, `description`. `depends_on` and
`related_to` target record IDs. `source`, `test`, `doc`, `adr` target literal
descriptor-relative files. `parent` and `child` are read-only derived links.
No `operations`, `actions`, parameters, child envelopes or multi-record lists.
Descriptions and rationale are authored claims, not verified facts. ADR status,
supersession and authority remain in the ADR, never inferred by this contract.

## Discovery, YAML and path semantics (W2 obligations)

* `--root` defaults to the current directory, never a search up the directory
  tree. It is an explicit application boundary, not necessarily a Git root.
  Root `archie.yaml` is mandatory except for `version` and root scaffolding.
* Discover exact lowercase `archie.yaml` and nonempty-prefix `*.archie.yaml`
  names, recursively. Do not follow descriptor, file or directory symlinks,
  junctions or other reparse points, even when their destination is inside root.
  Descriptor and referenced regular files must have exactly one hard link;
  hard-linked aliases cannot establish unique repository containment.
* Root-only `discovery.exclude` is a list of literal root-relative file or
  directory paths (directory entries exclude the subtree). No globs, negation
  or nested inheritance. Excluding `archie.yaml` or `.` is invalid.
* Always prune directory basenames `.git`, `.hg`, `.svn`, `node_modules`,
  `vendor`, `dist`, `build`, `coverage`, `.next`, `.venv`, `__pycache__`.
  Prune descendant repositories with a `.git` file/directory (not the root's).
  Ordinary monorepo package directories remain eligible. Do **not** interpret
  `.gitignore`, global ignores or attributes: machine/user configuration must
  not silently change architectural discovery. Additional generated locations
  require explicit root exclusions.
* Enumeration is repository-relative UTF-8 byte lexical order, case-sensitive
  even on Windows. At most 20,000 visited entries and 2,000 descriptors.
  A pruned directory counts as one excluded boundary; descendants are not
  counted or scanned. No claim is made about the number of hidden records.
* UTF-8 YAML, exactly one mapping document, no BOM, duplicate keys (at any
  depth), non-string keys, aliases, anchors, merge keys, custom/explicit tags,
  directives or extra document. Implicit plain booleans/numbers remain their
  YAML types and fail string fields; a plain date remains a string. Reject
  invalid UTF-8. No expansion. Limits per descriptor: 65,536 raw bytes,
  4,096 nodes including keys/document node, depth 32 including document node.
  Enforce raw byte limit before parsing and parser limits before decoding.
* Schema validation is followed by semantic identity/path checks. Duplicate
  IDs are fatal globally, never overridden. Self-links and semantic cycles
  are allowed; traversal uses visited IDs. Unresolved record IDs are invalid.
* A directory `archie.yaml` parents to the nearest strict ancestor directory
  `archie.yaml`. A named record parents to the same-directory `archie.yaml`,
  otherwise the nearest ancestor. Named siblings never parent one another.
  Root has no parent. Moving a descriptor preserves its ID, not its paths.
* Authored file paths use `/` on every OS. No URLs, anchors, globs, backslashes,
  absolute/drive/UNC paths, control characters, NTFS streams or empty segments.
  Literal `.` and `..` are permitted for file links only when normalization
  remains inside root. Exclusions, selectors and scaffold output are normalized
  root-relative paths with no `.`/`..` segments (`--path .` alone means root).
  Reject Windows device names, trailing dots/spaces, and case mismatches on all
  platforms. Walk each existing component without following symlinks/reparse
  points, verify exact spelling, and reject escape before reading content.
  Missing safe file targets are evidence `missing`, not schema errors.
  File links into excluded/nested-repository boundaries are not read.
* Global `validate` fails on every structural/semantic error. A scoped query
  can return partial for invalid *non-root* descriptors; root errors, duplicate
  identity, unsafe paths, case ambiguity or incomplete discovery that could
  hide duplicate identity fail closed with no records. Exclusions are explicit
  policy, not discovery incompleteness. Metadata limits are fatal; do not
  publish a seemingly unambiguous partial graph.

## Exact CLI surface

All commands emit one JSON envelope to stdout by default, also with `--json`.
UTF-8, compact JSON, one trailing LF, no BOM/ANSI/logging. Help (`--help`) is
plain text, exit 0; no JSON claim for help. No human output format in v1.
Unrecognized/duplicate singleton flags, positional extras and incompatible
flags are `USAGE`. Flags follow the subcommand; `--flag=value` and
`--flag value` are equivalent. Repeatable flags combine/deduplicate.

| Command | Arguments and purpose |
| --- | --- |
| `archie discover` | Enumerate valid record views, no evidence hydration. |
| `archie get --id ID` | Exactly one record, shallow derived links, no ancestors. |
| `archie context --query TEXT` | Bounded orientation; `--query` may be omitted if at least one selector is present. |
| `archie impact --path PATH` | Known records whose descriptors or canonical file links intersect paths; repeat `--path`. No exhaustive impact claim. |
| `archie validate` | Global metadata/link checks; optional `--evidence` enables bounded file checks. |
| `archie scaffold --file PATH --id ID --name NAME --summary TEXT` | Create one minimal descriptor exclusively. |
| `archie version` | Build version in the common envelope; no repository scan. |

Common: `--root DIR`, `--json` (version accepts only `--json`).
`discover/context/impact`: `--max-records`, `--max-links`, `--max-bytes`,
`--cursor`. `get`: `--max-links`, `--max-bytes`.
`context`: optional repeatable `--keyword WORD`, `--path PATH`, `--id ID`.
Each `--keyword` is one token; query tokens may contain whitespace.
Query text is at most 4,096 Unicode scalar values; at most 64 occurrences of
each repeatable flag; selectors/file arguments are at most 1,024 characters.
`get/context/impact`: `--baseline COMMIT`, `--max-evidence`,
`--max-evidence-bytes`; evidence checks are on by default.
`validate`: same evidence/baseline limits only with `--evidence`.
`scaffold`: no overwrite/force option, no implicit parent directory creation,
and filename must match discovery patterns. Root must exist and be safe;
root scaffolding is possible without metadata. Non-root scaffolding requires
a valid root/graph and unused ID. Validate the proposed document before opening
with exclusive create; if any file already exists, leave it unchanged.
Scaffold output must be inside discoverable scope, including exclusions naming
not-yet-existing files. It writes only the four required descriptive fields;
schema text limits apply (name 256, summary 1,024 Unicode scalar values).
Success returns `data:{"path":"<canonical descriptor>"}`. Failure does not
remove any file: an I/O failure after exclusive creation can leave a partial new
file requiring inspection before retry. Creation is exclusive through the original
repository root (not a descendant root handle that could move outside it), with
parent identity rechecks. The root is a directory identity, not a pathname lock;
there is no atomic filesystem snapshot against concurrent arbitrary renames.
Graph validation plus creation is not a multi-process transaction:
concurrent writers to different descriptors still require fresh validation for
duplicate IDs. No lock files, rollback deletion, force flags or implicit mkdir.
Read commands create no files, persistent cursors, indexes, databases or caches.
`version` returns `data:{"version":"<build version>"}`: a nonempty linker
override, otherwise the installed Go module version verbatim, otherwise
`0.1.0-dev` for local source builds. Schema/API version remains `"1"`.
See [versioning](versioning.md) for precedence and immutable Go tags (first
`v0.1.0` pending publication), and [build and measurement](build-and-measure.md)
for linker injection and local Windows/Linux/macOS archives (no publication).

## Deterministic selection, budgets and continuation (W3 obligations)

| Limit | Default | Allowed override |
| --- | ---: | ---: |
| Records per page | 10 | 1–100 |
| Navigation + reference links per returned record | 32 | 0–256 |
| Entire stdout JSON including LF (bytes) | 32768 | 4096–1048576 |
| Distinct selected evidence targets inspected | 16 | 0–256 |
| Current + baseline evidence bytes combined | 1048576 | 0–16777216 |

Never truncate authored fields or remove authored links from the nested record.
Exception: context records selected **only** for ancestor orientation carry
`summary_only:true` and project the four required fields without truncation.
Their full record remains available through `get`; ancestor file pointers in
read `links` retain the descriptor origin and receive normal selected checks.
The link limit bounds the unified read `links` collection (derived hierarchy,
explicit ID links and canonical file references),
not the authored record itself. If an entire record cannot fit even on an empty
page, omit it, report byte budget and return no repeating continuation for it.
Reserve envelope/diagnostic/coverage bytes before packing. Bound diagnostics to
16 entries, messages to 256 Unicode scalar values, omission path samples to 16;
aggregate further occurrences by code/reason in coverage, not silent loss.
`coverage.diagnostic_counts` counts occurrences by diagnostic code; omission
counts aggregate by reason. Path samples may be empty to reserve packet space.
No query-dependent timestamps, random IDs, absolute paths or timing fields.

Selection uses Unicode lowercase followed by maximal letter/digit tokens.
No stemming, fuzzy/semantic inference or language parsing. Count distinct token
matches against ID/name/tags at weight 3 and summary/description at weight 1
(once per token per group); rationale is not searched for inferred intent.
`--keyword` adds tokens; selectors are additive, not filters. ID selectors must
exist; path selectors may name missing/deleted files. Score exact-ID seeds
first, then path seeds (descriptor directory contains selector OR file reference
equals selector), then descending positive token score, then ID byte order.
No query match returns empty records (not even root), with no-implementation
claims prohibited. `discover` sorts by descriptor then ID. `impact` sorts by ID;
its path matches are exact file or directory-descendant matches on descriptor
or canonical file reference, not a general directory-ownership inference.

For context: starting with ordered seeds, insert ancestors root-first before
each seed, deduplicated; then append one hop of outgoing `depends_on/related_to`
targets sorted by ID. No recursive dependency expansion. `get` never adds
ancestors or related records. Derived navigation is parent first then children
by ID; canonical references sort by relation, target, description. Navigation
precedes explicit ID links and file references for link budgets; the latter
share the relation/target/description ordering. Reverse dependencies are calculated by
`impact` as one incoming hop from directly path-matched seeds, not persisted as
authored relations. Record reasons use the schema's enum, unique lexical order.

Plan all candidates from metadata once, deduplicate, pack whole records.
`selection.matched` is all planned unique candidates (including ancestors and
one-hop expansion), `returned` is this page, `omitted = matched - returned`
including earlier pages. `not_selected` reports valid records outside the
candidate set. Pagination does not pretend remaining records were inspected.
`metadata.discovered = loaded + invalid` when complete; excluded counts are
pruned boundaries/files, not hidden descriptors. `metadata.complete` concerns
enumeration, not validity or selection. `evidence.total = checked + unchecked`;
total is unique safe file targets from records on this page, even references
not previewed under the link budget. Repeated links share checks; each selected
record/relation/target still receives its own evidence entry.

Only `discover/context/impact` return continuation, and only for remaining
whole records that could fit a subsequent page. `continuation` is null otherwise.
The opaque cursor is unpadded base64url of compact UTF-8 JSON:
`{"v":"1","query":"<sha256>","snapshot":"<sha256>","offset":N}`.
`query` hashes canonical JSON of command, normalized sorted/deduplicated
selectors, token list, all effective budgets and resolved baseline (or requested
unavailable baseline); omit root's absolute path and cursor itself.
The exact preimage fields are `contract.QueryIdentity`: `baseline`, `command`,
`ids`, `limits`, `paths`, `tokens`. `limits` keys are `evidence_bytes`,
`evidence_targets`, `links`, `output_bytes`, `records`; unused evidence limits
are zero for discover. Baseline is `""` when not supplied. Empty collections
are arrays, never null. No trailing newline is included in either hash.
`snapshot` hashes the sorted sequence of all discovered descriptor paths + raw
bytes, exclusion boundaries and applicable discovery configuration, using
length-prefixed UTF-8 strings/bytes (unsigned 64-bit big-endian length).
No source bytes are included: evidence is always checked again on each page.
W3 must serialize canonical query object keys lexically, with compact JSON,
no HTML escaping, and UTF-8 characters literal. SHA-256 lowercase hex.
`offset` indexes the deduplicated candidate sequence, not returned records.
Reject malformed/extra keys/version/range/query mismatch as `CURSOR_INVALID`;
metadata snapshot mismatch as `CURSOR_STALE`. This is not an authorization token.
Never silently restart or store cursor state.

Intentional exclusion/not-selection alone is `ok`. Invalid localized records,
exhausted record/link/byte/evidence budgets and unavailable requested comparison
are `partial`. Partial requires diagnostics and exits 4. Empty no-match results
are `ok`. A cursor is not promised for link/evidence budgets or errors: repeat
with larger limits to inspect them. Limits and counts never imply full source
or semantic coverage.

## Explicit comparison baseline

`--baseline COMMIT` is the caller's **explicit attribution**: compare selected
evidence bytes in that Git commit with current working-tree bytes. It does not
assert metadata was reviewed at that commit. Accept only full lowercase 40- or
64-hex commit object IDs, not refs, revisions, options, paths or abbreviations.
No implicit HEAD/merge-base/mtime baseline. No fetching, Git mutation or optional
locks. Resolve objects only in the repository containing `--root`; map root's
prefix when it is a subdirectory. Reject baseline flags lexically as `USAGE`;
non-Git, absent object, non-commit and unavailable shallow history are reported
as unavailable, not automatically repaired.

Envelope baseline is always present:
`requested` input or `""`, `resolved` verified full commit ID or `""`,
`state` none/available/unavailable, `reason` from the schema.
An available commit still may lack a selected path: `unknown/not_in_baseline`.
Budget includes both sides' raw file bytes; no text conversions, filters,
smudge/clean commands, diff drivers, source execution or network access.
Check target existence and safe regular-file status before reading.
Available baseline compares current file bytes, including staged/unstaged and
untracked working-tree bytes; never substitute index/HEAD bytes for dirty files.
Concurrent changes detected during reading are `unknown/unattributable`.

| Evidence state | Meaning / allowed reason |
| --- | --- |
| `missing` | Safe current target absent, independent of baseline; reason `missing`. |
| `changed` | Both byte sequences available and different; `bytes_differ`. |
| `unchanged` | Both sequences equal; `bytes_equal`; not semantic correctness. |
| `unknown` | `no_baseline`, `baseline_unavailable`, `not_in_baseline`, `not_regular`, `read_error`, `unattributable`, `renamed`. |
| `unchecked` | No inspection: `budget` or `not_requested`. |

Renames do not silently retarget authored links: old missing paths stay missing;
new paths not in the baseline stay unknown. Rename knowledge may be reported as
unknown/renamed only when attributable from the supplied comparison. ADR links
use the same checks, so changed decision bytes are visible without asserting
their status. Missing or changed evidence is reported without claiming stale
architecture. Missing evidence makes query partial and evidence validation fail.
Changed evidence alone is informational/ok. Expected unknown/no_baseline does
not force partial; unavailable *requested* comparison/read failure does.

## JSON envelope and error/exit behavior

See `schemas/response.schema.json` and `contract/response.go`. `data.records`
contains `{record, descriptor, links, reasons}` and optional `summary_only:true`:
`record` retains the complete authored record with original relative targets;
`descriptor` is canonical root-relative origin. The **same** read `links` array
contains derived parent/child ID links, authored depends_on/related_to links,
and file links with canonical root-relative targets. Authored hierarchy remains
forbidden. Summary-only ancestors use the projection described above.
All arrays are `[]`, never null. Fatal `data` is null. A byte-exhausted `get`
may return zero records with `partial`; successful `get` returns exactly one.
Optional runtime `continuation` is explicit null when unavailable.

| Exit | Meaning |
| --- | --- |
| 0 | `status=ok`: successful operation, including no matches. |
| 1 | `status=error`: invalid metadata/paths/links, missing requested ID, missing evidence on validate. |
| 2 | `status=error`: usage or malformed/stale cursor. |
| 3 | `status=error`: I/O failure, existing scaffold output, internal failure. |
| 4 | `status=partial`: usable bounded result with explicit gaps; validate cannot claim valid. |

Diagnostic codes are schema-enumerated. `USAGE`, `CURSOR_INVALID`, `CURSOR_STALE`
map to 2; `IO_ERROR`, `ALREADY_EXISTS`, `INTERNAL` to 3; fatal data errors to 1.
For mixed failures, usage precedes operational failure then data invalidity.
Fatal takes precedence over partial. Global validation with invalid metadata
returns null data/error; otherwise bounded unfinished validation returns
`{valid:false,evidence:[...]}`/partial. Fully valid is `{valid:true,...}`/ok.
No stack traces/source bytes/secrets in diagnostics. On stdout write failure
exit 3; a valid JSON envelope cannot be guaranteed on a broken pipe.

## Fixtures and delivery boundary

`testdata/contract/records.json` is executed against the authored schemas.
`yaml.json` catalogs required W2 parser rejection inputs; `repositories.json`
catalogs W2/W3 discovery, hierarchy, identity, cycles, safe paths and exclusions;
`evidence.json` catalogs W3 bounded baseline/dirty/missing scenarios.
`response.json` is a complete bounded-context wire example.
W1 tests execute schema acceptance/rejection, transport round-trips, command
flag specifications, budget constants and response invariants. Catalog tests
check fixture coherence (required parser-case categories, valid descriptor
schemas, lexical expected order, ID/page deduplication, schema-valid evidence
state/reason output); **they do not execute a strict parser, filesystem loader,
path resolver, selection algorithm or evidence comparison**. W2/W3 must consume
the complete rows, including all input bytes/limits and expected
errors/parents/exclusions/resolutions/pages, as runtime tests. Their enforcement
is a remaining delivery obligation, not a W1 test pass or waived requirement.

Global `validate --evidence` bounds its evidence output before reading selected
files. If the output budget is exhausted, remaining unique targets count as
unchecked (unless shared with an inspected target), with explicit byte omissions
and no continuation. Validation without `--evidence` emits an empty evidence list.

## W2/W3 pre-release contract reconciliation

- Replaced W1's split navigation/references views with the approved uniform
  read `links` collection; authored records are unchanged.
- Added explicit concise ancestor projections and zero-record partial `get`
  for indivisible records over budget; no authored text is cut mid-field.
- Added aggregated diagnostic counts and clarified bounded global validation.
- Raised the Go minimum from 1.23 to 1.25 for `os.Root` containment across
  concurrent filesystem changes. No new package dependency or persisted state.
- Reject multiply linked regular files, including hard-linked descriptors, to
  avoid treating aliases of files outside the explicit root as owned metadata.
- W3 originally rejected scaffolding pending W4; it is now implemented below.

## W4 completion (compatible with the W3 read API)

- Implemented the already specified exclusive scaffold and advertised help.
- Added build-version injection and local cross-platform packaging.
- Added installed-module version reporting without changing the JSON envelope,
  schema/API versions or explicit linker-override precedence.
- Added real-process scaffold/refusal/readback/no-mutation tests and an opt-in
  full-process 10,000-file/500-record latency harness.
- No read response schema, `context`/`get` API, flag set, record schema or
  ranking/budget semantics changed.

Dependencies: `jsonschema/v5` compiles/validates the authoritative schemas
offline; `yaml.v3` supports typed YAML contract examples/tests and the later
strict node-based parser. No framework, daemon, SDK or persistence dependency.
