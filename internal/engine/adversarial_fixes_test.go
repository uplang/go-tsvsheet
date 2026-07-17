package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uplang/go-tsvsheet/internal/engine"
)

// These tests pin the Excel/Sheets-correct behavior for defects found by
// adversarial review of the engine. Each asserts the contract, not the prior
// (buggy) output.

// LEN counts characters (runes), not UTF-8 bytes.
func TestLen_CountsRunesNotBytes(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "4", formula1(t, `len("café")`)) // é is 2 bytes, 1 rune
	assert.Equal(t, "2", formula1(t, `len("😀!")`))   // emoji is 1 rune
	assert.Equal(t, "3", formula1(t, `len("abc")`))
}

// COUNT counts only numbers (and dates); COUNTA counts every non-empty operand.
func TestCount_NumbersOnly_CountaAll(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "2", formula1(t, `count(1, "a", 2)`))  // text not counted
	assert.Equal(t, "3", formula1(t, `counta(1, "a", 2)`)) // text counted
	assert.Equal(t, "1", formula1(t, `count("x", 5)`))     // only the number
}

// WEEKDAY honors the optional return_type; an unsupported type is #NUM!.
func TestWeekday_ReturnType(t *testing.T) {
	t.Parallel()
	// 2026-07-14 is a Tuesday.
	assert.Equal(t, "3", formula1(t, `weekday(date(2026, 7, 14))`))    // type 1 default: Sun=1 → Tue=3
	assert.Equal(t, "3", formula1(t, `weekday(date(2026, 7, 14), 1)`)) // explicit type 1
	assert.Equal(t, "2", formula1(t, `weekday(date(2026, 7, 14), 2)`)) // type 2: Mon=1 → Tue=2
	assert.Equal(t, "1", formula1(t, `weekday(date(2026, 7, 14), 3)`)) // type 3: Mon=0 → Tue=1
	assert.Equal(t, string(engine.ErrNum), formula1(t, `weekday(date(2026, 7, 14), 99)`))
	assert.Equal(t, string(engine.ErrValue), formula1(t, `weekday(date(2026, 7, 14), "x")`)) // bad type arg
	assert.Equal(t, string(engine.ErrValue), formula1(t, `weekday("not a date")`))           // bad date arg
}

// A non-finite arithmetic result (overflow, or a negative base to a fractional
// power) is #NUM!, never a leaked Go "NaN"/"+Inf" token.
func TestArithmetic_NonFiniteIsNum(t *testing.T) {
	t.Parallel()
	assert.Equal(t, string(engine.ErrNum), formula1(t, `(0 - 8) ^ (1 / 3)`)) // NaN → #NUM!
	assert.Equal(t, string(engine.ErrNum), formula1(t, `10 ^ 400`))          // +Inf → #NUM!
	assert.Equal(t, "16", formula1(t, `D1 ^ A1`))                            // finite still works: 4^2
}

// A single-cell range (A1:A1), written directly or produced by a structural
// edit collapse, reads as that cell's value in a scalar context, not #VALUE!.
func TestDegenerateRange_ReadsCell(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "11", cellAt(t, compute(t, "10\t=A1:A1 + 1\n"), 0, 1))
}

// A reversed range (written high-to-low) is shifted by a structural edit the
// same way the compute path already normalizes it — it never spuriously
// collapses to #REF! on a pure insertion.
func TestReversedRange_SurvivesStructuralEdit(t *testing.T) {
	t.Parallel()
	s := parse(t, "1\n2\n3\n=sum(A3:A1)\n") // A4 = sum of a reversed range = 6
	ins := s.InsertRow(addr(0, 0))          // blank row at top: every row shifts down one
	assert.Equal(t, "=sum(A4:A2)", sourceAt(t, ins, 4, 0))
	assert.Equal(t, "6", cellAt(t, ins.Compute(), 4, 0)) // was #REF! before the fix
}

// A rendered formula (stored by structural edits, shown by Explain) must
// re-parse to the same computation — precedence is preserved with minimal
// parentheses, so a parenthesized formula is never silently corrupted. The
// rendered form is read from Explain's trace (which is renderExpr's output),
// then parsed back and recomputed.
func TestRender_PrecedenceRoundTrips(t *testing.T) {
	t.Parallel()
	// A1=2 B1=3 C1=4; each formula in D1 must recompute identically after its
	// rendered form is parsed back.
	cases := map[string]string{
		"=(A1 + B1) * C1": "20",    // parens preserved (would be 14 without)
		"=A1 + B1 * C1":   "14",    // no parens added where unneeded
		"=A1 - (B1 - C1)": "3",     // right operand of a left-assoc op
		"=A1 - B1 - C1":   "-5",    // left-assoc chain needs no parens
		"=C1 / (A1 / B1)": "6",     // wrapped right operand of division
		"=-(A1 + B1)":     "-5",    // unary over a looser operand
		"=-A1 + B1":       "1",     // unary over an atom needs no parens
		"=(A1 + B1)%":     "0.05",  // percent over a looser operand
		"=(2 ^ 3) ^ 2":    "64",    // left operand of right-assoc ^
		"=2 ^ 3 ^ 2":      "512",   // right-assoc needs no parens
		"=A1 = B1":        "FALSE", // comparison round-trips
		"=A1 & (B1 & C1)": "234",   // right operand of concatenation
	}
	for formula, want := range cases {
		t.Run(formula, func(t *testing.T) {
			t.Parallel()
			sheet := parse(t, "2\t3\t4\t"+formula+"\n")
			trace, err := engine.Explain(sheet, engine.Address{Row: 0, Col: 3})
			require.NoError(t, err)
			reparsed := parse(t, "2\t3\t4\t="+trace.Formula+"\n")
			assert.Equal(t, want, cellAt(t, reparsed.Compute(), 0, 3),
				"formula %q rendered as %q must recompute to %s", formula, trace.Formula, want)
		})
	}
}

// Inserting at a negative index is a no-op, mirroring delete — never a
// slice-bounds panic.
func TestInsert_NegativeIndexIsNoOp(t *testing.T) {
	t.Parallel()
	s := parse(t, "1\t2\n3\t4\n")
	var rowIns, colIns engine.Sheet
	require.NotPanics(t, func() { rowIns = s.InsertRow(addr(-1, 0)) })
	require.NotPanics(t, func() { colIns = s.InsertCol(addr(0, -1)) })
	assert.Equal(t, s.Source(), rowIns.Source())
	assert.Equal(t, s.Source(), colIns.Source())
}
