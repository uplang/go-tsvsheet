package engine

import (
	"math"
	"strings"

	isnow "github.com/tsvsheet/go-isnow"

	"github.com/tsvsheet/go-tsvsheet/internal/tsvt"
)

// mod is Excel's MOD remainder: it takes the sign of the divisor via floored
// division (MOD(n,d) = n - d*FLOOR(n/d)), unlike Go's math.Mod, which takes the
// sign of the dividend. So MOD(-3,2) is 1 (not -1) and MOD(3,-2) is -1 (not 1).
func mod(l, r floatVal) floatVal {
	return floatVal(float64(l) - float64(r)*math.Floor(float64(l)/float64(r)))
}

// power raises l to the r-th power.
func power(l, r floatVal) floatVal { return floatVal(math.Pow(float64(l), float64(r))) }

// compare applies a comparison, yielding a boolean TRUE/FALSE (ADR 0004 §1):
// numeric when both operands are numeric (a bool compares as its 1/0), and
// lexicographic when both are strings; a mixed pair is #VALUE!.
func compare(op tsvt.BinaryOp, left, right Value) Value {
	if numericish(left) && numericish(right) {
		return boolValue(boolResult(numberOrder(op, floatVal(left.num), floatVal(right.num))))
	}
	if bothText(left, right) {
		return boolValue(boolResult(stringOrder(op, textVal(text(left)), textVal(text(right)))))
	}
	return errorValue(ErrValue)
}

// numericish reports whether a value participates in numeric comparison — a
// number or a boolean (whose 1/0 lives in the number field).
func numericish(v Value) bool {
	return v.kind == kindNumber || v.kind == kindBool || v.kind == kindDate
}

// bothText reports whether both operands compare as text (string or empty).
func bothText(left, right Value) bool {
	return textual(left) && textual(right)
}

// textual reports whether a value participates in string comparison.
func textual(v Value) bool { return v.kind == kindString || v.kind == kindEmpty }

// text is a value's comparable string form (empty for the empty value).
func text(v Value) string {
	if v.kind == kindString {
		return v.str
	}
	return ""
}

// numberOrder evaluates a comparison over two numbers.
func numberOrder(op tsvt.BinaryOp, l, r floatVal) bool {
	switch op {
	case tsvt.OpEq:
		return l == r
	case tsvt.OpNe:
		return l != r
	case tsvt.OpLt:
		return l < r
	case tsvt.OpLe:
		return l <= r
	case tsvt.OpGt:
		return l > r
	default: // OpGe
		return l >= r
	}
}

// stringOrder evaluates a comparison over two strings lexicographically.
func stringOrder(op tsvt.BinaryOp, l, r textVal) bool {
	return numberOrder(op, floatVal(strings.Compare(string(l), string(r))), 0)
}

// evalCall dispatches a function call by case-insensitive name (ADR 0004 §2);
// an unknown name is #NAME? and a call outside the function's arity bounds is
// #VALUE!.
func (r resolver) evalCall(call tsvt.Call) Value {
	name := funcName(strings.ToLower(call.Name))
	if v, ok := r.evalLazy(name, call.Args); ok {
		return v
	}
	fn, known := functions[string(name)]
	if !known {
		return errorValue(ErrName)
	}
	if !fn.accepts(argCount(len(call.Args))) {
		return errorValue(ErrValue)
	}
	values := r.argValues(call.Args, fn.spec)
	if bad, found := firstError(values); found {
		return bad
	}
	return fn.impl(values)
}

// evalLazy dispatches the builtins that evaluate their own arguments — the
// selective conditionals and the single-argument inspectors, which must observe
// errors and empties rather than have them short-circuited by the eager path.
// ok is false for any other (eager) name.
func (r resolver) evalLazy(name funcName, args []tsvt.Expr) (Value, boolResult) {
	for _, dispatch := range r.lazyDispatchers() {
		if v, ok := dispatch(name, args); ok {
			return v, true
		}
	}
	return Value{}, false
}

// lazyDispatch resolves a lazy builtin by name: ok is false when the dispatcher
// does not own the name.
type lazyDispatch func(name funcName, args []tsvt.Expr) (Value, boolResult)

// lazyDispatchers is the ordered set of lazy builtin dispatchers evalLazy tries;
// the first that owns the name produces the value.
func (r resolver) lazyDispatchers() []lazyDispatch {
	return []lazyDispatch{
		r.evalConditional,
		r.evalClock,
		r.evalTable,
		r.evalCriteria,
		r.evalArray,
		r.evalText,
		r.evalEmbed,
		r.evalImport,
		r.evalInspector,
	}
}

