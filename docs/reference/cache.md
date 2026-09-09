# Cache and rate reference

`effect/cache` keeps what a program has already found out. `effect/rate` is
permission to proceed no faster than agreed. They are two packages because
they are two things, and they are in one reference because a program that reads
somebody else's service wants both.

```go
cache.Recalling[A](most, time.Now)          // *Recollection[A], typed, in this process
cache.Holding(most, time.Now)               // *Held, a Store in this process
cache.Read[R](store, key)                   // Effect[R, cache.Fault, Kept]
cache.Write[R](store, filing)               // Effect[R, cache.Fault, Unit]
cache.Drop[R](store, about)                 // Effect[R, cache.Fault, Unit]

rate.Holding(time.Now)                      // *Held, a Limiter in this process
rate.Waiting[R](limiter, allowance, longest) // Effect[R, rate.Fault, Unit]
```

```go
keeping := cache.Recalling[Panel](500, time.Now)
if panel, held := keeping.Remembered(film); held {
    return panel
}
// ... read the providers, then
keeping.Remember(film, subject, panel, 12*time.Hour)
```

## Two faces, because two things are wanted

Within one process a caller wants the value it already had: typed, with no
encoding and no failure to handle. `Recollection[A]` is that.

Between processes a caller wants an answer another instance already got, which
means bytes, a network, and something that can go wrong. `Store` is that, and
it is the port [`effect-golang-cache`](https://github.com/mbauer83/effect-golang-cache)
implements for Redis and Valkey.

Both are bounded **and** dated, and neither alone is enough: bounded so a long
afternoon cannot grow one without limit, dated so nothing is served for longer
than it is worth serving. A map with expiry grows; a bounded map serves last
week's answer.

## Not a capability, unlike config

A program has one clock, one logger and one place its settings come from, and
`capability.Set` holds one of each. It has as many caches as it has things
worth keeping — a bounded one in front of a shared one, a long-lived one for
records and a short-lived one for listings. So a cache is a port a program
holds, like a repository, rather than something the runtime hands it.

The clock is a parameter of the in-process adapters for the same reason it is a
capability elsewhere: a thing with a lifetime is a thing a test has to be able
to move, and waiting out a twelve-hour lifetime is not a test.

## A miss is an answer

`Kept` carries whether anything was found. A miss is the ordinary state of a
key nobody has asked for yet, and a store that failed on one would make every
first request an error to handle.

`Forget` takes the **subject** a value was filed under, not its key. Somebody
asking for a page to be looked up again means "find out about this thing
again", not "drop these four keys" — so what is kept says what it is about,
and everything about one thing goes together. A subject nothing was kept about
is not a failure.

## A reservation, not a check

`rate.Limiter.Turn` reserves the next turn and says how long until it. A check
would report whether there is room *now*, and ten callers asking at once are
all told yes. A reservation hands each caller a moment nothing else has been
given, so a caller that waits for its moment and then proceeds is within the
rate whatever else is happening beside it.

It returns the wait rather than performing it, and `rate.Waiting` performs it.
So the waiting happens in the interpretation, where the runtime's clock and its
cancellation are: a caller abandoned while waiting for its turn is abandoned,
and a test can move time rather than spend it.

Both adapters use the generic cell rate algorithm, which is why they are
interchangeable: one moment is kept per allowance — when the next request would
arrive if requests were spaced evenly — and a caller moves it along by one
spacing and waits until the moment it just claimed, less the burst the
allowance tolerates. The first `Most` requests in a period go at once and
everything after them is spaced, and a spent allowance comes back one turn at a
time rather than all at once on a window boundary.

`Waiting` refuses rather than sleeps when the turn is further off than the
caller said it would wait for: a request that would wait four minutes is one
whose caller has long since gone, and a refusal somebody can be shown beats a
page that never arrives.

## The in-process limiter is honestly wrong for four containers

`rate.Holding` is correct for a program that runs as one instance. Four
instances would each keep their own count and together ask at four times the
rate one of them agreed to. That is what a shared limiter is for, and it is why
this is a port.

## Deliberately absent

**A policy for filling a cache.** Loading on a miss, collapsing concurrent
misses into one load, refreshing ahead of expiry: each is a decision about the
thing being cached, and a caller with the miss in front of it can express any
of them with the combinators it already has.

**A typed shared cache.** A `Store` and a `schema.Schema[A]` would give typed
values across processes. It needs the schema module, so it cannot live here,
and nothing has asked for it: what crosses a process boundary in practice is
already bytes.

**A filesystem store.** Stdlib, so it belongs here rather than in an adapter
module, and nothing has asked for one.
