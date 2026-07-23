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

func TestStructuralEditsPreserveUntouchedFormulaText(t *testing.T) {
	// A structural edit must not reformat formulas whose references it did not
	// shift — rewriting "=A1+B1" to "=A1 + B1" is diff noise on untouched
	// lines, against the format's core promise. Only formulas whose
	// references actually move are re-rendered canonically.
	doc := parseDoc(t, "1\t2\n=A1+B1\t= A1 *2\n")
	// Inserting a row BELOW every referenced cell shifts nothing.
	assert.Equal(t, "1\t2\n=A1+B1\t= A1 *2\n\t\n", string(doc.InsertRow(engine.Address{Row: 2}).Text()))
	// Inserting a row above shifts both formulas' references: those cells are
	// re-rendered canonically, and only those.
	assert.Equal(t, "\t\n1\t2\n=A2 + B2\t=A2 * 2\n", string(doc.InsertRow(engine.Address{Row: 0}).Text()))
}

func TestStructuralEditsRerenderEveryShiftedNodeForm(t *testing.T) {
	// When references DO shift, the formula is re-rendered canonically through
	// every node form — unary, call, percent, concatenation, and the loosest-
	// binding pipe spelling, which re-parenthesizes where an operator would
	// capture its final call.
	doc := parseDoc(t, "x\n=-A2 + sum(A2:A2) & B2% & (A2 | len())\n7\t8\n")
	moved := doc.InsertRow(engine.Address{Row: 0})
	assert.Equal(t, "\t\nx\n=-A3 + sum(A3:A3) & B3% & (A3 | len)\n7\t8\n", string(moved.Text()))
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
