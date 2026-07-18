package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tsvsheet/go-tsvsheet/internal/constants"
	"github.com/tsvsheet/go-tsvsheet/internal/engine"
)

// parseDoc parses src as a document, failing the test on error.
func parseDoc(t *testing.T, src string) engine.Document {
	t.Helper()
	doc, err := engine.ParseDocument([]byte(src))
	require.NoError(t, err)
	return doc
}

func TestParseDocumentCanonicalizesMissingTrailingNewline(t *testing.T) {
	assert.Equal(t, "a\tb\n", string(parseDoc(t, "a\tb").Text()))
}

func TestParseDocumentRejectsMalformedFormula(t *testing.T) {
	_, err := engine.ParseDocument([]byte("=(\n"))
	require.Error(t, err)
}

func TestParseDocumentReadFailure(t *testing.T) {
	// ParseDocument takes bytes, so the only read path is the line scanner's
	// too-long-line failure.
	long := make([]byte, 2<<20)
	for i := range long {
		long[i] = 'x'
	}
	_, err := engine.ParseDocument(long)
	require.Error(t, err)
	assert.ErrorIs(t, err, constants.ErrReadInput)
}

func TestDocumentSetCellPreservesComments(t *testing.T) {
	doc := parseDoc(t, "# header\na\tb\n# middle\n1\t2\n")
	edited, err := doc.SetCell(engine.Address{Row: 1, Col: 1}, "=A2*2", engine.DefaultLimits())
	require.NoError(t, err)
	assert.Equal(t, "# header\na\tb\n# middle\n1\t=A2*2\n", string(edited.Text()))
	// The original document is unchanged (immutability).
	assert.Equal(t, "# header\na\tb\n# middle\n1\t2\n", string(doc.Text()))
}

func TestDocumentSetCellGrowsAppendingRows(t *testing.T) {
	doc := parseDoc(t, "a\n# tail comment\n")
	edited, err := doc.SetCell(engine.Address{Row: 2, Col: 0}, "z", engine.DefaultLimits())
	require.NoError(t, err)
	// Grown rows append at the end; the trailing comment keeps its position
	// relative to the rows that existed when it was written.
	assert.Equal(t, "a\n# tail comment\n\nz\n", string(edited.Text()))
}

func TestDocumentSetCellError(t *testing.T) {
	doc := parseDoc(t, "a\tb\n")
	_, err := doc.SetCell(engine.Address{Row: 0, Col: 0}, "=(", engine.DefaultLimits())
	require.Error(t, err)
	_, err = doc.SetCell(engine.Address{Row: -1, Col: 0}, "x", engine.DefaultLimits())
	require.Error(t, err)
	assert.ErrorIs(t, err, constants.ErrInvalidValue)
}

func TestDocumentInsertRowAnchorsComments(t *testing.T) {
	doc := parseDoc(t, "# top\na\n# between\nb\n")
	// Insert before row 1 ("b"): the new blank row lands after "# between",
	// which stays anchored to the gap it was written in.
	assert.Equal(t, "# top\na\n# between\n\nb\n", string(doc.InsertRow(engine.Address{Row: 1}).Text()))
	// Out-of-range inserts are no-ops on the layout too.
	assert.Equal(t, "# top\na\n# between\nb\n", string(doc.InsertRow(engine.Address{Row: -1}).Text()))
	// Inserting past the last row appends.
	assert.Equal(t, "# top\na\n# between\nb\n\n", string(doc.InsertRow(engine.Address{Row: 9}).Text()))
}

func TestDocumentDeleteRowKeepsComments(t *testing.T) {
	doc := parseDoc(t, "# top\na\n# between\nb\n")
	assert.Equal(t, "# top\n# between\nb\n", string(doc.DeleteRow(engine.Address{Row: 0}).Text()))
	assert.Equal(t, "# top\na\n# between\n", string(doc.DeleteRow(engine.Address{Row: 1}).Text()))
	// Out-of-range deletes are no-ops.
	assert.Equal(t, "# top\na\n# between\nb\n", string(doc.DeleteRow(engine.Address{Row: 9}).Text()))
}

func TestDocumentColumnOpsLeaveLayoutAlone(t *testing.T) {
	doc := parseDoc(t, "# c\na\tb\n1\t2\n")
	assert.Equal(t, "# c\na\t\tb\n1\t\t2\n", string(doc.InsertCol(engine.Address{Col: 1}).Text()))
	assert.Equal(t, "# c\nb\n2\n", string(doc.DeleteCol(engine.Address{Col: 0}).Text()))
}

func TestDocumentSheetExposesParsedSheet(t *testing.T) {
	doc := parseDoc(t, "# c\n1\t=A1+1\n")
	assert.Equal(t, engine.Grid{{"1", "2"}}, doc.Sheet().Compute())
}

func TestDocumentEditStability(t *testing.T) {
	// Reparsing an edited document's text yields the identical text: the
	// canonical form is a fixed point through every operation class.
	doc := parseDoc(t, "#!/usr/bin/env tsvsheet\n# prices\na\tb\n# note\n1\t=A2\n")
	for name, edited := range map[string]engine.Document{
		"setCell":    mustSet(t, doc, engine.Address{Row: 1, Col: 0}, "42"),
		"insert row": doc.InsertRow(engine.Address{Row: 1}),
		"delete row": doc.DeleteRow(engine.Address{Row: 0}),
		"insert col": doc.InsertCol(engine.Address{Col: 0}),
		"delete col": doc.DeleteCol(engine.Address{Col: 1}),
	} {
		t.Run(name, func(t *testing.T) {
			text := edited.Text()
			assert.Equal(t, string(text), string(parseDoc(t, string(text)).Text()))
		})
	}
}

// mustSet applies SetCell, failing the test on error.
func mustSet(t *testing.T, doc engine.Document, at engine.Address, text string) engine.Document {
	t.Helper()
	out, err := doc.SetCell(at, text, engine.DefaultLimits())
	require.NoError(t, err)
	return out
}

func TestParseDocumentTextRoundTripsCanonicalSource(t *testing.T) {
	for name, src := range map[string]string{
		"plain grid":       "a\tb\n1\t=A1\n",
		"shebang":          "#!/usr/bin/env tsvsheet\na\tb\n",
		"leading comment":  "# budget sheet\na\tb\n",
		"interior comment": "a\tb\n# note between rows\n1\t2\n",
		"trailing comment": "a\tb\n# the end\n",
		"comments only":    "# nothing but\n# comments\n",
		"empty":            "",
		"ragged rows":      "a\n1\t2\t3\nx\ty\n",
		"error-value cell": "#N/A\tb\n",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, src, string(parseDoc(t, src).Text()))
		})
	}
}
