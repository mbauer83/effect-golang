package outcome

// CauseFolder defines how FoldCause evaluates each node of an erased cause.
// Keeping the handlers in one value makes call sites readable and lets the
// fold stay total over every kind.
type CauseFolder[A any] struct {
	Empty        func() A
	Failure      func(any) A
	Defect       func(Defect) A
	Interruption func(Interruption) A
	Then         func(A, A) A
	Both         func(A, A) A
}

type foldStage uint8

const (
	evaluateNode foldStage = iota
	combineResults
)

// FoldCause eliminates a cause without recursively growing the Go call stack,
// so an arbitrarily deep composition remains safe to inspect and render.
func FoldCause[A any](root Cause, folder CauseFolder[A]) A {
	type frame struct {
		cause Cause
		stage foldStage
	}

	frames := []frame{{cause: root}}
	results := make([]A, 0, 1)
	for len(frames) > 0 {
		current := frames[len(frames)-1]
		frames = frames[:len(frames)-1]

		if current.stage == combineResults {
			left, right := results[len(results)-2], results[len(results)-1]
			results = results[:len(results)-2]
			results = append(results, combineFolded(folder, current.cause.Kind, left, right))
			continue
		}

		switch current.cause.Kind {
		case CauseThen, CauseBoth:
			frames = append(frames,
				frame{cause: current.cause, stage: combineResults},
				frame{cause: Child(current.cause.Right)},
				frame{cause: Child(current.cause.Left)},
			)
		default:
			results = append(results, foldLeaf(folder, current.cause))
		}
	}
	return results[0]
}

func foldLeaf[A any](folder CauseFolder[A], leaf Cause) A {
	switch leaf.Kind {
	case CauseFailure:
		return folder.Failure(leaf.Failure)
	case CauseDefect:
		return folder.Defect(leaf.Defect)
	case CauseInterrupted:
		return folder.Interruption(leaf.Interruption)
	default:
		return folder.Empty()
	}
}

func combineFolded[A any](folder CauseFolder[A], kind CauseKind, left A, right A) A {
	if kind == CauseThen {
		return folder.Then(left, right)
	}
	return folder.Both(left, right)
}

// VisitCause walks every node in left-to-right order until visit returns false.
func VisitCause(root Cause, visit func(Cause) bool) {
	stack := []Cause{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !visit(node) {
			return
		}
		if node.Right != nil {
			stack = append(stack, *node.Right)
		}
		if node.Left != nil {
			stack = append(stack, *node.Left)
		}
	}
}

// MapCauseFailure rewrites every typed failure while preserving cause structure.
func MapCauseFailure(root Cause, transform func(any) any) Cause {
	return FoldCause(root, CauseFolder[Cause]{
		Empty:        func() Cause { return Cause{} },
		Failure:      func(failure any) Cause { return FailCause(transform(failure)) },
		Defect:       DieCause,
		Interruption: func(interruption Interruption) Cause { return InterruptCause(interruption.Cause) },
		Then:         Cause.Then,
		Both:         Cause.Both,
	})
}

// FailuresAsDefects rewrites every typed failure as a defect while preserving
// cause structure, so a workflow that must not fail with a typed error can
// still report a real failure.
func FailuresAsDefects(root Cause) Cause {
	return FoldCause(root, CauseFolder[Cause]{
		Empty: func() Cause {
			return Cause{}
		},
		Failure: func(failure any) Cause {
			return DieCause(Defect{Value: failure})
		},
		Defect: DieCause,
		Interruption: func(interruption Interruption) Cause {
			return InterruptCause(interruption.Cause)
		},
		Then: Cause.Then,
		Both: Cause.Both,
	})
}
