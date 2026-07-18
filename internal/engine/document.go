// The source-preserving document layer: a Document is a Sheet plus the file's
// physical line layout, so comment and shebang lines — which ReadTSV drops from
// the grid — survive editing and serialization. Every frontend that writes a
// .tsvt back out goes through Document.Text; no frontend serializes a grid
// (capability engine-doc-text).
package engine

import (
	"bytes"
	"strings"
)

// docLine is one physical line of the source: either a comment/shebang line
// held verbatim, or a marker consuming the next grid row.
type docLine struct {
	comment string
	isRow   bool
}

// Document is a parsed .tsvt file with its line layout retained. The zero
// value is an empty document. Document is immutable: editing operations return
// a new Document.
type Document struct {
	sheet  Sheet
	layout []docLine
}

// ParseDocument reads a .tsvt file like Parse, additionally recording the
// physical line layout so comment and shebang lines are preserved by Text.
func ParseDocument(src []byte) (Document, error) {
	var layout []docLine
	grid := Grid{}
	err := scanLines(bytes.NewReader(src), func(text string, isComment bool) {
		if isComment {
			layout = append(layout, docLine{comment: text})
			return
		}
		layout = append(layout, docLine{isRow: true})
		grid = append(grid, strings.Split(text, tab))
	})
	if err != nil {
		return Document{}, err
	}
	sheet, err := sheetFromGrid(grid)
	if err != nil {
		return Document{}, err
	}
	return Document{sheet: sheet, layout: layout}, nil
}

// Sheet returns the parsed sheet the document wraps.
func (d Document) Sheet() Sheet { return d.sheet }

// Text serializes the document canonically: lines in layout order — comments
// verbatim, grid rows tab-joined — every line newline-terminated. For input
// already in canonical form, ParseDocument followed by Text is byte-identity.
func (d Document) Text() []byte {
	rows := d.sheet.Source()
	var b strings.Builder
	next := rowIndex(0)
	for _, line := range d.layout {
		text := line.comment
		if line.isRow {
			text = strings.Join(rows[next], tab)
			next++
		}
		_, _ = b.WriteString(text)
		_, _ = b.WriteString(newline)
	}
	return []byte(b.String())
}

// SetCell returns a new document with the cell at addr replaced (Sheet.Set
// semantics); rows the grid grew by are appended to the layout, after any
// trailing comments — they did not exist when those comments were written.
func (d Document) SetCell(at Address, text string, limits Limits) (Document, error) {
	sheet, err := d.sheet.Set(at, text, limits)
	if err != nil {
		return Document{}, err
	}
	grown := rowIndex(len(sheet.cells) - len(d.sheet.cells))
	return Document{sheet: sheet, layout: appendMarkers(d.layout, grown)}, nil
}

// InsertRow returns a new document with a blank row inserted before at.Row
// (Sheet.InsertRow semantics); comments keep the between-row gap they were
// written in. A no-op on the sheet is a no-op on the document.
func (d Document) InsertRow(at Address) Document {
	sheet := d.sheet.InsertRow(at)
	if len(sheet.cells) == len(d.sheet.cells) {
		return d
	}
	return Document{sheet: sheet, layout: spliceMarker(d.layout, rowIndex(min(at.Row, len(d.sheet.cells))))}
}

// DeleteRow returns a new document with row at.Row removed (Sheet.DeleteRow
// semantics); comment lines are never deleted by a row deletion.
func (d Document) DeleteRow(at Address) Document {
	sheet := d.sheet.DeleteRow(at)
	if len(sheet.cells) == len(d.sheet.cells) {
		return d
	}
	idx := markerIndex(d.layout, rowIndex(at.Row))
	layout := make([]docLine, 0, len(d.layout)-1)
	layout = append(layout, d.layout[:idx]...)
	return Document{sheet: sheet, layout: append(layout, d.layout[idx+1:]...)}
}

// InsertCol returns a new document with a blank column inserted before at.Col;
// column operations never touch the line layout.
func (d Document) InsertCol(at Address) Document {
	return Document{sheet: d.sheet.InsertCol(at), layout: d.layout}
}

// DeleteCol returns a new document with column at.Col removed; column
// operations never touch the line layout.
func (d Document) DeleteCol(at Address) Document {
	return Document{sheet: d.sheet.DeleteCol(at), layout: d.layout}
}

// appendMarkers appends grown row markers to a copy of the layout.
func appendMarkers(layout []docLine, grown rowIndex) []docLine {
	out := make([]docLine, 0, len(layout)+int(grown))
	out = append(out, layout...)
	for range grown {
		out = append(out, docLine{isRow: true})
	}
	return out
}

// spliceMarker inserts a row marker before the row-th existing row marker
// (appending when row equals the row count), leaving comment anchoring intact.
func spliceMarker(layout []docLine, row rowIndex) []docLine {
	idx := markerIndex(layout, row)
	out := make([]docLine, 0, len(layout)+1)
	out = append(out, layout[:idx]...)
	out = append(out, docLine{isRow: true})
	return append(out, layout[idx:]...)
}

// markerIndex is the layout index of the row-th row marker, or len(layout)
// when there is no such marker.
func markerIndex(layout []docLine, row rowIndex) int {
	seen := rowIndex(0)
	for i, line := range layout {
		if !line.isRow {
			continue
		}
		if seen == row {
			return i
		}
		seen++
	}
	return len(layout)
}
