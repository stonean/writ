// Package parser turns one or more .writ files into a single ast.Program
// plus a flat list of structured errors.
//
// Pre-1.0 the parser surface (Parse, ParseString, Option, Error) is
// stable enough to use but not promised to be source-compatible across
// minor versions; the AST it produces is similarly unstable per
// package ast's package comment. Internal Writ components (runtime,
// code generator, CLI) are the primary consumers; third-party use is
// welcome but accepts that instability.
package parser

import (
	"fmt"

	"github.com/stonean/writ/ast"
)

// Error is a structured parse-or-lex error.
//
// Every Error carries the file path, the 1-based line and column of
// its starting position, and the full Span the error covers. The Span
// references the originating Source so consumers can recover the
// offending bytes via Span.Text().
//
// Multiple Errors may be returned from a single Parse call; the parser
// reports as many as it can in a single pass rather than aborting on
// the first.
type Error struct {
	File    string
	Line    int
	Column  int
	Span    ast.Span
	Message string
}

// Error formats as "file:line:col: message".
func (e Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Message)
}
