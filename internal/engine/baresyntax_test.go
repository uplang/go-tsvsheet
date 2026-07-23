package engine_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBareNullaryAndPipe covers the drop-the-empty-parens affordance: a
// zero-argument call needs no parentheses, as a standalone value or as a pipe
// stage, and a bare column+row stays a cell reference rather than a call.
func TestBareNullaryAndPipe(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ expr, want string }{
		"bare nullary computes":        {"round(pi, 5)", "3.14159"},
		"bare nullary alone":           {"round(pi(), 5)", "3.14159"}, // parens still accepted
		"bare pipe stage computes":     {"A1 | len", "1"},             // len of A1's text "2"
		"parenthesized pipe unchanged": {"A1 | len()", "1"},
		"bare pipe chains":             {"A1 | trim | len", "1"},
		"A1 stays a reference":         {"A1", "2"}, // not a call named "A"
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, formula1(t, tc.expr))
		})
	}
}
