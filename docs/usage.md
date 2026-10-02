# Using Archie

[Overview](../README.md) | [Getting started](getting-started.md) | [CLI contract](contract-v1.md)

Archie is most useful when records describe meaningful responsibilities and
point readers to the implementation. Start small, keep claims specific, and
update the map with the code.

Examples below assume `archie` is on `PATH` and you are in your application's
root. The record IDs and file paths are illustrative; use ones that exist in
your project.

## Describe a component

After creating a root record with the [quickstart](getting-started.md#create-your-first-record),
add a named sibling:

```sh
archie scaffold --root . --file rendering.archie.yaml --id renderer --name "Report renderer" --summary "Formats customer reports for delivery."
```

Open the new file and extend it with knowledge verified against the source.
For example, the following record is appropriate **only if the referenced
files exist and the description matches your implementation**:

```yaml
schema_version: "1"
id: renderer
name: Report renderer
summary: Formats customer reports for delivery.
kind: component
tags: [reports, rendering]
description: |
  Converts report data into the document sent to a customer.
  Owns formatting, not scheduling or sending messages.
links:
  - relation: source
    target: src/render.go
    description: Report formatting implementation.
  - relation: test
    target: src/render_test.go
    description: Formatting regression tests.
  - relation: doc
    target: docs/report-format.md
    description: Supported report content and layout.
```

All records require `schema_version: "1"`, `id`, `name`, and `summary`.
Optional fields include `kind`, `tags`, `description`, `rationale`, and `links`.
IDs are unique across the application; preserve them when moving a component.
Omit unused optional fields rather than assigning blank or null values.

Use a directory's `archie.yaml` for that area's boundary, or a named
`*.archie.yaml` for a component beside other records. Archie derives parent and
child relationships from their locations; do not author `parent` or `child`
links yourself. Scaffolding requires existing parent directories and never
overwrites files.

### Connect responsibilities and evidence

Each link has a `relation`, `target`, and `description`:

| Relation | Target | Use it for |
| --- | --- | --- |
| `depends_on` | Another record's ID | A documented dependency between components |
| `related_to` | Another record's ID | A useful association without implying dependency |
| `source`, `test`, `doc`, `adr` | A file path relative to the descriptor | Implementation, regression tests, documentation, or an architecture decision |

For example, a separate mailer record can link to the renderer:

```yaml
links:
  - relation: depends_on
    target: renderer
    description: Uses the renderer to format the report before sending it.
```

Use `/` in authored file paths on every OS, even Windows. Paths are literal,
not globs or URLs, and must remain within the application root. A record under
`src/archie.yaml` would refer to a sibling source file as `render.go`, not
`src/render.go`. Links must name actual files with the correct casing; unsafe
paths, symlinks, and hard-linked file aliases are not accepted.

Keep record rationale concise and link longer decisions with `adr`. The
decision's status and authority remain in the ADR; Archie does not infer them.
See the [record schema](../schemas/record.schema.json) and
[root discovery schema](../schemas/root.schema.json) for exact field shapes.

## Find context for a task

```sh
archie context --root . --query "report delivery"
archie context --root . --id renderer
archie context --root . --path src/render.go
```

Topic matching uses words in authored metadata. It does not search source
contents, use embeddings, or infer synonymous concepts. ID and path selectors
can help when you already know part of the area. Multiple selectors add
candidates rather than filtering each other.

The packet includes selected records, relevant relationships, file pointers,
and evidence states. An ancestor may appear as a summary for orientation; use
`get` when you need its full record:

```sh
archie get --root . --id renderer
archie discover --root .
```

`discover` lists records without checking their file evidence. It is bounded,
so inspect continuation rather than assuming the first page is the entire map.

## Explore known impact

```sh
archie impact --root . --path src/render.go
```

This finds known records associated with the selected path through descriptor
locations or authored file links, with bounded related context. Repeat `--path`
for multiple files. This is not a call graph or an exhaustive reverse-dependency
analysis: undocumented relationships cannot be recovered from metadata.

Use the returned pointers to decide what source and tests to inspect, not as
proof that no other component can be affected.

## Validate the map

```sh
archie validate --root .
```

Plain validation checks record schemas, IDs and record links, and every
distinct referenced file's existence and path/identity safety within its work
limits. It reports compact counts under `coverage.references` rather than a
success row for every file.

Missing or unsafe references fail. Excluded or unvisited references cannot
produce complete success. Validation does not read all file contents or prove
full readability, semantic freshness, or the truth of architectural claims.
Optional `--evidence` adds bounded file evidence detail:

```sh
archie validate --root . --evidence
```

If using validation as a required check, require exit 0, a supported parseable
response, `status: "ok"`, `data.valid: true`, complete valid metadata, and
complete reference coverage: `checked = total`, with zero `unchecked`, `missing`,
and `invalid`. Missing coverage is unsupported, not successful empty coverage.
The [validation contract](contract-v1.md#complete-reference-validation) is the
authoritative integration specification.

## Compare linked files with a Git commit

Evidence comparison requires an explicitly chosen full commit ID in the local
Git repository. Archie never chooses HEAD or a merge base for you and does not
fetch history.

In PowerShell, for example, to compare linked files against the current commit:

```powershell
$baseline = git rev-parse HEAD
archie get --root . --id renderer --baseline $baseline
```

Use this only in a Git repository with an existing commit, and stop if resolving
the commit fails. For a different review baseline, resolve that commit instead.
Validation accepts `--baseline` only together with `--evidence`.

Without a baseline, existing files have `unknown` / `no_baseline` evidence,
not "unchanged." With a baseline, identical or different bytes are reported as
`unchanged` or `changed`; neither proves whether a description is correct.
Missing, unavailable, or unvisited evidence remains explicitly uncertain.
See [baseline semantics](contract-v1.md#explicit-comparison-baseline).

## Read JSON results and partial coverage

Commands return one JSON envelope; `--help` returns plain text. Useful fields:

| Field | Meaning |
| --- | --- |
| `data` | Command-specific records, evidence, validation result, or version |
| `status` and `diagnostics` | Outcome and actionable problems |
| `coverage` | What was loaded, selected, checked, excluded, or omitted |
| `continuation` | A cursor for remaining records when pagination is possible |
| `baseline` | Requested/resolved commit and comparison availability |

Exit codes are `0` success, `1` invalid metadata/path/reference, `2` usage or
cursor errors, `3` operational failure, and `4` partial/incomplete work.
Check both the exit code and response, including diagnostics and coverage.

For paginated queries, pass `continuation.cursor` back using `--cursor`, keeping
the original command, selectors, and budgets unchanged. A changed metadata
snapshot can invalidate the cursor; restart the query rather than modifying
the cursor yourself. Not every omission is pageable.

You can set `--max-records`, `--max-links`, or `--max-bytes` where the command
supports them. More output does not remove evidence or discovery limits.
Use the [CLI contract](contract-v1.md#exact-cli-surface) for supported flags and
the [response schema](../schemas/response.schema.json) when building a consumer.

## Keep records useful

When changing a component, review its responsibilities, relationships, and file
links together with the implementation. Validate the updated map, query the area
again, and inspect the linked source before treating the record as current.

Prefer a small number of grounded records over a record for every file.
Document important boundaries and decisions, not commands for an agent to obey.
