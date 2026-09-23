# Rewrite direct style into FlatMap chains

Direct style runs each body on a goroutine of its own. That costs a goroutine
hand-off per run, and a goroutine per running body. `effectgo` removes both for
the bodies it can translate, by rewriting them into the `FlatMap` chains you
would otherwise have written by hand.

```sh
go -C tools/effectgo build -o "$(go env GOPATH)/bin/effectgo" .   # from a checkout

effectgo test ./...
effectgo build -o server ./cmd/server
```

`effectgo` runs the go command with an `-overlay` naming rewritten copies of the
files that hold direct-style bodies. The source tree is not touched, nothing is
generated into it, and an editor sees the code as written. To use the overlay
with a command of your own:

```sh
go test -race -overlay="$(effectgo overlay ./...)" ./...
```

## What it does and does not change

The rewrite is an optimisation, never a semantics. A body is ordinary Go that
compiles and runs correctly without `effectgo`, and a body it cannot translate
is left exactly as written and keeps running on direct's goroutine. The only
cost of a declined body is the speed it would have gained.

`//line` directives keep every position at the line that was written, so a stack
trace, a coverage profile and a failure's origin read the same either way.

A rewritten body behaves as a `FlatMap` chain, which is what it is. That differs
from direct style in one place: a `runtime.Goexit` inside the body — the
realistic case is `testing.T.FailNow` — ends the goroutine running the fiber,
as it would inside any `FlatMap`, instead of being reported as a defect. A body
that calls `runtime.Goexit`, `FailNow`, `Fatal`, `Fatalf`, `SkipNow`, `Skip` or
`Skipf` itself is therefore declined. One that reaches it through a function it
calls cannot be seen.

## What is translated

- `x := do.Await(fx)`, `do.Await(fx)`, `x = do.Await(fx)`, `return
  do.Await(fx)`, `var x = do.Await(fx)` and `do.Fail(e)`;
- an `Await` inside an expression, awaited before the statement — left to right,
  arguments before the call they are passed to — provided no other call is
  written before it in the same statement, and it is not on the right of `&&` or
  `||`;
- `if`, `else if`, `switch`, type switches and blocks whose branches await, with
  an unlabeled `break` leaving a translated `switch`;
- `for` loops and ranges over an integer, a slice, an array, a pointer to an
  array or a channel whose bodies await, with `break`, `continue` and `return`
  inside them. Each iteration is a function call whose parameters are that
  iteration's variables, so a closure made in one iteration keeps seeing its
  own, as Go has since 1.22; and every iteration goes through `Suspend`, so a
  million of them add no Go stack;
- nested bodies, each rewritten on its own.

## What is declined

Run with `EFFECTGO_EXPLAIN=1` to see every declined body and why.

- a step inside a `select` or a `go` statement, or in a loop's init, condition
  or post statement;
- a step inside a range over a map, a string or an iterator function: a map's
  iteration tolerates deletion mid-way in a way a snapshot does not, a string's
  decodes runes, and an iterator's `yield` cannot wait for an effect;
- a body that defers a call, or uses a label, `goto` or `fallthrough`;
- `do` used other than as the receiver of `Await` or `Fail`, or inside a
  function literal;
- a body that is not a function literal written at the call — one passed
  through a helper;
- a body whose values have a type the file cannot name, such as one unexported
  from another package.

## Checking it

The claim that a rewrite changes nothing is checked, not assumed: run the test
suite both ways. This repository's CI does, and so does the rewriter's own suite,
on a module of bodies chosen for what a rewrite could get wrong.
