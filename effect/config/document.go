package config

// Printing what a program needs to be told.

import (
	"fmt"
	"sort"
	"strings"
)

// Document renders expectations as a table, one line per value.
//
// What a program prints when it is asked what it needs, and the reason a
// description is a description rather than a function that reads: a closure
// can answer "here is the value" and only a description can answer "here is
// what I would have asked for". A deployment that has just been told a value
// is missing can be shown the whole list without running anything.
//
//	fmt.Print(config.Document(settings.Expects()))
//
//	  db.host       text        required   the address of the primary
//	  db.password   a secret    required
//	  port          a port      8080       the address to listen on
//	  timeout       a duration  5s
//
// A secret's value is never here: this is built from the description, which
// never held one.
func Document(expects []Expectation) string {
	if len(expects) == 0 {
		return "nothing\n"
	}
	rows := rowsOf(expects)
	widths := widthsOf(rows)

	text := strings.Builder{}
	for _, row := range rows {
		line := strings.Builder{}
		line.WriteString("  ")
		for column, cell := range row {
			line.WriteString(cell)
			if column == len(row)-1 {
				break
			}
			line.WriteString(strings.Repeat(" ", widths[column]-len(cell)+2))
		}
		// Trimmed, because a row whose last column is empty would otherwise
		// carry the padding of the one before it to the end of the line.
		text.WriteString(strings.TrimRight(line.String(), " "))
		text.WriteString("\n")
	}
	return text.String()
}

// rowsOf lays one expectation out as its cells, sorted by path so a long list
// reads as a reference rather than as the order somebody happened to compose
// it in.
func rowsOf(expects []Expectation) [][]string {
	byPath := make([]Expectation, len(expects))
	copy(byPath, expects)
	sort.SliceStable(byPath, func(first int, second int) bool {
		return Render(byPath[first].Path) < Render(byPath[second].Path)
	})

	rows := make([][]string, 0, len(byPath))
	for _, expectation := range byPath {
		rows = append(rows, []string{
			Render(expectation.Path),
			expectation.Type,
			expectationText(expectation),
			expectation.Doc,
		})
	}
	return rows
}

// expectationText says what happens when nobody supplies the value, which is the
// column a deployment reads first.
func expectationText(expectation Expectation) string {
	switch {
	case expectation.Default != "":
		return expectation.Default
	case expectation.Optional:
		return "optional"
	default:
		return "required"
	}
}

func widthsOf(rows [][]string) []int {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for column, cell := range row {
			if len(cell) > widths[column] {
				widths[column] = len(cell)
			}
		}
	}
	return widths
}

// String renders one expectation on its own, for a message about a single
// value.
func (expectation Expectation) String() string {
	line := fmt.Sprintf("%s (%s, %s)",
		Render(expectation.Path), expectation.Type, expectationText(expectation))
	if expectation.Doc == "" {
		return line
	}
	return line + ": " + expectation.Doc
}
