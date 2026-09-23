package effect

import (
	"fmt"
	"io"
	"strings"
)

// String renders the complete cause tree without panic stacks.
func (c Cause[E]) String() string {
	return renderCause(c, false)
}

// Format supports %v and %s for concise output and %+v for panic stacks.
func (c Cause[E]) Format(state fmt.State, verb rune) {
	_, _ = io.WriteString(state, renderCause(c, state.Flag('+')))
}

// textLine is one output line and its nesting depth. Recording depths and
// indenting once at the end keeps rendering linear in the size of the tree,
// which matters because a long chain of finalizer defects composes into a very
// deep Then spine.
type textLine struct {
	depth int
	text  string
}

type renderAction uint8

const (
	renderNode renderAction = iota
	separateBranches
	closeComposite
)

type renderStep[E any] struct {
	action renderAction
	cause  Cause[E]
	depth  int
}

// renderCause walks the tree with an explicit work list so rendering remains
// total and stack-safe for any cause the runtime can build.
func renderCause[E any](root Cause[E], includeStacks bool) string {
	lines := make([]textLine, 0, 1)
	steps := []renderStep[E]{{cause: root}}
	for len(steps) > 0 {
		current := steps[len(steps)-1]
		steps = steps[:len(steps)-1]

		switch current.action {
		case separateBranches:
			lines[len(lines)-1].text += ","
		case closeComposite:
			lines = append(lines, textLine{depth: current.depth, text: ")"})
		default:
			lines, steps = renderStepOf(current, includeStacks, lines, steps)
		}
	}
	return joinLines(lines)
}

func renderStepOf[E any](
	current renderStep[E],
	includeStacks bool,
	lines []textLine,
	steps []renderStep[E],
) ([]textLine, []renderStep[E]) {
	node := current.cause
	if !isCompositeKind(node.Kind()) {
		return append(lines, renderLeaf(node, current.depth, includeStacks)...), steps
	}

	lines = append(lines, textLine{depth: current.depth, text: node.Kind().String() + "("})
	left, right := node.branches()
	childDepth := current.depth + 1
	return lines, append(steps,
		renderStep[E]{action: closeComposite, depth: current.depth},
		renderStep[E]{cause: right, depth: childDepth},
		renderStep[E]{action: separateBranches},
		renderStep[E]{cause: left, depth: childDepth},
	)
}

func renderLeaf[E any](leaf Cause[E], depth int, includeStacks bool) []textLine {
	if defect, ok := leaf.Defect(); ok {
		return renderDefect(defect, depth, includeStacks)
	}
	return []textLine{{depth: depth, text: renderLeafText(leaf)}}
}

func renderLeafText[E any](leaf Cause[E]) string {
	if failure, ok := leaf.Failure(); ok {
		return fmt.Sprintf("%s(%v)%s", CauseFailure, failure, renderOrigin(leaf))
	}
	if interruption, ok := leaf.Interruption(); ok {
		return fmt.Sprintf("%s(%v)%s", CauseInterrupt, interruption.Cause, renderOrigin(leaf))
	}
	return leaf.Kind().String()
}

// renderOrigin is where a failure came from, appended to it.
//
// On the same line as the failure, because the two are read together: a reader
// asking what went wrong is about to ask where, and a rendering that made them
// look up the second somewhere else would be a rendering nobody uses.
func renderOrigin[E any](leaf Cause[E]) string {
	origin := leaf.Origin()
	if !origin.IsKnown() {
		return ""
	}
	return " at " + origin.String()
}

func renderDefect(defect Defect, depth int, includeStacks bool) []textLine {
	lines := []textLine{{depth: depth, text: fmt.Sprintf("%s(%v)", CauseDefect, defect.Value)}}
	if !includeStacks || defect.Stack == "" {
		return lines
	}
	for _, line := range strings.Split(strings.TrimSuffix(defect.Stack, "\n"), "\n") {
		lines = append(lines, textLine{depth: depth + 1, text: line})
	}
	return lines
}

func joinLines(lines []textLine) string {
	var output strings.Builder
	for index, line := range lines {
		if index > 0 {
			output.WriteString("\n")
		}
		output.WriteString(strings.Repeat("  ", line.depth))
		output.WriteString(line.text)
	}
	return output.String()
}
