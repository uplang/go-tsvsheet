package engine

import (
	"strconv"
	"strings"

	"github.com/tsvsheet/go-tsvsheet/internal/tsvt"
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
// unary sign, * /, + -, & concatenation, comparisons, and the pipe loosest of
// all; atoms (literals, refs, un-piped calls) never need parentheses.
type precedence int

const (
	precPipe precedence = iota + 1
	precCompare
	precCat
	precAdd
	precMul
	precUnary
	precPow
	precPercent
	precAtom
)

// exprPrec is the binding rank of an expression's top node. A call rendered in
// its pipe spelling (§5.4) ranks loosest, so it is parenthesized wherever an
// operator would otherwise capture its final call: `(A1 | len()) + 1`.
func exprPrec(expr tsvt.Expr) precedence {
	switch e := expr.(type) {
	case tsvt.Binary:
		return binaryPrec(e.Op)
	case tsvt.Unary:
		return precUnary
	case tsvt.Percent:
		return precPercent
	case tsvt.Call:
		return callPrec(e)
	default:
		return precAtom
	}
}

// callPrec is the binding rank of a call: an atom in its plain spelling, the
// loosest rank in its pipe spelling.
func callPrec(call tsvt.Call) precedence {
	if call.IsPiped {
		return precPipe
	}
	return precAtom
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

// renderCall reconstructs a function call in the author's spelling: plain
// `f(x, a)`, or the pipe form `x | f(a)` it desugared from (§5.4). A call with
// no explicit arguments renders bare, without empty parentheses — the canonical
// form (`pi`, `x | sort`).
func renderCall(call tsvt.Call) string {
	if call.IsPiped {
		return renderExpr(call.Args[0]) + " | " + callTail(funcName(call.Name), call.Args[1:])
	}
	return callTail(funcName(call.Name), call.Args)
}

// callTail renders a name with its explicit arguments, dropping the empty
// parentheses of a zero-argument call to the bare canonical form.
func callTail(name funcName, args []tsvt.Expr) string {
	if len(args) == 0 {
		return string(name)
	}
	return string(name) + "(" + joinArgs(args) + ")"
}

// joinArgs renders a comma-joined argument list.
func joinArgs(exprs []tsvt.Expr) string {
	args := make([]string, len(exprs))
	for i, arg := range exprs {
		args[i] = renderExpr(arg)
	}
	return strings.Join(args, ",")
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
