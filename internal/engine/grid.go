// TSV serialization for the sheet engine: reading a .tsvt grid into a Grid of
// raw cell strings and writing a computed Grid back out. The A1 reference
// resolution and formula evaluation live in model.go and eval.go; this file is
// just the tab-separated line format. (The package doc is in model.go.)
package engine

import (
	"bufio"
	"io"
	"strings"

	"github.com/tsvsheet/go-tsvsheet/internal/constants"
)

// tab is the single field separator; newline terminates a row.
const (
	tab     = "\t"
	newline = "\n"
)

// Grid is a rectangular value grid indexed [row][col], 0-based. Cells are raw
// strings: a literal's own text on input, or a formula cell's computed value
// after ComputeAt.
type Grid [][]string

// ReadTSV reads a tab-separated value grid. Rows are newline-separated; a
// trailing newline does not add an empty row. Full-line comments are skipped
// and do not occupy a grid row: a leading `#!` on the first line (a shebang, so
// a .tsvt can be `chmod +x` and run via `#!/usr/bin/env tsvsheet`) and any line
// beginning with `# ` (hash-space). An error-value cell like `#N/A` (hash then a
// non-space) is data, not a comment. A read failure surfaces as ErrReadInput.
func ReadTSV(r io.Reader) (Grid, error) {
	grid := Grid{}
	err := scanLines(r, func(text string, isComment bool) {
		if !isComment {
			grid = append(grid, strings.Split(text, tab))
		}
	})
	if err != nil {
		return nil, err
	}
	return grid, nil
}

// scanLines reads r line by line, calling fn with each line's text and whether
// it is a comment line (a first-line `#!` shebang or a `# ` hash-space line —
// the lines ReadTSV skips and Document preserves). A read failure surfaces as
// ErrReadInput.
func scanLines(r io.Reader, fn func(text string, isComment bool)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLineBytes)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		text := scanner.Text()
		isShebang := lineNum == 1 && strings.HasPrefix(text, "#!")
		fn(text, isShebang || strings.HasPrefix(text, "# "))
	}
	if err := scanner.Err(); err != nil {
		return constants.ErrReadInput.With(err)
	}
	return nil
}

// maxLineBytes bounds a single scanned row (1 MiB) so a pathological input
// cannot exhaust memory silently.
const maxLineBytes = 1 << 20

// WriteTSV writes the grid as tab-separated rows, each terminated by a newline.
// A write failure surfaces as constants.ErrWriteFile. Callers wanting buffering
// pass a bufio.Writer; WriteTSV writes each row directly so a write error is
// reported at its source.
func WriteTSV(w io.Writer, g Grid) error {
	for _, row := range g {
		if _, err := io.WriteString(w, strings.Join(row, tab)+newline); err != nil {
			return constants.ErrWriteFile.With(err)
		}
	}
	return nil
}
