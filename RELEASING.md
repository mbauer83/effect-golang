# Releasing

The four modules are one project in four repositories:

```text
github.com/mbauer83/effect-golang         the runtime
github.com/mbauer83/effect-golang-schema  descriptions, on the runtime
github.com/mbauer83/effect-golang-sql     tables and migrations, on both
github.com/mbauer83/effect-golang-web     transports, on the first two
```

Each carries its own version, and a module's `go.mod` records which versions of
the others it was built against — so "which schema does this transport agree
with" has an answer a person can read off a file.

That is a correction. This said at first that all four share one version number,
which reads well and costs three empty releases every time one module gains
something: adding an HTTP client to the transports does not make the runtime a
new version, and tagging it as one says something untrue about the runtime. What
the shared number was for was answering the agreement question, and a
requirement already answers it.

## Publishing is pushing a tag

Go has no registry to upload to. A module version *is* a tag on a commit, and
`proxy.golang.org` fetches and caches it the first time anybody asks. So there
is nothing to publish beyond the tag — and nothing to un-publish either, which
is why the order below matters more than it would elsewhere.

## The order

Dependency order, one module fully released before the next begins:

1. `effect-golang`
2. `effect-golang-schema`
3. `effect-golang-sql`
4. `effect-golang-web`

Only the modules that changed need releasing; the order is the order among
those.

Each module's `go.mod` requires the ones above it **by version**. A commit whose
`go.mod` names `v0.1.0` of the runtime cannot be built by anyone — CI included —
until that tag is pushed. Releasing out of order does not produce a broken
version; it produces a red build that goes green later, which is worse, because
nothing distinguishes it from a real failure.

For each module, in that order:

```sh
cd effect-golang            # then -schema, then -sql, then -web
go build ./... && go vet ./... && go test -count=1 ./...
git push                                 # the commits first
git tag -a v0.1.0 -m 'effect-golang v0.1.0'
git push origin v0.1.0                   # the tag makes the version exist
go list -m github.com/mbauer83/effect-golang@v0.1.0   # the proxy has it
```

Then wait for that repository's CI to go green before starting the next one. Its
green run is the evidence the next module's requirement can be resolved, which
is the thing being checked.

## Bumping to the next version

Bump the module that changed. A dependent module follows only when it wants
what changed:

```sh
cd effect-golang-schema
go get github.com/mbauer83/effect-golang@v0.2.0
go mod tidy
go build ./... && go vet ./... && go test -count=1 ./...
```

Then tag `effect-golang-schema` itself — a new requirement is a change to it,
so it earns a version of its own.

Dependency order still governs: a requirement must exist before the module that
names it can be built by anyone. So raising a requirement and tagging the
module that raised it are two pushes in that order, never one.

## Working on several at once

A `go.work` above all four resolves them to the working copies beside each
other, so a change to a description can be tried against the transports before
anything is tagged:

```sh
cd workspace
go work init ./effect-golang ./effect-golang-schema ./effect-golang-sql ./effect-golang-web
```

It is not checked in to any of the modules — it belongs to whoever has several
checked out at once. A `replace` in a `go.mod` is not the tool for this: it is
ignored by anything that depends on the module carrying it, so it says nothing
to a consumer and only ever describes one person's layout.

**While a required version does not exist yet**, add version-qualified
replacements to the `go.work` as well as the `use` block:

```text
replace (
	github.com/mbauer83/effect-golang v0.1.0 => ./effect-golang
	github.com/mbauer83/effect-golang-schema v0.1.0 => ./effect-golang-schema
)
```

A `use` block already makes each module the selected version of its path, so
these look redundant, and for almost everything they are. The exception is the
*full* module graph: `modernc.org/sqlite`, which the SQL tests use as their
local database, depends on modules whose `go` directives are 1.16 and 1.12, and
a module below 1.17 has no pruned requirements — so loading it loads the whole
graph, and that traversal follows requirement *edges*, which name a version. An
edge naming an unpushed tag fails there, and the failure is reported against
whichever import triggered it. Once the tags are pushed the lines stop
mattering.
