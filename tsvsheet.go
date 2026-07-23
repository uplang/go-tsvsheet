package tsvsheet

import (
	"io"

	"github.com/tsvsheet/go-tsvsheet/internal/engine"
)

// Address is a cell coordinate in spreadsheet notation (`F4`): column letters
// plus a 1-based row. It carries 0-based indices internally.
type Address = engine.Address

// AddressText is spreadsheet-address source text (`A1`, `F4`) accepted by
// ParseAddress. It is exported so callers in other packages can convert their
// string input at the call site.
type AddressText = engine.AddressText

// CellInfo describes one non-empty cell: its address, source text, and whether
// it is a formula — the projection the parse command emits.
type CellInfo = engine.CellInfo

// ComputeOptions configures a compute pass. Loader and Base enable embedded
// sub-sheets; a zero Loader disables SHEET (it resolves to #REF!). Tick is the
// recompute-pass ordinal a refreshing frontend increments so TICK()/FRAME()
// advance across passes.
type ComputeOptions = engine.ComputeOptions

// Tick is a recompute-pass ordinal read by TICK()/FRAME(); a frontend that
// re-renders a volatile sheet passes an incrementing value each pass.
type Tick = engine.Tick

// Diagnostic is an advisory finding about a formula cell: currently an unknown
// function call (which computes to #NAME?).
type Diagnostic = engine.Diagnostic

// ErrorValue is a spreadsheet error value — a cell value, not a Go error. It
// propagates through expressions per ADR 0003 (rules 3, 8, 12, 14).
type ErrorValue = engine.ErrorValue

// Expr is one compiled bare expression — the text that would follow `=` in a
// formula cell — detached from any sheet: compile once with CompileExpr, then
// evaluate against any number of grids, including concurrently.
type Expr = engine.Expr

// FetchResult is a Fetcher's response: the raw body and the media type the
// server declared, which must match the requested Accept for the handshake to
// succeed (ADR 0006 §2).
type FetchResult = engine.FetchResult

// Fetcher retrieves the content-typed import at url, sending accept as the
// requested media type. The engine holds only this interface; the concrete
// net/http fetcher, allowlist, and caching are injected by a frontend. A nil
// Fetcher disables imports (every IMPORT* is #IMPORT!).
type Fetcher = engine.Fetcher

// Grid is a rectangular value grid indexed [row][col], 0-based. Cells are raw
// strings: a literal's own text on input, or a formula cell's computed value
// after ComputeAt.
type Grid = engine.Grid

// ImportURL is the location an IMPORT* function fetches — the (already
// evaluated) string value of its single argument.
type ImportURL = engine.ImportURL

// Limits bounds the sizes an untrusted sheet may drive an allocation to.
type Limits = engine.Limits

// Loader resolves the sheet referenced by ref, relative to the embedding
// sheet's own path base, returning the parsed sub-sheet and its resolved path
// (used for cycle detection and as the base for the sub-sheet's own SHEET
// calls). The frontend injects it, keeping the engine filesystem-free; a
// resolution or containment failure is reported as an error and surfaces as
// #REF!.
type Loader = engine.Loader

// MediaType is a content-typed import's RFC 6838 media type — the Accept header
// an IMPORT* function requests, which the response Content-Type must match.
type MediaType = engine.MediaType

// Path identifies a sheet to a Loader: the reference written in a
// SHEET(...) call, and (as the loader's result) the sheet's own resolved path.
type Path = engine.Path

// Sheet is a parsed spreadsheet grid of literal and formula cells.
type Sheet = engine.Sheet

// Span is a rectangular reference target resolved to 0-based addresses: a single
// cell (From == To) or a range (From is the top-left, To the bottom-right as
// written). It is the projection a frontend highlights.
type Span = engine.Span

// Trace explains how one cell was produced: its value, the formula (empty for a
// literal), and the resolved value of each cell the formula reads.
type Trace = engine.Trace

// TraceInput is one reference a formula reads, with its resolved value.
type TraceInput = engine.TraceInput

// Value is an evaluated cell value: empty, number, string, boolean, date, error,
// or a 2-D array (a dynamic-array result that spills, or reduces to its top-left
// value in a scalar context).
type Value = engine.Value

