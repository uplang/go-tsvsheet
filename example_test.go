package tsvsheet_test

import (
	"fmt"

	tsvsheet "github.com/uplang/go-tsvsheet"
)

// Parse compiles a .tsvt grid; Compute evaluates every =formula in dependency
// order and returns the value grid ([][]string), literals passing through.
func ExampleParse() {
	sheet, err := tsvsheet.Parse([]byte("2\t3\n=A1*B1\t=A1+B1\n"))
	if err != nil {
		fmt.Println(err)
		return
	}
	grid := sheet.Compute()
	fmt.Println(grid[1][0], grid[1][1])
	// Output: 6 5
}

// A cell that fails to evaluate carries a spreadsheet error value, which
// propagates through the formulas that read it — it is data, not a Go error.
func ExampleParse_errorValues() {
	sheet, _ := tsvsheet.Parse([]byte("=1/0\t=A1+1\n"))
	grid := sheet.Compute()
	fmt.Println(grid[0][0], grid[0][1])
	// Output: #DIV/0! #DIV/0!
}

// A malformed formula is reported as ErrSyntax, matchable with errors.Is.
func ExampleParse_syntaxError() {
	_, err := tsvsheet.Parse([]byte("=1 +\n"))
	fmt.Println(err != nil)
	// Output: true
}

// Set returns a new sheet with one cell replaced — the engine is immutable, so
// the original is unchanged and the result recomputes from the edit.
func ExampleSheet_Set() {
	sheet, _ := tsvsheet.Parse([]byte("1\t=A1+10\n"))
	edited, _ := sheet.Set(tsvsheet.Address{Row: 0, Col: 0}, "5", tsvsheet.DefaultLimits())
	fmt.Println(sheet.Compute()[0][1], edited.Compute()[0][1])
	// Output: 11 15
}

// Check reports static diagnostics — unknown functions, provable arity errors,
// non-A1 references — without computing.
func ExampleCheck() {
	sheet, _ := tsvsheet.Parse([]byte("=BOGUS(1)\n"))
	for _, d := range tsvsheet.Check(sheet) {
		fmt.Printf("%s: %s\n", d.Cell, d.Message)
	}
	// Output: A1: unknown function: BOGUS
}

// Explain traces how a cell was produced: its value, formula, and the inputs
// the formula read.
func ExampleExplain() {
	sheet, _ := tsvsheet.Parse([]byte("2\t3\n=A1+B1\t\n"))
	trace, _ := tsvsheet.Explain(sheet, tsvsheet.Address{Row: 1, Col: 0})
	fmt.Printf("%s = %s (from %s, %d inputs)\n", trace.Cell, trace.Value, trace.Formula, len(trace.Inputs))
	// Output: A2 = 5 (from A1 + B1, 2 inputs)
}

// stubFetcher is a trivial Fetcher for the example below: it answers every
// request with the value 42, echoing the requested media type so the handshake
// succeeds.
type stubFetcher struct{}

func (stubFetcher) Fetch(_ tsvsheet.ImportURL, accept tsvsheet.MediaType) (tsvsheet.FetchResult, error) {
	return tsvsheet.FetchResult{ContentType: accept, Body: []byte("42")}, nil
}

// The engine is network-free: IMPORT* cells resolve only through a Fetcher
// injected via ComputeOptions. With none, they are #IMPORT!; with one, they
// resolve to the fetched value.
func ExampleFetcher() {
	sheet, _ := tsvsheet.Parse([]byte(`=IMPORTCELL("https://example/v")` + "\n"))
	fmt.Println(sheet.Compute()[0][0])
	fmt.Println(sheet.ComputeWith(tsvsheet.ComputeOptions{Fetcher: stubFetcher{}})[0][0])
	// Output:
	// #IMPORT!
	// 42
}
