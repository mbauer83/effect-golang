# Faults and tasks

Most of an application needs no environment, because an adapter closes over
the client it was built with, and fails for one of a few reasons a caller
decides on. `effect.Task[A]` is that shape: `Effect[Unit, fault.Fault, A]`.
ZIO calls it `IO`; it is `Task` here because `effect.IO()` names the
filesystem operations.

```go
func (store Store) Find(id SessionID) effect.Task[Session]

effect.FromValue(session)                                      // already known
effect.FromFault[Session](fault.Missing.About("the session", err)) // fails at once
effect.Done()                                                  // nothing to do
effect.TaskOperations().Now()                                  // the clock, logging, forking
```

## Kinds

`fault.Kind` is one of four, because four decisions are made from them:

| Kind | Means | A caller |
| --- | --- | --- |
| `Unavailable` | there, and could not be reached | retries later, or responds 503 |
| `Missing` | not there | responds "not found" |
| `Unreadable` | a response or document that could not be understood, or a declaration that cannot be served | tells whoever maintains it |
| `Unacceptable` | a legitimate no; which rule is the wrapped error | passes it on to whoever asked |

`fault.Fault` carries its kind, a subject and the error behind it, which
`errors.Is` and `errors.As` reach. `Message` is the cause's own sentence, for a
refusal worded to a client; `Error` adds the kind and the subject, for a log.

## Adopting a library's fault

Every fault the effect-golang modules fail with states its kind with a
`Kind() fault.Kind` method: `web.Fault`, `redis.Fault`, `sql.Fault`,
`schema.Error`, `cache.Fault` and `rate.Fault`. `fault.From` adopts one, so an
adapter translates with one call rather than a closure per call site:

```go
web.Fetch[effect.Unit](client, http.MethodGet, "/keys", web.ClientRequest{}).MapError(fault.From)
```

`fault.KindOf(err)` reads the kind of any error, through wrapping. An error
that states none is `Unavailable`: most often a connection that failed, and
retrying it later is the safe reading.

## A program's main

`RunUntilStopped` interprets a program on a runtime until it ends or the
process receives SIGINT or SIGTERM, then closes the runtime so its observers
deliver what they hold. Being asked to stop is not a failure.

```go
func main() {
	settings, err := config.Load(config.Environment(), SettingsConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, err) // what is missing, and everything the program reads
		os.Exit(1)
	}
	runtime, err := effect.NewRuntime()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := effect.RunUntilStopped(runtime, serve(settings)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```
