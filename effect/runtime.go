package effect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mbauer83/effect-golang/effect/capability"
	"github.com/mbauer83/effect-golang/effect/config"
	"github.com/mbauer83/effect-golang/effect/internal/lifetime"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
	"github.com/mbauer83/effect-golang/effect/internal/platform"
	runtimecore "github.com/mbauer83/effect-golang/effect/internal/runtime"
)

// Runtime interprets effects with one immutable set of base capabilities and
// owns the root lifetime that bounds every detached fiber and root-registered
// resource it creates.
//
// A capability override belongs to one Runtime and never mutates package-global
// state, so concurrent tests using different clocks or filesystems cannot
// interfere with each other.
type Runtime struct {
	state  *runtimecore.State
	root   *lifetime.Scope
	ledger *lifetime.Ledger
}

// NewRuntime constructs an interpreter with live defaults and local overrides.
// A nil capability is rejected at construction rather than discovered as a
// panic while an effect is running.
func NewRuntime(options ...RuntimeOption) (*Runtime, error) {
	configured := runtimeConfig{capabilities: capability.Set{
		Clock:       platform.LiveClock{},
		FileSystem:  platform.LiveFileSystem{},
		Logger:      platform.LiveLogger{Handler: slog.Default().Handler()},
		Diagnostics: platform.LiveDiagnostics{},
		// The environment, because it is the one place every deployment can
		// put a setting and reading it has no effect on anything. A program
		// that reads from a document or a store says so with
		// WithConfigSource.
		ConfigSource: config.Environment(),
	}}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("effect: RuntimeOption must not be nil")
		}
		if err := option.apply(&configured); err != nil {
			return nil, err
		}
	}

	root := lifetime.NewScope(context.Background())
	return &Runtime{
		state:  runtimecore.NewState(configured.capabilities, root, configured.ledger),
		root:   root,
		ledger: configured.ledger,
	}, nil
}

// Run evaluates fx with env using live default capabilities. It interprets the
// effect in an ephemeral runtime whose root lifetime is closed, with every
// resource released, before Run returns.
func Run[R, E, A any](ctx context.Context, env R, fx Effect[R, E, A]) Exit[E, A] {
	runtime, err := NewRuntime()
	if err != nil {
		panic(err)
	}
	exit := runtime.Run(ctx, env, fx)
	return composeCleanup(exit, runtime.close(ctx).node)
}

// Run evaluates fx with env and this Runtime's capabilities.
//
// The effect is interpreted inside a lifetime of its own, so resources it
// acquires and fibers it forks are released and awaited before Run returns.
// Work deliberately detached to the runtime root outlives it and is bounded by
// Close instead.
func (runtime *Runtime) Run[R, E, A any](ctx context.Context, env R, fx Effect[R, E, A]) Exit[E, A] {
	if runtime == nil || runtime.state == nil {
		panic("effect: nil Runtime")
	}

	scope := lifetime.NewScope(ctx)
	exit := fx.run(scope.Context(), runtime.state.WithScope(scope), env)
	cleanup := scope.Close(ctx, exit.erased, lifetime.ErrScopeClosed)
	return composeCleanup(exit, cleanup)
}

// Close ends the runtime's root lifetime. It interrupts and awaits detached
// work, releases root-registered resources in reverse acquisition order,
// flushes capabilities that buffer, and reports the composed cleanup cause.
//
// The returned cause has no typed failures: a release workflow cannot widen an
// application's error channel. It may still contain defects.
func (runtime *Runtime) Close(ctx context.Context) Cause[Never] {
	if runtime == nil || runtime.state == nil {
		panic("effect: nil Runtime")
	}
	return runtime.close(ctx)
}

// LiveWork reports the fibers and scoped resources this Runtime still owns.
//
// It is meaningful only for a Runtime built WithDebugTracking; otherwise it
// reports nothing live, because nothing was counted. A test can assert that a
// program left no work behind, and Close reports any remainder to diagnostics.
func (runtime *Runtime) LiveWork() LiveWork {
	return runtime.ledger.Live()
}

func (runtime *Runtime) close(ctx context.Context) Cause[Never] {
	runtime.reportRemainingWork(ctx)
	closingAt := runtime.state.EmitStart(ctx, capability.EventRuntimeClosing)
	cleanup := runtime.root.Close(ctx, outcome.Success(Unit{}), lifetime.ErrRuntimeClosed)
	cleanup = cleanup.Then(flushCapabilities(ctx, runtime.state))
	runtime.state.EmitEnd(
		ctx,
		capability.EventRuntimeClosed,
		closingAt,
		outcome.CleanupStatus(cleanup),
	)
	return Cause[Never]{node: cleanup}
}

// reportRemainingWork tells diagnostics what the program left running before
// Close interrupts it, which is the only moment at which that count is still
// observable.
func (runtime *Runtime) reportRemainingWork(ctx context.Context) {
	remaining := runtime.ledger.Live()
	if remaining.IsEmpty() {
		return
	}
	runtime.state.Report(ctx, capability.RuntimeFault{
		Component: capability.FaultRuntime,
		Operation: "close",
		Err: fmt.Errorf(
			"effect: %d fiber(s) and %d resource(s) were still owned at Close",
			remaining.Fibers,
			remaining.Resources,
		),
	})
}

// composeCleanup appends a lifetime's cleanup cause to an exit. Cleanup causes
// carry no typed failure, so retyping them to E cannot lose information.
func composeCleanup[E, A any](exit Exit[E, A], cleanup outcome.Cause) Exit[E, A] {
	if cleanup.IsEmpty() {
		return exit
	}
	return Exit[E, A]{erased: outcome.Failure(exit.erased.Cause().Then(cleanup))}
}

func flushCapabilities(ctx context.Context, state *runtimecore.State) outcome.Cause {
	capabilities := state.Capabilities()
	cause := flushOne(ctx, capabilities.Observer, capability.FaultObserver)
	return cause.Then(flushOne(ctx, capabilities.Logger, capability.FaultLogger))
}

func flushOne(ctx context.Context, subject any, component capability.FaultComponent) outcome.Cause {
	flusher, buffers := subject.(capability.Flusher)
	if !buffers {
		return outcome.Cause{}
	}
	if err := flusher.Flush(ctx); err != nil {
		return outcome.DieCause(outcome.CaptureDefect(capability.RuntimeFault{
			Component: component,
			Operation: "flush",
			Err:       err,
		}))
	}
	return outcome.Cause{}
}