// isText reports whether name is a lazily-dispatched text builtin — the set
// evalText owns. Check consults it so the checker and the evaluator agree.
func isText(name funcName) boolResult {
	return name == "rept"
}

// evalText dispatches the text builtins that must read an injected resource
// limit — currently only REPT, whose result is bounded by the byte budget. ok is
// false for any other name.
func (r resolver) evalText(name funcName, args []tsvt.Expr) (Value, boolResult) {
	if !isText(name) {
		return Value{}, false
	}
	return r.evalRept(args), true
}

// evalRept evaluates REPT(text, count) lazily so it can bound its result by the
// injected byte limit. Its observable behavior matches the former eager path: a
// wrong arity or a propagated argument error short-circuits, then repeatText
// applies the count and byte-budget checks.
func (r resolver) evalRept(args []tsvt.Expr) Value {
	if len(args) != 2 {
		return errorValue(ErrValue)
	}
	values := r.argValues(args, paramModes{})
	if bad, found := firstError(values); found {
		return bad
	}
	return repeatText(values, byteBudget(r.comp.limits.ResultBytes))
}

// evalClock dispatches the volatile clock builtins TODAY and NOW, which read the
// pass clock; ok is false for any other name. A non-empty argument list is
// #VALUE!.
// Clock-dependent (volatile) function names: their value changes with time.
const (
	fnToday = "today"
	fnNow   = "now"
	fnIsnow = "isnow"
)

func (r resolver) evalClock(name funcName, args []tsvt.Expr) (Value, boolResult) {
	switch name {
	case fnToday:
		return clockResult(argCount(len(args)), dateValue(daySerial(r.comp.now))), true
	case fnNow:
		return clockResult(argCount(len(args)), dateValue(datetimeSerial(r.comp.now))), true
	case fnIsnow:
		return r.evalIsnow(args), true
	default:
		return Value{}, false
	}
}

// evalIsnow tests whether an isnow date/time pattern (tsvsheet/isnow) holds at the
// compute clock: ISNOW("M-F noon") is TRUE when the pattern matches the current
// pass instant. A malformed pattern, or the wrong arity, is #VALUE!.
func (r resolver) evalIsnow(args []tsvt.Expr) Value {
	if len(args) != 1 {
		return errorValue(ErrValue)
	}
	pattern := r.eval(args[0])
	if pattern.isError() {
		return pattern
	}
	holds, err := isnow.Is(isnow.PatternText(pattern.String()), r.comp.now)
	if err != nil {
		return errorValue(ErrValue)
	}
	return boolValue(boolResult(holds))
}

// clockResult returns v for a no-argument call, else #VALUE!.
func clockResult(argc argCount, v Value) Value {
	if argc != 0 {
		return errorValue(ErrValue)
	}
	return v
}

// evalConditional handles the selectively-lazy conditionals, which evaluate
// only the arguments they need. ok is false for a non-conditional name.
func (r resolver) evalConditional(name funcName, args []tsvt.Expr) (Value, boolResult) {
	switch name {
	case "if":
		return r.evalIf(args), true
	case "ifs":
		return r.evalIfs(args), true
	case "iferror":
		return r.evalIferror(args, false), true
	case "ifna":
		return r.evalIferror(args, true), true
	case "switch":
		return r.evalSwitch(args), true
	default:
		return Value{}, false
	}
}

// isConditional reports whether name is one of the lazy conditional builtins.
func isConditional(name funcName) boolResult {
	switch name {
	case "if", "ifs", "iferror", "ifna", "switch":
		return true
	default:
		return false
	}
}

// evalInspector handles the single-argument inspectors (`IS*`, `N`, `TYPE`): it
// evaluates the argument (observing an error or empty result) and applies the
// pure inspector function.
func (r resolver) evalInspector(name funcName, args []tsvt.Expr) (Value, boolResult) {
	fn, ok := inspectors[string(name)]
	if !ok {
		return Value{}, false
	}
	if len(args) != 1 {
		return errorValue(ErrValue), true
	}
	return fn(r.eval(args[0])), true
}

