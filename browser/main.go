//go:build js && wasm

// Command browser exposes the tsvsheet engine to the browser as a set of STATELESS
// functions: the caller holds the .tsvt source, and each call parses it, applies
// one immutable engine operation, and returns the result as a JSON string. There
// is no server and no filesystem — SHEET(...) / "file"!A1 resolve to #REF! — but
// every other function, including the clock functions TODAY/NOW/ISNOW, works
// against the browser's own clock.
//
// go-tsvsheet's release CI builds this into the versioned tsvsheet.wasm asset
// that browser consumers (the docs playground, tsvsheet.js) pin. It is
// js/wasm-tagged, so it is invisible to the host quality gate.
package main

import (
	"encoding/json"
	"syscall/js"
	"time"

	tsvsheet "github.com/uplang/go-tsvsheet"
)

func main() {
	obj := js.Global().Get("Object").New()
	obj.Set("compute", js.FuncOf(compute))
	obj.Set("setCell", js.FuncOf(setCell))
	obj.Set("insertRow", js.FuncOf(edit(tsvsheet.Sheet.InsertRow)))
	obj.Set("deleteRow", js.FuncOf(edit(tsvsheet.Sheet.DeleteRow)))
	obj.Set("insertCol", js.FuncOf(edit(tsvsheet.Sheet.InsertCol)))
	obj.Set("deleteCol", js.FuncOf(edit(tsvsheet.Sheet.DeleteCol)))
	obj.Set("references", js.FuncOf(references))
	obj.Set("explain", js.FuncOf(explain))
	js.Global().Set("tsvsheet", obj)
	select {} // run until the page unloads
}

// view is the render model returned to JS after any operation: the computed
// grid, the (possibly edited) source, static diagnostics, and whether any
// formula is clock-volatile (so the page can offer periodic recompute).
type view struct {
	Computed    [][]string            `json:"computed"`
	Source      [][]string            `json:"source"`
	Diagnostics []tsvsheet.Diagnostic `json:"diagnostics"`
	Volatile    bool                  `json:"volatile"`
}

// render computes a sheet under the tighter browser limits and its own clock,
// and gathers the read model.
func render(sheet tsvsheet.Sheet) view {
	opts := tsvsheet.ComputeOptions{At: time.Now(), Limits: tsvsheet.BrowserLimits()}
	return view{
		Computed:    sheet.ComputeWith(opts),
		Source:      sheet.Source(),
		Diagnostics: tsvsheet.Check(sheet),
		Volatile:    sheet.IsVolatile(),
	}
}

// result marshals v (or a {"error": …} object on failure) to a JSON string.
func result(v any, err error) any {
	if err != nil {
		v = map[string]string{"error": err.Error()}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// addr builds a cell address from the (row, col) JS integer arguments.
func addr(row, col js.Value) tsvsheet.Address {
	return tsvsheet.Address{Row: row.Int(), Col: col.Int()}
}

// parse is the shared first step of every function: the source is args[0].
func parse(args []js.Value) (tsvsheet.Sheet, error) {
	return tsvsheet.Parse([]byte(args[0].String()))
}

// compute parses and renders the source (args: source).
func compute(_ js.Value, args []js.Value) any {
	sheet, err := parse(args)
	if err != nil {
		return result(nil, err)
	}
	return result(render(sheet), nil)
}

// setCell replaces one cell and re-renders (args: source, row, col, text).
func setCell(_ js.Value, args []js.Value) any {
	sheet, err := parse(args)
	if err != nil {
		return result(nil, err)
	}
	updated, err := sheet.Set(addr(args[1], args[2]), args[3].String(), tsvsheet.BrowserLimits())
	if err != nil {
		return result(nil, err)
	}
	return result(render(updated), nil)
}

// edit adapts an immutable structural operation into a JS function that parses,
// applies it at the given cell, and re-renders (args: source, row, col).
func edit(op func(tsvsheet.Sheet, tsvsheet.Address) tsvsheet.Sheet) func(js.Value, []js.Value) any {
	return func(_ js.Value, args []js.Value) any {
		sheet, err := parse(args)
		if err != nil {
			return result(nil, err)
		}
		return result(render(op(sheet, addr(args[1], args[2]))), nil)
	}
}

// references returns a cell's precedents and dependents (args: source, row, col).
func references(_ js.Value, args []js.Value) any {
	sheet, err := parse(args)
	if err != nil {
		return result(nil, err)
	}
	at := addr(args[1], args[2])
	return result(map[string]any{
		"precedents": sheet.Precedents(at),
		"dependents": sheet.Dependents(at),
	}, nil)
}

// explain traces how a cell was produced (args: source, row, col).
func explain(_ js.Value, args []js.Value) any {
	sheet, err := parse(args)
	if err != nil {
		return result(nil, err)
	}
	trace, err := tsvsheet.Explain(sheet, addr(args[1], args[2]))
	if err != nil {
		return result(nil, err)
	}
	return result(trace, nil)
}
