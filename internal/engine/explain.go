package engine

import (
	"strings"
	"time"

	"github.com/uplang/go-tsvsheet/internal/constants"
	"github.com/uplang/go-tsvsheet/internal/tsvt"
)

// Trace explains how one cell was produced: its value, the formula (empty for a
// literal), and the resolved value of each cell the formula reads.
type Trace struct {
	Cell    string       `json:"cell"`
	Value   string       `json:"value"`
	Formula string       `json:"formula,omitempty"`
	Inputs  []TraceInput `json:"inputs,omitempty"`
}

// TraceInput is one reference a formula reads, with its resolved value.
type TraceInput struct {
	Ref   string `json:"ref"`
	Value string `json:"value"`
}

// Explain computes the sheet and describes the cell at at: its value, and — when
// the cell is a formula — that formula and each reference it reads.
func Explain(s Sheet, at Address) (Trace, error) {
	cl, inGrid := s.at(rowIndex(at.Row), colIndex(at.Col))
	if !inGrid {
		return Trace{}, constants.ErrNotFound.With(nil, "cell", at.String())
	}
	comp := newComputer(s, time.Now())
	trace := Trace{Cell: at.String(), Value: comp.read(rowIndex(at.Row), colIndex(at.Col)).String()}
	if cl.isFormula() {
		trace.Formula = renderExpr(cl.formula)
		trace.Inputs = traceInputs(comp, cl.formula)
	}
	return trace, nil
}

// traceInputs renders each reference in the formula with its computed value.
func traceInputs(comp computer, expr tsvt.Expr) []TraceInput {
	res := resolver{comp: comp}
	var inputs []TraceInput
	walkRefs(expr, func(ref tsvt.Reference) {
		inputs = append(inputs, TraceInput{Ref: renderReference(ref), Value: traceValue(res.resolveOperand(ref))})
	})
	return inputs
}

// traceValue renders a resolved reference for a trace: a single cell as its
// value, a multi-cell range as its cell values joined with ", " — so a range
// input reads informatively rather than the #VALUE! that scalar() yields for a
// range in a scalar context.
func traceValue(cs cellset) string {
	if cs.isSingle {
		return cs.scalar().String()
	}
	parts := make([]string, len(cs.values))
	for i, v := range cs.values {
		parts[i] = v.String()
	}
	return strings.Join(parts, ", ")
}

// walkRefs visits every reference operand in an expression tree.
func walkRefs(expr tsvt.Expr, visit func(tsvt.Reference)) {
	if operand, ok := expr.(tsvt.RefOperand); ok {
		visit(operand.Ref)
	}
	for _, child := range children(expr) {
		walkRefs(child, visit)
	}
}
