# archie
Repository-native architectural knowledge for coding agents.

Archie v1 is a local Go CLI: strict metadata loading, bounded context/navigation,
known impact, validation, explicit Git evidence comparison and non-overwriting
descriptor scaffolding. Read commands are cache-free and do not mutate files.

- [Public v1 contract](docs/contract-v1.md): commands, JSON, safety and budgets.
- [Authored record schema](schemas/record.schema.json)
- [Root discovery extension](schemas/root.schema.json)
- [Response schema](schemas/response.schema.json)
- [Local packaging and representative measurement](docs/build-and-measure.md)

Requires Go 1.25 or later (rooted filesystem containment). Build and run:

```sh
go build -o archie.exe ./cmd/archie
.\archie.exe version
.\archie.exe context --root C:\path\to\application --query "report delivery"
.\archie.exe get --root C:\path\to\application --id renderer
.\archie.exe impact --root C:\path\to\application --path src/render.go
.\archie.exe validate --root C:\path\to\application --evidence
.\archie.exe scaffold --root C:\path\to\application --file archie.yaml --id app --name "Application" --summary "Owns application boundaries."
```

Queries emit bounded JSON. Exit 4 means an explicitly partial result; exit 1
invalid metadata, 2 usage/cursor errors, and 3 operational failure. Missing
matches never imply missing implementation. Without an explicit full commit
`--baseline`, existing evidence is `unknown`, not verified unchanged.
Scaffold requires an existing root/parent directory, validates generated YAML and
creates exactly one new file. Use `rendering.archie.yaml` for a named sibling;
non-root scaffolds require valid existing metadata and an unused ID.

Archie's own architecture starts at [archie.yaml](archie.yaml), with sparse
component records beside the implementation. With `archie` on PATH, from this
repository root:

```sh
archie context --root . --query "architecture of Archie CLI and bounded query/evidence pipeline" --max-records 5 --max-bytes 32768
archie get --root . --id query
archie discover --root .
```

Run `go test ./...` and `go vet ./...`. Tests use `ARCHIE_TEST_SCRATCH` when set
for their isolated fixtures; otherwise they create and remove fixture directories
beneath each test package, never the system temporary directory.