// The error values. #REF! (out-of-grid), #VALUE! (type), #NAME? (unknown
// function), #DIV/0! (division by zero), #CIRC! (a formula whose evaluation
// depends on itself), #N/A (lookup miss / NA()), #NUM! (numeric domain),
// #NULL! (empty range intersection), #SPILL! (blocked dynamic-array spill),
// #IMPORT! (a content-typed import failed — disabled, denied, or a bad
// handshake).
const (
	ErrRef    ErrorValue = engine.ErrRef
	ErrValue  ErrorValue = engine.ErrValue
	ErrName   ErrorValue = engine.ErrName
	ErrDiv    ErrorValue = engine.ErrDiv
	ErrCirc   ErrorValue = engine.ErrCirc
	ErrNA     ErrorValue = engine.ErrNA
	ErrNum    ErrorValue = engine.ErrNum
	ErrNull   ErrorValue = engine.ErrNull
	ErrSpill  ErrorValue = engine.ErrSpill
	ErrImport ErrorValue = engine.ErrImport
)

// Parse reads a .tsvt grid: each TAB-separated field is a literal, or — when it
// begins with `=` — a formula compiled from the expression that follows. A
// malformed formula is a syntax error naming its cell.
func Parse(src []byte) (Sheet, error) { return engine.Parse(src) }

// Document is a parsed .tsvt file with its physical line layout retained, so
// comment and shebang lines — which the grid drops — survive editing and are
// written back in position by Text. Document is immutable: every editing
// operation returns a new Document. It is the one sanctioned way to serialize
// a .tsvt; frontends must never rebuild a file from a grid.
type Document = engine.Document

// ParseDocument reads a .tsvt file like Parse, additionally recording the
// physical line layout so comment and shebang lines are preserved by Text.
func ParseDocument(src []byte) (Document, error) { return engine.ParseDocument(src) }

// ParseAddress parses spreadsheet notation (`A1`, `F4`, `AA10`) into an
// Address. The column is one or more ASCII uppercase letters, the row a
// positive integer; anything else is constants.ErrInvalidValue.
func ParseAddress(s AddressText) (Address, error) { return engine.ParseAddress(s) }

// Check reports the static diagnostics of a parsed sheet: each unknown function
// call. Syntax errors are already rejected by Parse, and every reference the
// narrowed grammar admits is a valid A1 form, so Check never reports those.
func Check(s Sheet) []Diagnostic { return engine.Check(s) }

// ReadTSV reads a tab-separated value grid. Rows are newline-separated; a
// trailing newline does not add an empty row. Full-line comments are skipped
// and do not occupy a grid row: a leading `#!` on the first line (a shebang, so
// a .tsvt can be `chmod +x` and run via `#!/usr/bin/env tsvsheet`) and any line
// beginning with `# ` (hash-space). An error-value cell like `#N/A` (hash then a
// non-space) is data, not a comment. A read failure surfaces as ErrReadInput.
func ReadTSV(r io.Reader) (Grid, error) { return engine.ReadTSV(r) }

// WriteTSV writes the grid as tab-separated rows, each terminated by a newline.
// A write failure surfaces as constants.ErrWriteFile. Callers wanting buffering
// pass a bufio.Writer; WriteTSV writes each row directly so a write error is
// reported at its source.
func WriteTSV(w io.Writer, g Grid) error { return engine.WriteTSV(w, g) }

// DefaultLimits are generous for real spreadsheets while still bounding OOM.
func DefaultLimits() Limits { return engine.DefaultLimits() }

// BrowserLimits are the tighter ceilings the WASM build applies, sized for a
// browser tab rather than a workstation.
func BrowserLimits() Limits { return engine.BrowserLimits() }

// Explain computes the sheet and describes the cell at at: its value, and — when
// the cell is a formula — that formula and each reference it reads.
func Explain(s Sheet, at Address) (Trace, error) { return engine.Explain(s, at) }

// CompileExpr parses and compiles one bare expression — the text that would
// follow `=` in a formula cell. A malformed expression is ErrSyntax carrying
// line/column detail via With. The compiled Expr is an immutable value, safe
// for concurrent reuse; its Eval(g, opts) evaluates against a Grid with the
// exact semantics of a formula cell in a sheet over that grid — reference
// resolution, literal coercion, ranges, dynamic arrays, error-value
// propagation, volatile functions from opts.At, Limits enforcement, and
// Loader/Fetcher gating — returning error values, never Go errors.
func CompileExpr(src []byte) (Expr, error) { return engine.CompileExpr(src) }

// FormatValue is the canonical computed-cell text for v — byte-identical to
// what WriteTSV emits for that value in a computed grid. A 2-D array value
// reduces to its scalar-context (top-left) value before formatting.
func FormatValue(v Value) string { return engine.FormatValue(v) }
