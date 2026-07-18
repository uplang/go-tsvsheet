package tsvsheet_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tsvsheet "github.com/tsvsheet/go-tsvsheet"
)

// The facade exposes the source-preserving document layer: ParseDocument keeps
// comment and shebang lines that Parse's grid drops, and Text writes the
// canonical source back — the one sanctioned way to serialize a .tsvt.
func TestFacadeDocumentRoundTrip(t *testing.T) {
	src := "#!/usr/bin/env tsvsheet\n# prices\n2\t3\n# note\n=A1*B1\t=A1+B1\n"
	doc, err := tsvsheet.ParseDocument([]byte(src))
	require.NoError(t, err)
	assert.Equal(t, src, string(doc.Text()))

	edited, err := doc.SetCell(tsvsheet.Address{Row: 0, Col: 0}, "7", tsvsheet.DefaultLimits())
	require.NoError(t, err)
	assert.Equal(t, "#!/usr/bin/env tsvsheet\n# prices\n7\t3\n# note\n=A1*B1\t=A1+B1\n", string(edited.Text()))
	assert.Equal(t, tsvsheet.Grid{{"7", "3"}, {"21", "10"}}, edited.Sheet().Compute())
}
