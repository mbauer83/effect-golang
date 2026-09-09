# Configuration reference

`effect/config` describes what a program must be told, and `effect.LoadConfig`
reads it.

```go
config.Config[A]                                    // a description
effect.LoadConfig[R, A](description)                 // Effect[R, ConfigError, A]
effect.ConfigLayer[RIn, A](description)              // Layer[RIn, ConfigError, A]
effect.WithConfigSource(source)                      // a RuntimeOption
fx.ReadingConfigFrom(source)                         // one part of a program
```

```go
described := config.ZipWith(
    config.NonEmptyText("host").Documented("the address of the primary"),
    config.Port("port").WithDefault(5432),
    func(host string, port int) Store {
        return Store{Host: host, Port: port}
    })

settings := effect.ConfigLayer[effect.Unit](config.Nested("db", described))
program := serve().ProvideLayerSame(settings.MapError(refusalOf))
```

## Why it is here and not in the transports

A listen address is configuration, and so is a database URL, a queue name, a
log level and a retry budget. Nothing about it is about HTTP, so it sits beside
the runtime, where every module above can use it and none has to depend on
another to get it.

It is a **capability**, for the same reason the clock is: where a value comes
from is a property of the deployment and not of the code that needs the value.
A component describes what it needs; the runtime is told where to read.

## Why the composition is its own and not the effect's

`Config` has `ZipWith`, `All`, `Map`, `MapOrFail` and `OrElse`, which look like
combinators `Effect` already has. They are not the same operations, and the
difference is the reason this type exists.

**A description accumulates; an effect short-circuits.** A deployment that has
supplied none of four required settings must be told about four settings, not
about the first one — otherwise it is restarted once per key. `Effect`'s
sequential composition stops at the first failure, which is what sequencing
means, and its parallel composition forks fibers and produces a `Cause` rather
than a typed failure. A description's `ZipWith` reads both sides whatever
either did and composes the failures with `And`. That is an applicative over a
semigroup of failures, and it is a different structure from a monad, not a
duplicate of one.

**A description is inspectable; an effect is opaque.** `Expects()` is why
`config.Document` can print what a program needs without running it, and why a
value can be marked optional or secret. An effect is a closure the interpreter
walks; it cannot answer "what would you have asked for".

**Sequencing is the effect's, and stays the effect's.** There is no `FlatMap`
on `Config`. A read that depends on a value already read — which store to use,
named by a setting — composes with the runtime's own `FlatMap`, because that is
where accumulation has to stop anyway:

```go
effect.LoadConfig[Env](config.Text("store")).
    FlatMap(func(name string) effect.Effect[Env, effect.ConfigError, Store] {
        return effect.LoadConfig[Env](config.Nested(name, describedStore()))
    })
```

So: **accumulating composition belongs to a description, sequencing belongs to
the runtime.** ZIO and Effect both draw the line in the same place and for the
same reason.

## The failure

```go
type ConfigError = config.Error

failure.Failures()    // the leaves, left to right
failure.MissingOnly() // every leaf is a path nobody supplied
failure.Kind()        // KindMissing, KindInvalid, KindUnavailable, KindAnd, KindOr
```

Compositional, and shaped like the runtime's `Cause` on purpose: `And` for
failures a description needed both of, `Or` for alternatives that were all
tried, and `Failures` to flatten. It is a **typed failure** and not a `Cause`,
because a program must be able to catch it, report it or fall back from it with
`CatchAll` — a composite cause is only visible at the `Exit`.

Three leaves, and the distinctions between them are load-bearing:

| Leaf | Means | A default stands in |
|---|---|---|
| `KindMissing` | the source does not carry the path | yes |
| `KindInvalid` | the value is there and unreadable | **no** |
| `KindUnavailable` | the source could not be consulted | **no** |

Reading those as one fact is how a mistyped setting becomes a default nobody
chose, and how a secret store that is down becomes a service that started
without its credentials. `WithDefault` and `Optional` apply to absence only;
`OrElse` is the combinator that means "try the other one whatever went wrong".

## Descriptions

```go
config.Of[A](name, reads, parse)   // the general primitive, and the seam
config.Text(name)                  config.NonEmptyText(name)
config.Int(name)                   config.Float(name)
config.Bool(name)                  config.Duration(name)
config.Port(name)                  config.SecretOf(name)  // a Secret
```

`Bool` accepts what deployments actually write — `true`, `yes`, `on`, `1` and
their negatives — and refuses anything else rather than reading it as false.
`Port` is a primitive rather than an `Int` with a range because every program
wants the same range and the same message, and because a port read as a plain
number fails at bind time instead of at start-up.

`NonEmptyText` is its own primitive because "set to the empty string" is the
commonest way a deployment supplies nothing while looking like it supplied
something — a variable assigned from an unset variable, a template that
rendered a missing value. `Text` accepts it, and a host of `""` then fails at
connect time instead of at start-up.

**An empty name reads the value where the description stands** rather than at a
key beneath it. That is one rule, and it is what makes `Table` and `Many` work
with the same primitives everything else uses.

### Nested and Table are two different questions

`Nested` moves a description one segment deeper: the **program** knows the
keys. `Table` asks the source which keys exist beneath a name and reads the
entry description once per key: the **source** knows them.

```go
config.Nested("db", config.ZipWith(config.Text("host"), config.Port("port"), …))
// reads db.host and db.port -- named in the code, one value of one type

config.Table("limits", config.Int(""))
// reads whatever keys exist under limits -- map[string]int, keys from the
// deployment: LIMITS_READ and LIMITS_WRITE become read and write
```

