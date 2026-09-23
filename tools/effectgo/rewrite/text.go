package rewrite

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
)

// edit replaces the source between two offsets.
type edit struct {
	start, end int
	text       string
}

// source is one file's text and the positions that index it.
type source struct {
	fset     *token.FileSet
	file     *token.File
	text     []byte
	filename string
}

func (src *source) offset(pos token.Pos) int { return src.file.Offset(pos) }

// line is a directive that makes what follows it report the position of node
// in the original file, so a stack trace, a coverage profile and a failure's
// origin name the line that was written rather than the one generated.
func (src *source) line(node ast.Node) string {
	position := src.fset.Position(node.Pos())
	return fmt.Sprintf("/*line %s:%d:%d*/", src.filename, position.Line, position.Column)
}

// splice is the text of [start, end) with edits applied. Edits must lie inside
// the range and must not overlap.
func (src *source) splice(start, end int, edits []edit) string {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out strings.Builder
	at := start
	for _, change := range edits {
		if change.start < at || change.end > end {
			continue
		}
		out.Write(src.text[at:change.start])
		out.WriteString(change.text)
		at = change.end
	}
	out.Write(src.text[at:end])
	return out.String()
}
