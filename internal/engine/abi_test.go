package engine_test

// The eager call ABI (ADR 0004 §2, go-tsvsheet#2): the registry descriptor
// declares each parameter slot scalar or cells, so a multi-cell operand in a
// scalar slot no longer shifts the arguments that follow it. A scalar slot
// behaves exactly like every other scalar context: a multi-cell range is
// #VALUE! (cellset.scalar) and an array reduces to its top-left element (the
// pinned no-broadcasting rule).

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tsvsheet/go-tsvsheet/internal/engine"
)

func TestCompute_ScalarSlotRejectsMultiCellRange(t *testing.T) {
	t.Parallel()

	// A1=1.777 A2=5 A3=2; each formula puts a multi-cell range in a scalar
	// parameter slot, which is #VALUE! — never a silent read of the range's
	// first cell with the later arguments shifted (go-tsvsheet#2).
	src := "1.777\n5\n2\n=round(A1:A3, 2)\n=len(A1:A3)\n=rept(A1:A2, 2)\n=abs(A1:A2)\n"
	g := compute(t, src)
	for row := 3; row <= 6; row++ {
		assert.Equal(t, string(engine.ErrValue), cellAt(t, g, row, 0))
	}
}

func TestCompute_ScalarSlotReducesArrayTopLeft(t *testing.T) {
	t.Parallel()

	// sort(A1:A2) is {1.777, 5}; the scalar first slot of ROUND reduces the
	// array to its top-left element, and the second argument keeps its place:
	// round(1.777, 2) = 1.78 — not round(1.777, 5) via positional shift.
	g := compute(t, "5\n1.777\n=round(sort(A1:A2), 2)\n")
	assert.Equal(t, "1.78", cellAt(t, g, 2, 0))
}

func TestCompute_ScalarSlotAcceptsSingleCellRange(t *testing.T) {
	t.Parallel()

	// A single-cell range is a scalar in a scalar context (resolveMatrix keeps
	// it single), so it fills a scalar slot without error.
	g := compute(t, "2.6\n=round(A1:A1, 0)\n")
	assert.Equal(t, "3", cellAt(t, g, 1, 0))
}

func TestCompute_TrailingScalarKeepsItsSlot(t *testing.T) {
	t.Parallel()

	// LARGE/SMALL flatten their leading values (cells slots) while k stays a
	// scalar slot: a range in the k slot is #VALUE!, not a k silently read
	// from the range's cells.
	src := "10\n30\n20\n=large(A1:A3, 2)\n=small(A1:A3, 2)\n=large(A1:A3, A1:A2)\n"
	g := compute(t, src)
	assert.Equal(t, "20", cellAt(t, g, 3, 0))
	assert.Equal(t, "20", cellAt(t, g, 4, 0))
	assert.Equal(t, string(engine.ErrValue), cellAt(t, g, 5, 0))
}

func TestCompute_LeadingScalarKeepsItsSlot(t *testing.T) {
	t.Parallel()

	// NPV's rate is a scalar slot ahead of the cells-mode cashflows: a range
	// of cashflows still flattens, while a range in the rate slot is #VALUE!.
	src := "100\n200\n=round(npv(0.1, A1:A2), 2)\n=npv(A1:A2, 100)\n"
	g := compute(t, src)
	assert.Equal(t, "256.2", cellAt(t, g, 2, 0))
	assert.Equal(t, string(engine.ErrValue), cellAt(t, g, 3, 0))
}

func TestCompute_CellsSlotsStillFlatten(t *testing.T) {
	t.Parallel()

	// Aggregates keep the flat consumption: every cell of a range argument
	// participates (§11.3), exactly as before the scalar-slot fix.
	src := "1\n2\n3\n=sum(A1:A3)\n=median(A1:A3)\n=concat(A1:A3)\n=and(A1:A3)\n=count(A1:A3)\n"
	g := compute(t, src)
	assert.Equal(t, "6", cellAt(t, g, 3, 0))
	assert.Equal(t, "2", cellAt(t, g, 4, 0))
	assert.Equal(t, "123", cellAt(t, g, 5, 0))
	assert.Equal(t, "TRUE", cellAt(t, g, 6, 0))
	assert.Equal(t, "3", cellAt(t, g, 7, 0))
}