So `Nested` never consults `Children` and cannot fail for a key nobody wrote;
`Table` consults nothing else, and no keys beneath the name is an empty table
rather than a failure. Adding a limit is a deployment change; adding a field to
`db` is a release.

They compose, and that is where a nested table earns its shape: the source
decides the entries, and the description decides what an entry holds.

```go
config.Table("queues", config.ZipWith(config.Int("depth"), config.Int("workers"), …))
// QUEUES_JOBS_DEPTH, QUEUES_JOBS_WORKERS, QUEUES_MAIL_DEPTH, …
```

```go
config.ZipWith(first, second, combine)    // both are read; failures accumulate
config.All(descriptions...)               // the homogeneous case
config.Nested(name, of)                   // read beneath a name
config.Table(name, of)                    // one entry per key the source holds
config.Many(name, separator, of)          // several values held in one key

description.Map(f)                        description.MapOrFail(f)
description.Validated(message, keep)      description.Documented(doc)
description.WithDefault(value)            description.OrElse(that)
config.Optional(of, supplied, absent)
```

`Optional` takes two branches rather than producing an option, because a
program that treats a setting as optional has to say what its absence means —
no certificate is plaintext, no schedule is off — and saying it beside the
description is better than a value that might not be there at every use.

## Sources

```go
config.Environment()                      // the process environment
config.EnvironmentOf(entries...)          // a fixed one, for a test
config.Fixed(map[string]string{...})      // values a program already holds
config.Sources(first, second, ...)        // the first that carries a path answers
config.Beneath(source, path...)           // mount a description inside a document
config.Renaming(source, spell)            // one description, another spelling
```

`Environment` spells a path in upper case with underscores — `db.host` is
`DB_HOST` — which is the convention every deployment tool already writes. It
reads a snapshot when it is constructed, so two descriptions read at different
moments cannot disagree about what the deployment said.

`Sources` is **order as precedence**: what a deployment overrides with comes
first, and the defaults a program shipped with come last. A source that cannot
be consulted stops the search and is reported rather than falling through,
because falling through from a secret store that is down to the defaults
beneath it would be silent and would only happen when the store is down.

The port is two methods, and a new source is expected to be small:

```go
type ConfigSource interface {
    Value(ctx context.Context, path []string) (string, bool, error)
    Children(ctx context.Context, path []string) ([]string, error)
}
```

Absence is the boolean and unavailability is the error. Parsing, defaults,
composition and nesting are the description's business, so a source that reads
a document or a store has nothing to get wrong but the reading.

## n providers, m consumers

Both directions are the point, and neither needs the other to know about it.

**n providers, one view.** `Sources` composes them; `Beneath` and `Renaming`
adapt a source's shape and spelling to what a description says. One description
reads a value the environment supplied and a value the shipped defaults
supplied and cannot tell which was which.

**One provider, m consumers.** Each component holds its own description and
reads through the runtime, so a component's settings are its own type built by
its own layer. `ZipLayers` puts two of them beside each other, `ThenLayers`
feeds one into the next, and a consumer that needs a setting its layer does not
build will not compile.

**m consumers, m providers.** `ReadingConfigFrom` gives one part of a program a
source of its own — a plugin configured from the document it shipped with while
the program around it reads the environment. It is runtime-local and inherited
exactly as a span or a name is: work forked inside it reads from the same
source, and the effect after it does not.

## Secrets

`config.SecretOf(name)` reads a `config.Secret`, which is opaque the three ways a
Go value escapes: `String` for anything formatted, `MarshalText` for anything
encoded, and `LogValue` for `slog` — which is what this runtime's logs,
annotations and events are made of. `Reveal()` is the only way out, and it is a
word a reviewer can search for.

A printed expectation shows that a secret is wanted and never a value: it is
built from the description, which never held one.

## Saying what a program needs

```go
fmt.Print(config.Document(described.Expects()))
```

```text
  db.host       non-empty text         required  the address of the primary
  db.limits     a table of an integer  optional  one limit per operation
  db.password   a secret               required  supplied by the deployment
  db.port       a port                 5432
  db.timeout    a duration             5s        how long a statement may take
```

The question a description can answer and a function that reads cannot. A
deployment that has just been told a value is missing can be shown the whole
list without starting anything.

## Deliberately absent

**Reloading.** A source is read when a description is read, and a program that
wants to re-read one interprets the effect again — inside a `Schedule`, or when
a signal arrives. What is deliberately not here is a value that changes under a
component that has already been given it: that is a `Ref` or a `Hub`, and it is
the program's decision which.

**Formats.** No YAML, no TOML, no dotenv. Each is a decoder that produces a
flat map, which is `config.Fixed`, and a module that pulled in a parser would
make every program that only reads the environment carry it.

**Struct decoding from a schema.** The natural next step, and it belongs in
[`effect-golang-schema`](https://github.com/mbauer83/effect-golang-schema),
which already has `Struct`, `FieldOf`, constraints and a decoder port: a
`schema.Source` backed by a `config.Source` would decode a whole settings type
with the constraints and documentation the schema already carries. Building it
here would mean a second description language beside that one.

## See also

- [Layers and provision](core.md), which is how settings reach a consumer.
- [`examples/configured`](../../examples/configured/settings.go), a complete
  program: two components, two sources, a plugin with a source of its own, and
  an end-to-end test for each claim.
