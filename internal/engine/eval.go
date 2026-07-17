package engine

import "github.com/uplang/go-tsvsheet/internal/tsvt"

// eval evaluates a §11 expression to a Value; error values propagate strictly
// (ADR 0003 rule 3), left operand first.
func (r resolver) eval(expr tsvt.Expr) Value {
	switch e := expr.(type) {
	case tsvt.Number:
		return value(textVal(e.Text))
	case tsvt.StringLit:
		return stringValue(textVal(e.Value))
	case tsvt.BoolLit:
		return boolValue(boolResult(e.IsTrue))
	case tsvt.ErrorLit:
		return errorValue(ErrorValue(e.Code))
	case tsvt.RefOperand:
		return r.resolveOperand(e.Ref).scalar()
	case tsvt.Unary:
		return r.evalUnary(e)
	case tsvt.Percent:
		return r.evalPercent(e)
	case tsvt.Binary:
		return r.evalBinary(e)
	default: // tsvt.Call
		return r.evalCall(expr.(tsvt.Call))
	}
}

// evalPercent applies a postfix percent: `50%` is 0.5. A non-numeric operand is
// #VALUE!; an error propagates.
func (r resolver) evalPercent(e tsvt.Percent) Value {
	n, v := r.eval(e.X).asNumber()
	if v.isError() {
		return v
	}
	return numberValue(floatVal(n / 100))
}

// evalUnary applies a unary sign; a non-numeric operand is #VALUE!, an error
// propagates.
func (r resolver) evalUnary(e tsvt.Unary) Value {
	n, v := r.eval(e.X).asNumber()
	if v.isError() {
		return v
	}
	if e.Op == tsvt.OpNeg {
		return numberValue(floatVal(-n))
	}
	return numberValue(floatVal(n))
}

// evalBinary evaluates a binary operation, propagating an error operand before
// dispatching comparison, text concatenation, or arithmetic.
func (r resolver) evalBinary(e tsvt.Binary) Value {
	left := r.eval(e.Left)
	if left.isError() {
		return left
	}
	right := r.eval(e.Right)
	if right.isError() {
		return right
	}
	switch {
	case isComparison(e.Op):
		return compare(e.Op, left, right)
	case e.Op == tsvt.OpCat:
		return stringValue(textVal(left.String() + right.String()))
	default:
		return arithmetic(e.Op, left, right)
	}
}

// isComparison reports whether op is a §11 comparison operator (level 5).
func isComparison(op tsvt.BinaryOp) bool {
	switch op {
	case tsvt.OpEq, tsvt.OpNe, tsvt.OpLt, tsvt.OpLe, tsvt.OpGt, tsvt.OpGe:
		return true
	default:
		return false
	}
}

// arithmetic applies a multiplicative/additive operator over numeric operands
// (ADR 0003 rules 8, 14).
func arithmetic(op tsvt.BinaryOp, left, right Value) Value {
	l, lv := left.asNumber()
	if lv.isError() {
		return lv
	}
	r, rv := right.asNumber()
	if rv.isError() {
		return rv
	}
	return apply(op, floatVal(l), floatVal(r))
}

// apply computes a numeric binary result, guarding division by zero and mapping
// a non-finite result (overflow to ±Inf, or a domain error such as a negative
// base to a fractional power) to #NUM! rather than leaking a Go "NaN"/"+Inf"
// token into the cell — matching Excel and the POWER/SQRT builtins.
func apply(op tsvt.BinaryOp, l, r floatVal) Value {
	switch op {
	case tsvt.OpMul:
		return mathResult(l * r)
	case tsvt.OpAdd:
		return mathResult(l + r)
	case tsvt.OpSub:
		return mathResult(l - r)
	case tsvt.OpPow:
		return mathResult(power(l, r))
	default: // OpDiv
		return divide(l, r)
	}
}

// divide applies division, yielding #DIV/0! on a zero divisor and #NUM! on a
// non-finite (overflow) quotient.
func divide(l, r floatVal) Value {
	if r == 0 {
		return errorValue(ErrDiv)
	}
	return mathResult(l / r)
}
