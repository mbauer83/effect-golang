package lifetime

import (
	"context"
	"github.com/mbauer83/effect-golang/effect/internal/outcome"
	"sync"
)

// Finalizer releases one registered resource. It receives the exit its scope is
// closing with and returns the cause of its own failure, which is empty on
// success.
//
// A finalizer has no typed-failure channel: one scope may hold unrelated
// resources whose release errors have no common type, and those types cannot be
// added to an effect's E after composition. A finalizer may still defect, and
// that defect is preserved in the closing cause.
type Finalizer func(context.Context, outcome.Exit) outcome.Cause

type scopeStatus uint8

const (
	scopeOpen scopeStatus = iota
	scopeClosing
	scopeClosed
)

// Scope owns one lifetime boundary: the fibers started inside it and the
// finalizers of the resources acquired inside it.
//
// Closing a scope prevents further registration, cancels the work it owns,
// waits for that work to finish, and only then releases resources in reverse
// acquisition order. That order matters because a child may still be using a
// resource its scope acquired.
type Scope struct {
	mutex      sync.Mutex
	status     scopeStatus
	closeExit  outcome.Exit
	ctx        context.Context
	cancel     context.CancelCauseFunc
	children   sync.WaitGroup
	finalizers []Finalizer
}

// NewScope opens a scope whose lifetime is bounded by parent.
func NewScope(parent context.Context) *Scope {
	ctx, cancel := context.WithCancelCause(parent)
	return &Scope{ctx: ctx, cancel: cancel}
}

// Context returns the cancelable context shared by everything this scope owns.
func (scope *Scope) Context() context.Context {
	return scope.ctx
}

// AddFinalizer registers finalizer for release when the scope closes.
//
// Registration is atomic with respect to closure: a resource acquired while the
// scope was still open is either registered or, when closure has already begun,
// released immediately. It can therefore never leak. The bool reports whether
// the scope accepted ownership.
func (scope *Scope) AddFinalizer(ctx context.Context, finalizer Finalizer) (bool, outcome.Cause) {
	scope.mutex.Lock()
	if scope.status == scopeOpen {
		scope.finalizers = append(scope.finalizers, finalizer)
		scope.mutex.Unlock()
		return true, outcome.Cause{}
	}
	closeExit := scope.closeExit
	scope.mutex.Unlock()
	return false, runFinalizer(context.WithoutCancel(ctx), finalizer, closeExit)
}

// Fork starts work owned by this scope, so scope closure waits for it. The bool
// reports whether the scope accepted ownership; a rejected caller must not
// start the work at all.
func (scope *Scope) Fork(run func()) bool {
	scope.mutex.Lock()
	defer scope.mutex.Unlock()
	if scope.status != scopeOpen {
		return false
	}
	scope.children.Go(run)
	return true
}

// Interrupt cancels everything the scope owns without closing it. A parallel
// composition uses it to abandon branches whose results it no longer needs
// while it continues to wait for them.
func (scope *Scope) Interrupt(reason error) {
	scope.cancel(reason)
}

// Close ends the scope's lifetime and returns the composed cause of every
// finalizer that failed. It returns only once all owned work has finished and
// all resources have been released.
func (scope *Scope) Close(ctx context.Context, exit outcome.Exit, reason error) outcome.Cause {
	finalizers, accepted := scope.beginClose(exit)
	if !accepted {
		return outcome.Cause{}
	}

	scope.cancel(reason)
	scope.children.Wait()

	// Cleanup keeps the context's values but drops its cancellation: releasing
	// a resource must not be skipped because the caller was already canceled.
	cleanup := context.WithoutCancel(ctx)
	cause := outcome.Cause{}
	for index := len(finalizers) - 1; index >= 0; index-- {
		cause = cause.Then(runFinalizer(cleanup, finalizers[index], exit))
	}

	scope.finishClose()
	return cause
}

func (scope *Scope) beginClose(exit outcome.Exit) ([]Finalizer, bool) {
	scope.mutex.Lock()
	defer scope.mutex.Unlock()
	if scope.status != scopeOpen {
		return nil, false
	}
	scope.status = scopeClosing
	scope.closeExit = exit
	finalizers := scope.finalizers
	scope.finalizers = nil
	return finalizers, true
}

func (scope *Scope) finishClose() {
	scope.mutex.Lock()
	defer scope.mutex.Unlock()
	scope.status = scopeClosed
}

func runFinalizer(ctx context.Context, finalizer Finalizer, exit outcome.Exit) (cause outcome.Cause) {
	defer func() {
		if panicValue := recover(); panicValue != nil {
			cause = outcome.DieCause(outcome.CaptureDefect(panicValue))
		}
	}()
	return finalizer(ctx, exit)
}