// inspectors are the pure single-argument value functions behind the `IS*`,
// `N`, and `TYPE` builtins. They take an already-evaluated value, so this map
// holds no reference back into evalCall and stays a cycle-free var initializer.
var inspectors = map[string]func(v Value) Value{
	"isblank":   func(v Value) Value { return boolValue(v.kind == kindEmpty) },
	"iserror":   func(v Value) Value { return boolValue(boolResult(v.isError())) },
	"iserr":     func(v Value) Value { return boolValue(boolResult(v.isError()) && v.str != string(ErrNA)) },
	"isna":      func(v Value) Value { return boolValue(boolResult(v.isError()) && v.str == string(ErrNA)) },
	"isnumber":  func(v Value) Value { return boolValue(v.kind == kindNumber) },
	"istext":    func(v Value) Value { return boolValue(v.kind == kindString) },
	"isnontext": func(v Value) Value { return boolValue(v.kind != kindString) },
	"islogical": func(v Value) Value { return boolValue(v.kind == kindBool) },
	"iseven":    func(v Value) Value { return parityIs(v, false) },
	"isodd":     func(v Value) Value { return parityIs(v, true) },
	"n":         inspectN,
	"type":      func(v Value) Value { return numberValue(floatVal(typeCode(v))) },
}

// function is a registered eager builtin: its arity bounds, its parameter
// modes, and its impl over pre-evaluated, error-free argument values (ADR 0004
// §2). Lazy builtins that evaluate their own arguments (currently only `if`)
// are dispatched separately so the registry stays a cycle-free var
// initializer.
type function struct {
	impl    func(args []Value) Value
	spec    paramModes
	minArgs argCount
	maxArgs argCount // negative means variadic (unbounded)
}

// accepts reports whether n arguments fall within the function's arity bounds.
func (f function) accepts(n argCount) bool {
	return n >= f.minArgs && (f.maxArgs < 0 || n <= f.maxArgs)
}

// firstError returns the first error value among values, left to right.
func firstError(values []Value) (Value, boolResult) {
	for _, v := range values {
		if v.isError() {
			return v, true
		}
	}
	return Value{}, false
}

// evalIf evaluates `if(cond, then, else)` lazily: only cond and the selected
// branch are evaluated (ADR 0004 §2). A wrong arity is #VALUE!; an error
// condition propagates.
func (r resolver) evalIf(args []tsvt.Expr) Value {
	if len(args) != 3 {
		return errorValue(ErrValue)
	}
	chosen, v := r.eval(args[0]).truthy()
	if v.isError() {
		return v
	}
	if chosen {
		return r.eval(args[1])
	}
	return r.eval(args[2])
}

// argMode selects how an eager parameter slot consumes its operand (ADR 0004
// §2): a scalar slot yields exactly one value, a cells slot flattens a range
// or array operand row-major into the argument list.
type argMode int

const (
	modeScalar argMode = iota
	modeCells
)

// paramModes declares a function's parameter slots: lead modes bind the first
// arguments, tail modes the last, and rest every slot between. The zero value
// is all-scalar — the default for positional (scalar-parameter) functions.
type paramModes struct {
	lead []argMode
	tail []argMode
	rest argMode
}

// mode is the declared mode of argument slot i in a call of n arguments.
func (p paramModes) mode(i argIndex, n argCount) argMode {
	if int(i) < len(p.lead) {
		return p.lead[i]
	}
	if tailAt := int(n) - len(p.tail); int(i) >= tailAt {
		return p.tail[int(i)-tailAt]
	}
	return p.rest
}

// The recurring parameter shapes: an aggregate flattens every slot (SUM,
// AND, …), LARGE/SMALL flatten their values but keep the trailing k scalar,
// and NPV keeps its leading rate scalar ahead of the flattened cashflows.
var (
	cellsRest       = paramModes{rest: modeCells}
	cellsThenK      = paramModes{rest: modeCells, tail: []argMode{modeScalar}}
	scalarThenCells = paramModes{lead: []argMode{modeScalar}, rest: modeCells}
)

// argValues materializes call arguments per the declared parameter modes: a
// cells slot contributes every cell of a range or array operand (§11.3), a
// scalar slot exactly one value — so a multi-cell operand can never shift the
// arguments that follow it (go-tsvsheet#2).
func (r resolver) argValues(args []tsvt.Expr, spec paramModes) []Value {
	values := make([]Value, 0, len(args))
	for i, arg := range args {
		if spec.mode(argIndex(i), argCount(len(args))) == modeCells {
			values = append(values, r.argCells(arg)...)
			continue
		}
		values = append(values, r.argScalar(arg))
	}
	return values
}

// argScalar evaluates one argument in scalar context (ADR 0004 §2): eval
// already reduces a multi-cell range to #VALUE! (cellset.scalar), and an
// array reduces to its top-left element per the pinned no-broadcasting rule.
func (r resolver) argScalar(arg tsvt.Expr) Value {
	v := r.eval(arg)
	if v.kind == kindArray {
		return v.arr[0][0]
	}
	return v
}

