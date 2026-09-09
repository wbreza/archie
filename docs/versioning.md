# Go versions and manual releases

**First release: `v0.1.0`, pending publication.** The commands below describe the
approved policy, not an already available remote tag. Do not advertise a working
tagged install until the exact merged release commit has been approved, tagged
and verified. No GitHub Release, release assets, registry or publishing workflow
is required: Go installs directly from the immutable Git tag.

## Version policy

- This repository has one root Go module, `github.com/wbreza/archie`. Tags are
  root-level **`vMAJOR.MINOR.PATCH`**, including the required `v` prefix; not
  `cmd/archie/v0.1.0` or `0.1.0`.
- Start at **`v0.1.0`**, not `v1.0.0`: the existing “Archie v1” contract names
  API/schema generation `"1"`, not a promise of Go module v1 stability.
- Increment patch for backward-compatible fixes. Increment minor for features;
  before v1, breaking changes also require a minor increment and explicit
  migration/compatibility notes. Never hide breaking changes in a patch.
- Optional prereleases use forms such as `v0.2.0-rc.1`. Pin a specific tag;
  `@main` is mutable and `@latest` is not a stability guarantee.
- Never move, delete or reuse a published tag. Correct a bad release with a new
  version. Do not force-push tags or use `git push --tags`.
- A future Go module **v2 or later requires the matching `/v2` (etc.) suffix**
  in `go.mod`, imports and install paths. API/schema version changes require
  their own compatibility decision; they do not follow the CLI/module major.

## What `archie version` reports

The JSON envelope and `api_version: "1"` remain unchanged. `data.version` uses:

1. A nonempty explicit `-ldflags "-X github.com/wbreza/archie/internal/query.BuildVersion=..."` value, verbatim.
2. Otherwise, Go's embedded main-module version from `runtime/debug.ReadBuildInfo`,
   verbatim: e.g. `v0.1.0`, `v0.2.0-rc.1`, or an installed commit's pseudo-version.
3. Otherwise, `0.1.0-dev` for local source builds (`(devel)` or missing metadata).

There is no runtime Git/network lookup. A local checkout at a tag is still a
development build unless explicitly stamped; HEAD alone is not a release.
An explicit `0.1.0-dev` override still wins over installed-module metadata.
The local [packager](build-and-measure.md) retains its existing unprefixed
`-version 0.1.0` input and injects that value; this is separate from Go's required
`v0.1.0` tag spelling. Use `go version -m` to inspect provenance independently.

## Private tagged install and verification (PowerShell 7)

Requires Go 1.25+, Git and a GitHub account with repository read access.
`wbreza/archie` is currently private. Before downloading, authorize the account,
tag and destination; an install may replace a binary already in that directory.
Choose a separate directory if the existing binary must remain untouched.
No OAuth `workflow` scope is needed for an install. Never switch the global
`gh` account, put a token in a URL/command argument/file, or persist Go settings.

After `v0.1.0` is published, the supported package is:

```text
go install github.com/wbreza/archie/cmd/archie@v0.1.0
```

For private access, run it in the protected process below, not via the default
public proxy. The effective existing `GOPRIVATE`, `GONOPROXY` and `GONOSUMDB`
lists are extended independently, including explicit ambient overrides.
Credential helper changes and the selected stored token are process-only.

```powershell
$account = 'wbreza' # Explicitly authorized, already authenticated account.
$tag = 'v0.1.0'     # Pending publication; do not run until verified available.
$installDir = 'C:\tools\archie-v0.1.0' # Approved absolute destination.
$archie = Join-Path $installDir 'archie.exe'
$count = if ($env:GIT_CONFIG_COUNT) { [int]$env:GIT_CONFIG_COUNT } else { 0 }
$names = @('GH_TOKEN','GOBIN','GOPRIVATE','GONOPROXY','GONOSUMDB',
    'GIT_CONFIG_COUNT','GIT_TERMINAL_PROMPT',
    "GIT_CONFIG_KEY_$count","GIT_CONFIG_VALUE_$count",
    "GIT_CONFIG_KEY_$($count + 1)","GIT_CONFIG_VALUE_$($count + 1)")
$saved = @{}
$patterns = @{}
foreach ($name in $names) { $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
foreach ($name in @('GOPRIVATE','GONOPROXY','GONOSUMDB')) { $patterns[$name] = (go env $name).Trim() }
try {
    foreach ($name in $patterns.Keys) {
        $value = ($patterns[$name], 'github.com/wbreza/archie' | Where-Object { $_ }) -join ','
        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
    }
    $env:GH_TOKEN = gh auth token --hostname github.com --user $account
    if ($LASTEXITCODE -ne 0 -or -not $env:GH_TOKEN) { throw 'Authenticate the selected account first.' }
    $login = gh api user --hostname github.com --jq .login
    if ($LASTEXITCODE -ne 0 -or $login -ne $account) { throw 'Unexpected GitHub identity.' }
    gh api repos/wbreza/archie --hostname github.com --jq .full_name
    if ($LASTEXITCODE -ne 0) { throw 'Selected account cannot read the repository.' }
    foreach ($i in $count..($count + 1)) {
        [Environment]::SetEnvironmentVariable("GIT_CONFIG_KEY_$i", 'credential.https://github.com.helper', 'Process')
    }
    [Environment]::SetEnvironmentVariable("GIT_CONFIG_VALUE_$count", '', 'Process')
    [Environment]::SetEnvironmentVariable("GIT_CONFIG_VALUE_$($count + 1)", '!gh auth git-credential', 'Process')
    $env:GIT_CONFIG_COUNT = [string]($count + 2)
    $env:GIT_TERMINAL_PROMPT = '0'
    $env:GOBIN = $installDir
    go list -m -json "github.com/wbreza/archie@$tag"
    if ($LASTEXITCODE -ne 0) { throw 'Tag lookup failed; do not retry through public services.' }
    go install "github.com/wbreza/archie/cmd/archie@$tag"
    if ($LASTEXITCODE -ne 0) { throw 'Install failed; diagnose before retrying.' }
    go version -m $archie
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read installed build information.' }
    & $archie version
    if ($LASTEXITCODE -ne 0) { throw 'Installed executable failed.' }
} finally {
    foreach ($name in $names) {
        if ($null -eq $saved[$name]) { Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue }
        else { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
    }
}
```

