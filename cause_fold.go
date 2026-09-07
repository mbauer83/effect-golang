package effect

import (
	"github.com/mbauer83/effect-golang/internal/outcome"
)

// CauseFolder defines how Fold evaluates each cause node. Keeping the handlers
// in one value makes call sites readable and allows the fold to remain total.
type CauseFolder[E, A any] struct {
	Empty        func() A
	Failure      func(E) A
	Defect       func(Defect) A
	Interruption func(Interruption) A
	Then         func(A, A) A
	Both         func(A, A) A
}

// Fold eliminates a cause without recursively growing the Go call stack.
func (c Cause[E]) Fold[A any](folder CauseFolder[E, A]) A {
	return outcome.FoldCause(c.node, erasedFolder(folder))
}

// Failures returns every typed failure from left to right.
func (c Cause[E]) Failures() []E {
	return collectFromCause(c, func(node Cause[E]) (E, bool) {
		return node.Failure()
	})
}

// Defects returns every defect from left to right.
func (c Cause[E]) Defects() []Defect {
	return collectFromCause(c, func(node Cause[E]) (Defect, bool) {
		return node.Defect()
	})
}

// Interruptions returns every interruption from left to right.
func (c Cause[E]) Interruptions() []Interruption {
	return collectFromCause(c, func(node Cause[E]) (Interruption, bool) {
		return node.Interruption()
	})
}

func collectFromCause[E, A any](root Cause[E], selectValue func(Cause[E]) (A, bool)) []A {
	values := make([]A, 0)
	root.visit(func(node Cause[E]) bool {
		if value, ok := selectValue(node); ok {
			values = append(values, value)
		}
		return true
	})
	return values
}

// IsInterruptedOnly reports whether c is non-empty and every leaf is Interrupt.
func (c Cause[E]) IsInterruptedOnly() bool {
	return c.hasOnlyLeaves(CauseInterrupted)
}

// IsFailureOnly reports whether c is non-empty and every leaf is Fail.
func (c Cause[E]) IsFailureOnly() bool {
	return c.hasOnlyLeaves(CauseFailure)
}

// ContainsDefect reports whether any node is Die.
func (c Cause[E]) ContainsDefect() bool {
	return c.containsKind(CauseDefect)
}

func (c Cause[E]) hasOnlyLeaves(kind CauseKind) bool {
	if c.IsEmpty() {
		return false
	}
	matching := true
	c.visit(func(node Cause[E]) bool {
		if !isCompositeKind(node.Kind()) {
			matching = matching && node.Kind() == kind
		}
		return matching
	})
	return matching
}

func (c Cause[E]) containsKind(kind CauseKind) bool {
	found := false
	c.visit(func(node Cause[E]) bool {
		found = node.Kind() == kind
		return !found
	})
	return found
}

func isCompositeKind(kind CauseKind) bool {
	return kind == CauseThen || kind == CauseBoth
}

func (c Cause[E]) visit(visitor func(Cause[E]) bool) {
	outcome.VisitCause(c.node, func(node outcome.Cause) bool {
		return visitor(Cause[E]{node: node})
	})
}