// argCells expands one argument: a bare reference contributes all its resolved
// cells (so `sum(A:H)` sees the whole range), and an expression that evaluates
// to an array contributes its elements row-major — consumed exactly like a
// range, so `sum(sort(A1:A3))` aggregates (ADR 0004 §2 array-valued
// arguments). Any other expression is one scalar value.
func (r resolver) argCells(arg tsvt.Expr) []Value {
	if ref, ok := arg.(tsvt.RefOperand); ok {
		return r.resolveOperand(ref.Ref).values
	}
	v := r.eval(arg)
	if v.kind == kindArray {
		return flatten1D(v.arr)
	}
	return []Value{v}
}

// functions is the case-insensitive eager builtin registry (ADR 0004 §2); `if`
// is dispatched separately (evalCall/isKnownFunc) because it is lazy, which also
// keeps this a cycle-free var initializer.
var functions = map[string]function{
	"sum":     {impl: fnSum, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"min":     {impl: fnMin, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"max":     {impl: fnMax, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"count":   {impl: fnCountNumbers, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"avg":     {impl: fnAvg, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"average": {impl: fnAvg, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"abs":     {impl: fnAbs, minArgs: 1, maxArgs: 1},
	"round":   {impl: fnRound, minArgs: 1, maxArgs: 2},
	"concat":  {impl: fnConcat, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"len":     {impl: fnLen, minArgs: 1, maxArgs: 1},
	"mod":     {impl: fnMod, minArgs: 2, maxArgs: 2},
	"output":  {impl: outputValue, minArgs: 1, maxArgs: 1},

	// Phase 1 — math & trig.
	"pi":       {impl: fnPi, minArgs: 0, maxArgs: 0},
	"sign":     {impl: unaryNumeric(sign), minArgs: 1, maxArgs: 1},
	"int":      {impl: unaryNumeric(mFloor), minArgs: 1, maxArgs: 1},
	"trunc":    {impl: unaryNumeric(mTrunc), minArgs: 1, maxArgs: 1},
	"sqrt":     {impl: unaryNumeric(mSqrt), minArgs: 1, maxArgs: 1},
	"sqrtpi":   {impl: unaryNumeric(sqrtPi), minArgs: 1, maxArgs: 1},
	"power":    {impl: binaryNumeric(mPow), minArgs: 2, maxArgs: 2},
	"exp":      {impl: unaryNumeric(mExp), minArgs: 1, maxArgs: 1},
	"ln":       {impl: unaryNumeric(mLn), minArgs: 1, maxArgs: 1},
	"log10":    {impl: unaryNumeric(mLog10), minArgs: 1, maxArgs: 1},
	"log":      {impl: fnLog, minArgs: 1, maxArgs: 2},
	"quotient": {impl: fnQuotient, minArgs: 2, maxArgs: 2},
	"product":  {impl: fnProduct, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"sumsq":    {impl: fnSumsq, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"sin":      {impl: unaryNumeric(mSin), minArgs: 1, maxArgs: 1},
	"cos":      {impl: unaryNumeric(mCos), minArgs: 1, maxArgs: 1},
	"tan":      {impl: unaryNumeric(mTan), minArgs: 1, maxArgs: 1},
	"asin":     {impl: unaryNumeric(mAsin), minArgs: 1, maxArgs: 1},
	"acos":     {impl: unaryNumeric(mAcos), minArgs: 1, maxArgs: 1},
	"atan":     {impl: unaryNumeric(mAtan), minArgs: 1, maxArgs: 1},
	"atan2":    {impl: binaryNumeric(atan2Excel), minArgs: 2, maxArgs: 2},
	"sinh":     {impl: unaryNumeric(mSinh), minArgs: 1, maxArgs: 1},
	"cosh":     {impl: unaryNumeric(mCosh), minArgs: 1, maxArgs: 1},
	"tanh":     {impl: unaryNumeric(mTanh), minArgs: 1, maxArgs: 1},
	"degrees":  {impl: unaryNumeric(toDegrees), minArgs: 1, maxArgs: 1},
	"radians":  {impl: unaryNumeric(toRadians), minArgs: 1, maxArgs: 1},

	// Phase 2 — logical (eager; conditionals and inspectors dispatch lazily).
	"and":   {impl: fnAnd, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"or":    {impl: fnOr, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"xor":   {impl: fnXor, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"not":   {impl: fnNot, minArgs: 1, maxArgs: 1},
	"true":  {impl: fnTrue, minArgs: 0, maxArgs: 0},
	"false": {impl: fnFalse, minArgs: 0, maxArgs: 0},
	"na":    {impl: fnNa, minArgs: 0, maxArgs: 0},

	// Phase 3 — text.
	"lower":        {impl: fnLower, minArgs: 1, maxArgs: 1},
	"upper":        {impl: fnUpper, minArgs: 1, maxArgs: 1},
	"proper":       {impl: fnProper, minArgs: 1, maxArgs: 1},
	"trim":         {impl: fnTrim, minArgs: 1, maxArgs: 1},
	"clean":        {impl: fnClean, minArgs: 1, maxArgs: 1},
	"left":         {impl: fnLeft, minArgs: 1, maxArgs: 2},
	"right":        {impl: fnRight, minArgs: 1, maxArgs: 2},
	"mid":          {impl: fnMid, minArgs: 3, maxArgs: 3},
	"exact":        {impl: fnExact, minArgs: 2, maxArgs: 2},
	"t":            {impl: fnT, minArgs: 1, maxArgs: 1},
	"concatenate":  {impl: fnConcatenate, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"find":         {impl: fnFind, minArgs: 2, maxArgs: 3},
	"search":       {impl: fnSearch, minArgs: 2, maxArgs: 3},
	"substitute":   {impl: fnSubstitute, minArgs: 3, maxArgs: 4},
	"replace":      {impl: fnReplace, minArgs: 4, maxArgs: 4},
	"char":         {impl: fnChar, minArgs: 1, maxArgs: 1},
	"unichar":      {impl: fnChar, minArgs: 1, maxArgs: 1},
	"code":         {impl: fnCode, minArgs: 1, maxArgs: 1},
	"unicode":      {impl: fnCode, minArgs: 1, maxArgs: 1},
	"value":        {impl: fnValue, minArgs: 1, maxArgs: 1},
	"regexmatch":   {impl: fnRegexMatch, minArgs: 2, maxArgs: 2},
	"regexextract": {impl: fnRegexExtract, minArgs: 2, maxArgs: 2},
	"regexreplace": {impl: fnRegexReplace, minArgs: 3, maxArgs: 3},

	// Phase 4 — date & time (TODAY/NOW dispatch via the clock path).
	"year":      {impl: fnYear, minArgs: 1, maxArgs: 1},
	"month":     {impl: fnMonth, minArgs: 1, maxArgs: 1},
	"day":       {impl: fnDay, minArgs: 1, maxArgs: 1},
	"hour":      {impl: fnHour, minArgs: 1, maxArgs: 1},
	"minute":    {impl: fnMinute, minArgs: 1, maxArgs: 1},
	"second":    {impl: fnSecond, minArgs: 1, maxArgs: 1},
	"weekday":   {impl: fnWeekday, minArgs: 1, maxArgs: 2},
	"date":      {impl: fnDate, minArgs: 3, maxArgs: 3},
	"edate":     {impl: fnEdate, minArgs: 2, maxArgs: 2},
	"eomonth":   {impl: fnEomonth, minArgs: 2, maxArgs: 2},
	"days":      {impl: fnDays, minArgs: 2, maxArgs: 2},
	"datevalue": {impl: fnDatevalue, minArgs: 1, maxArgs: 1},

	// Phase 5 — lookup (VLOOKUP/HLOOKUP/INDEX/MATCH/ROWS/COLUMNS dispatch via
	// the table path, which keeps a range's 2-D shape).
	"choose": {impl: fnChoose, minArgs: 2, maxArgs: -1},

	// Phase 6 — statistical (COUNTIF/SUMIF/AVERAGEIF dispatch via the criteria
	// path).
	"median":     {impl: fnMedian, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"mode":       {impl: fnMode, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"stdev":      {impl: fnStdev, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"stdevp":     {impl: fnStdevp, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"var":        {impl: fnVar, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"varp":       {impl: fnVarp, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"geomean":    {impl: fnGeomean, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"large":      {impl: fnLarge, spec: cellsThenK, minArgs: 2, maxArgs: -1},
	"small":      {impl: fnSmall, spec: cellsThenK, minArgs: 2, maxArgs: -1},
	"counta":     {impl: fnCount, spec: cellsRest, minArgs: 1, maxArgs: -1},
	"countblank": {impl: fnCountblank, spec: cellsRest, minArgs: 1, maxArgs: -1},

	// Phase 8 — financial (basic).
	"pmt": {impl: fnPmt, minArgs: 3, maxArgs: 5},
	"fv":  {impl: fnFv, minArgs: 3, maxArgs: 5},
	"pv":  {impl: fnPv, minArgs: 3, maxArgs: 5},
	"npv": {impl: fnNpv, spec: scalarThenCells, minArgs: 2, maxArgs: -1},
	"sln": {impl: fnSln, minArgs: 3, maxArgs: 3},
}
