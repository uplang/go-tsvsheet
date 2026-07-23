package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tsvsheet/go-tsvsheet/internal/constants"
	"github.com/tsvsheet/go-tsvsheet/internal/engine"
)

// parse is a test helper that parses a sheet, failing on error.
func parse(t *testing.T, src string) engine.Sheet {
	t.Helper()
	s, err := engine.Parse([]byte(src))
	require.NoError(t, err)
	return s
}

func TestCheck_Clean(t *testing.T) {
	t.Parallel()

	assert.Empty(t, engine.Check(parse(t, "1\t2\t=A1 + B1\n")))
}

func TestCheck_UnknownFunction(t *testing.T) {
	t.Parallel()

	diags := engine.Check(parse(t, "1\t=bogus(A1)\n"))
	require.Len(t, diags, 1)
	assert.Equal(t, "B1", diags[0].Cell)
	assert.Contains(t, diags[0].Message, "bogus")
	assert.False(t, diags[0].IsFatal)
}

func TestCheck_NumberFormulaHasNoRefs(t *testing.T) {
	t.Parallel()

	// A formula with no calls yields no diagnostics (the walker no-ops).
	assert.Empty(t, engine.Check(parse(t, "=1 + 2\n")))
}

func TestCheck_KnownFunctionsClean(t *testing.T) {
	t.Parallel()

	// A conditional (`if`), an inspector (`isnumber`), a table function
	// (`index`), a criteria function (`countif`), an array function (`unique`),
	// and an eager function (`sum`) are all known — no diagnostics.
	assert.Empty(
		t,
		engine.Check(parse(t, "1\t2\t=if(isnumber(A1), countif(unique(A1:B1), 1), index(A1:B1, 1, 1))\n")),
	)
}

func TestCheck_TextFunctionsKnown(t *testing.T) {
	t.Parallel()

	// The lazily-dispatched text builtins (REPT, bounded by the byte budget at
	// compute time) are known — Check must not flag what the evaluator computes.
	assert.Empty(t, engine.Check(parse(t, "3\t=rept(\"█\", A1)\n")))
}

func TestCheck_ImportFunctionsKnown(t *testing.T) {
	t.Parallel()

	// The lazily-dispatched IMPORT* functions are known builtins — Check must not
	// report them as unknown functions (they resolve to #IMPORT! at compute time
	// only when no fetcher is injected, which is a value, not a static error).
	for _, fn := range []string{"importcell", "importrow", "importcolumn", "importrange", "importsheet"} {
		t.Run(fn, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, engine.Check(parse(t, "="+fn+`("https://x/v")`+"\n")))
		})
	}
}

func TestCheck_WalksIntoUnaryPercentBinaryAndCall(t *testing.T) {
	t.Parallel()

	// Each wrapper form must be walked to reach the unknown call inside it.
	for _, src := range []string{"=-bogus(A1)", "=bogus(A1)%", "=bogus(A1) + 1", "=abs(bogus(A1))"} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			diags := engine.Check(parse(t, "1\t"+src+"\n"))
			require.Len(t, diags, 1)
			assert.Contains(t, diags[0].Message, "bogus")
		})
	}
}

func TestExplain_Formula(t *testing.T) {
	t.Parallel()

	// C1 = A1 + B1 over 2 and 3.
	trace, err := engine.Explain(parse(t, "2\t3\t=A1 + B1\n"), engine.Address{Row: 0, Col: 2})
	require.NoError(t, err)
	assert.Equal(t, "C1", trace.Cell)
	assert.Equal(t, "5", trace.Value)
	assert.Equal(t, "A1 + B1", trace.Formula)
	assert.Equal(t, []engine.TraceInput{{Ref: "A1", Value: "2"}, {Ref: "B1", Value: "3"}}, trace.Inputs)
}

func TestExplain_RangeInput(t *testing.T) {
	t.Parallel()

	// A range operand renders as a two-cell A1 range whose value lists the
	// range's cells — not the #VALUE! that scalar reduction would yield.
	trace, err := engine.Explain(parse(t, "1\t2\t=sum(A1:B1)\n"), engine.Address{Row: 0, Col: 2})
	require.NoError(t, err)
	require.Len(t, trace.Inputs, 1)
	assert.Equal(t, "A1:B1", trace.Inputs[0].Ref)
	assert.Equal(t, "1, 2", trace.Inputs[0].Value)
}

func TestExplain_Literal(t *testing.T) {
	t.Parallel()

	trace, err := engine.Explain(parse(t, "hello\t=A1\n"), engine.Address{Row: 0, Col: 0})
	require.NoError(t, err)
	assert.Equal(t, "hello", trace.Value)
	assert.Empty(t, trace.Formula)
	assert.Empty(t, trace.Inputs)
}

func TestExplain_OutOfGrid(t *testing.T) {
	t.Parallel()

	_, err := engine.Explain(parse(t, "1\t2\n"), engine.Address{Row: 9, Col: 9})
	require.Error(t, err)
	assert.ErrorIs(t, err, constants.ErrNotFound)
}

func TestExplain_RendersEveryExpressionForm(t *testing.T) {
	t.Parallel()

	// Each formula exercises one renderExpr branch; the rendered form round-trips.
	cases := map[string]string{
		"=42":              "42",
		`="hi"`:            `"hi"`,
		"=TRUE":            "TRUE",
		"=FALSE":           "FALSE",
		"=#N/A":            "#N/A",
		"=-A1":             "-A1",
		"=A1%":             "A1%",
		"=A1 + 1":          "A1 + 1",
		"=abs(A1)":         "abs(A1)",
		"=pi()":            "pi",              // a nullary call canonicalizes to bare
		"=pi":              "pi",              // and the bare form round-trips
		"=now()":           "now",             // holds for any zero-argument call
		`="other.tsvt"!A1`: `"other.tsvt"!A1`, // cross-sheet single cell
		`="d.tsvt"!A1:B2`:  `"d.tsvt"!A1:B2`,  // cross-sheet range
		// The pipe spelling is preserved (§5.4): a piped call renders as the
		// author's pipe, a chain stays a chain, and an operator capturing a
		// piped call parenthesizes it. A stage with no explicit arguments
		// renders bare — the canonical form drops its empty parentheses.
		"=A1 | len()":            "A1 | len",
		"=A1 | len":              "A1 | len", // bare stage round-trips unchanged
		"=A1 | round(2)":         "A1 | round(2)",
		"=A1 | trim() | len()":   "A1 | trim | len",
		"=(A1 | len()) + 1":      "(A1 | len) + 1",
		"=sum(A1 | round(2), 1)": "sum(A1 | round(2),1)", // piped call as an argument needs no parens
	}
	for src, want := range cases {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			trace, err := engine.Explain(parse(t, "5\t"+src+"\n"), engine.Address{Row: 0, Col: 1})
			require.NoError(t, err)
			assert.Equal(t, want, trace.Formula)
		})
	}
}
