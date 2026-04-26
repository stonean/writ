package pipeline

import (
	"fmt"

	"github.com/stonean/writ/ast"
)

// ErrorKind classifies an elaboration error.
type ErrorKind int

const (
	// StagePlacement is a declaration the grammar accepts but elaboration's
	// placement rules forbid: format/redirect outside a handler block, or
	// format none / redirect none at any level.
	StagePlacement ErrorKind = iota

	// StageOrder is a semantic stage written in non-canonical source order
	// within its block.
	StageOrder

	// AmbiguousGroup is two or more matching groups whose patterns overlap
	// without containment, leaving no rule to determine which group's
	// overrides apply.
	AmbiguousGroup

	// AmbiguousErrorsBlock is two or more matching errors blocks whose
	// patterns overlap without containment, leaving no rule to determine
	// which block's entries take precedence.
	AmbiguousErrorsBlock
)

// String returns the human-readable name of the kind.
func (k ErrorKind) String() string {
	switch k {
	case StagePlacement:
		return "stage-placement"
	case StageOrder:
		return "stage-order"
	case AmbiguousGroup:
		return "ambiguous-group"
	case AmbiguousErrorsBlock:
		return "ambiguous-errors-block"
	}
	return fmt.Sprintf("ErrorKind(%d)", int(k))
}

// Error is a structured elaboration error.
//
// Every Error carries the file path, 1-based line and column of its
// starting position, and the full Span the error covers. Spans is
// non-empty for ambiguity errors that point at every conflicting block
// plus the affected handler; for single-site errors Spans is empty.
//
// Multiple Errors may be returned from a single Elaborate call; the
// elaborator reports every violation in a single pass rather than
// aborting on the first.
type Error struct {
	File    string
	Line    int
	Column  int
	Span    ast.Span
	Spans   []ast.Span
	Kind    ErrorKind
	Message string
}

// Error formats as "file:line:col: message".
func (e Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Message)
}

// newError builds an Error from a primary span and message.
func newError(kind ErrorKind, span ast.Span, message string) Error {
	return Error{
		File:    span.Start.Source.Path,
		Line:    span.Start.Line,
		Column:  span.Start.Column,
		Span:    span,
		Kind:    kind,
		Message: message,
	}
}
