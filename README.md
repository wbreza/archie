# Archie

**An architectural map of your codebase, kept with your code.**

Archie helps developers and coding agents understand how a project fits
together before changing it. You describe the important parts of your system in
small YAML files alongside the source: what each part owns, how it relates to
other parts, and where to find the code, tests, documentation, and decisions
behind it. Archie's local command-line tool turns those records into focused,
machine-readable context.

Instead of rediscovering the same boundaries on every task, your team can keep
that knowledge in version control and review it with the code it describes.

## Why use Archie?

Code search helps you find a symbol. Archie helps you find the **architectural
context around the work**:

- **Get oriented in an unfamiliar project.** Find the components associated
  with a topic and understand their responsibilities and relationships.
- **Give coding agents a starting point.** Retrieve a bounded context packet
  with pointers to relevant source, tests, and architectural decisions.
- **Understand known impact before a change.** Find documented components
  associated with a file or directory you plan to edit.
- **Keep the map connected to the code.** Validate records and their file
  references, and optionally compare linked files with an explicit Git commit.

For example, a task about report delivery might lead you to records for a
renderer and a mailer, explain which owns formatting versus sending, and point
you to their tests and design decisions.

## How it works

1. **Describe your system.** Add a root `archie.yaml`, then records near
   meaningful components using `archie.yaml` or a name such as
   `rendering.archie.yaml`. Start with useful boundaries, not every file.
2. **Ask for context.** Query by topic, record ID, or path. Archie reads the
   records, follows documented relationships, and returns JSON within explicit
   output limits.
3. **Maintain the map as the code evolves.** Update descriptions and links
   alongside implementation changes, and validate them before handing work off.

Archie runs locally. Read commands do not modify your project or create
persistent indexes or caches. The records are descriptive data, not executable
instructions. The CLI is language-independent: it works with the architecture
you describe, rather than requiring a parser for your application's language.

## Quickstart

Download the archive for your platform from
[the releases page](https://github.com/wbreza/archie/releases/latest), extract
it, and put the executable on your `PATH`. No Go installation is needed to run
a release binary.

Alternatively, with **Go 1.25+** and Git installed:

```sh
go install github.com/wbreza/archie/cmd/archie@v0.2.0
archie version
```

Go's binary directory must be on your `PATH`. See
[installation and setup](docs/getting-started.md) for platform details.

In an **existing demo directory without an `archie.yaml`**, run:

```sh
archie scaffold --root . --file archie.yaml --id reports --name "Report service" --summary "Generates and delivers customer reports."
archie context --root . --query "customer reports"
archie validate --root .
```

The first command creates a minimal architecture record. The second returns a
JSON context packet containing that record. The third checks the metadata and
any referenced files; this minimal example does not have file links yet.

Next, [add a component and links to its code](docs/usage.md#describe-a-component)
to make the map useful for real work. If your repository already has Archie
records, skip scaffolding and query them instead.

## What Archie does not do

Archie does not automatically infer your architecture or replace reading source
code. Its topic matching searches authored metadata, not the meaning of every
file. Records need to be written and maintained by people or agents that have
examined the implementation.

Known impact is not exhaustive dependency analysis, and successful validation
does not prove a description is still true. Results make limits and uncertainty
explicit so an agent or developer can decide what to inspect next.

## Learn more

| I want to... | Start here |
| --- | --- |
| Install Archie and create my first record | [Getting started](docs/getting-started.md) |
| Describe components, query context, or check a change | [Usage guide](docs/usage.md) |
| Contribute or explore Archie's implementation | [Contributing](CONTRIBUTING.md) |
| Integrate the CLI with tools or coding agents | [CLI and JSON contract](docs/contract-v1.md) |
| Build archives or understand measurement results | [Build and measurement](docs/build-and-measure.md) |
| Understand version compatibility and releases | [Versioning](docs/versioning.md) |