Require module lookup/build information to identify `github.com/wbreza/archie`
at exactly `v0.1.0`, and the executable to return `data.version: "v0.1.0"` with
`api_version: "1"`. A pseudo-version is useful commit provenance, **not proof of
the release tag**. Keep invoking the inspected absolute binary path; do not
overwrite another install or persist PATH changes automatically.

## Maintainer: publish exactly one approved tag

Run only after review and merge, **with separate approval of the exact version
and full commit SHA**. Use a clean checkout of that merged `main` commit and
require the existing `evaluate` workflow's main-push run to be green for that
exact SHA. No branch-protection/admin bypass is part of this process.
This example deliberately refuses a changed main tip, dirty checkout or existing
tag. Resolve any failed check before continuing; do not replace a tag.

```powershell
$account = 'wbreza'
$tag = 'v0.1.0'
$sha = '<approved full merged main commit SHA>'
if ($sha -notmatch '^[0-9a-f]{40}$') { throw 'Supply the approved full commit SHA.' }
$savedToken = $env:GH_TOKEN
try {
    $env:GH_TOKEN = gh auth token --hostname github.com --user $account
    if ($LASTEXITCODE -ne 0 -or -not $env:GH_TOKEN) { throw 'Authenticate the selected account first.' }
    $login = gh api user --hostname github.com --jq .login
    if ($LASTEXITCODE -ne 0 -or $login -ne $account) { throw 'Unexpected GitHub identity.' }
    $gitAuth = @('-c', 'credential.helper=', '-c', 'credential.helper=!gh auth git-credential')
    git @gitAuth fetch --no-tags origin '+refs/heads/main:refs/remotes/origin/main'
    if ($LASTEXITCODE -ne 0) { throw 'Fetch failed.' }
    $head = git rev-parse HEAD
    if ($LASTEXITCODE -ne 0 -or $head -ne $sha) { throw 'Checkout is not the approved commit.' }
    $main = git rev-parse origin/main
    if ($LASTEXITCODE -ne 0 -or $main -ne $sha) { throw 'Main changed; re-evaluate release approval.' }
    $dirty = git status --porcelain
    if ($LASTEXITCODE -ne 0 -or $dirty) { throw 'Release checkout must be clean.' }
    $runs = gh run list --repo wbreza/archie --workflow evaluate.yml --event push --branch main --commit $sha --limit 1 --json headSha,status,conclusion
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read evaluate status.' }
    $run = @($runs | ConvertFrom-Json)
    if ($run.Count -ne 1 -or $run[0].headSha -ne $sha -or $run[0].status -ne 'completed' -or $run[0].conclusion -ne 'success') {
        throw 'Require green evaluate on this exact merged commit.'
    }
    git show-ref --verify --quiet "refs/tags/$tag"
    if ($LASTEXITCODE -ne 1) { throw 'Local tag exists or could not be checked.' }
    git @gitAuth ls-remote --exit-code --tags origin "refs/tags/$tag"
    if ($LASTEXITCODE -ne 2) { throw 'Remote tag exists or could not be checked.' }
    git tag -a $tag $sha -m "Archie $tag"
    if ($LASTEXITCODE -ne 0) { throw 'Annotated tag creation failed.' }
    git @gitAuth push origin "refs/tags/${tag}:refs/tags/${tag}"
    if ($LASTEXITCODE -ne 0) { throw 'Push failed; inspect remote state before any retry.' }
    git @gitAuth ls-remote --tags origin "refs/tags/$tag" "refs/tags/$tag^{}"
    if ($LASTEXITCODE -ne 0) { throw 'Cannot verify published tag.' }
} finally {
    $env:GH_TOKEN = $savedToken
}
```

The peeled `refs/tags/v0.1.0^{}` commit must equal the approved SHA. Annotation
is required; signing may use an already configured key but is not a new
prerequisite. Never delete even a local release tag to conceal a failed publish.
Then perform the private `go list -m`, isolated `go install`, `go version -m` and
`archie version` checks above. Only after those pass, update install guidance to
mark the tag available. Tag availability and a remote tagged install cannot be
proved by local fixture tests or a source build.
