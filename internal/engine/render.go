package engine

import (
	"strconv"
	"strings"

	"github.com/uplang/go-tsvsheet/internal/tsvt"
)

// renderExpr reconstructs a readable source form of an expression, used by
// diagnostics and the explain trace.
func renderExpr(expr tsvt.Expr) string {
	switch e := expr.(type) {
	case tsvt.Number:
		return e.Text
	case tsvt.StringLit:
		return `"` + e.Value + `"`
	case tsvt.BoolLit:
		return renderBool(boolResult(e.IsTrue))
	case tsvt.ErrorLit:
		return e.Code
	case tsvt.RefOperand:
		return renderReference(e.Ref)
	case tsvt.Unary:
		return string(e.Op) + operandParens(e.X, precUnary)
	case tsvt.Percent:
		return operandParens(e.X, precPercent) + "%"
	case tsvt.Binary:
		return renderBinary(e)
	default: // tsvt.Call
		return renderCall(expr.(tsvt.Call))
	}
}

// precedence ranks a rendered expression by how tightly it binds; a higher rank
// binds tighter, so a sub-expression needs parentheses when it binds looser than
// its context. The ranks mirror the grammar: a postfix % binds tightest, then ^,
// unary sign, * /, + -, & concatenation, and comparisons loosest; atoms
// (literals, refs, calls) never need parentheses.
type precedence int

const (
	precCompare precedence = iota + 1
	precCat
	precAdd
	precMul
	precUnary
	precPow
	precPercent
	precAtom
)

// exprPrec is the binding rank of an expression's top node.
func exprPrec(expr tsvt.Expr) precedence {
	switch e := expr.(type) {
	case tsvt.Binary:
		return binaryPrec(e.Op)
	case tsvt.Unary:
		return precUnary
	case tsvt.Percent:
		return precPercent
	default:
		return precAtom
	}
}

// binaryPrec is the binding rank of a binary operator.
func binaryPrec(op tsvt.BinaryOp) precedence {
	switch op {
	case tsvt.OpPow:
		return precPow
	case tsvt.OpMul, tsvt.OpDiv:
		return precMul
	case tsvt.OpAdd, tsvt.OpSub:
		return precAdd
	case tsvt.OpCat:
		return precCat
	default: // comparisons
		return precCompare
	}
}

// renderBinary renders a binary expression, parenthesizing each operand only
// when the parser would otherwise regroup it.
func renderBinary(e tsvt.Binary) string {
	left := binaryOperand(e.Left, e.Op, false)
	right := binaryOperand(e.Right, e.Op, true)
	return left + " " + string(e.Op) + " " + right
}

// binaryOperand renders one operand of a binary op, wrapping it in parentheses
// when its own precedence (and the parent's associativity) would let the parser
// regroup the expression. `^` is right-associative, so its left operand needs
// parentheses at equal precedence while every other operator's right one does.
func binaryOperand(operand tsvt.Expr, parentOp tsvt.BinaryOp, isRight boolResult) string {
	s := renderExpr(operand)
	childPrec, parentPrec := exprPrec(operand), binaryPrec(parentOp)
	tooLoose := childPrec < parentPrec ||
		(childPrec == parentPrec && bool(isRight) != (parentOp == tsvt.OpPow))
	if tooLoose {
		return "(" + s + ")"
	}
	return s
}

// operandParens renders the operand of a unary or postfix operator, wrapping it
// when it binds looser than that operator.
func operandParens(operand tsvt.Expr, parentPrec precedence) string {
	s := renderExpr(operand)
	if exprPrec(operand) < parentPrec {
		return "(" + s + ")"
	}
	return s
}

// renderBool reconstructs a boolean literal.
func renderBool(isTrue boolResult) string {
	if isTrue {
		return "TRUE"
	}
	return "FALSE"
}

// renderCall reconstructs a function call.
func renderCall(call tsvt.Call) string {
	args := make([]string, len(call.Args))
	for i, arg := range call.Args {
		args[i] = renderExpr(arg)
	}
	return call.Name + "(" + strings.Join(args, ",") + ")"
}

// renderReference reconstructs an A1 reference: a cell or a two-cell range,
// with its `"file"!` sheet qualifier when present.
func renderReference(ref tsvt.Reference) string {
	rangeRef := ref.(tsvt.RangeRef)
	body := renderCell(rangeRef.From)
	if rangeRef.To != nil {
		body += ":" + renderCell(*rangeRef.To)
	}
	return renderQualifier(Path(rangeRef.File)) + body
}

// renderQualifier reconstructs a `"file"!` sheet qualifier, or "" for the
// current sheet.
func renderQualifier(file Path) string {
	if file == "" {
		return ""
	}
	return `"` + string(file) + `"!`
}

// renderCell reconstructs one A1 cell (`B2`).
func renderCell(cell tsvt.CellRef) string {
	return cell.Col + strconv.Itoa(cell.Row)
}
