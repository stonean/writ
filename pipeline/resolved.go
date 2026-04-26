package pipeline

import (
	"fmt"

	"github.com/stonean/writ/ast"
)

// Resolved is the per-handler effective-pipeline structure produced by
// Elaborate. Always non-nil after Elaborate; on partial failure it
// contains entries for every handler that elaborated cleanly.
type Resolved struct {
	Handlers []*Handler
}

// Handler is one entry in Resolved — a single declared handler with
// its effective pipeline, opt-outs, and error map.
type Handler struct {
	Method     string
	MethodSpan ast.Span
	Pattern    *ast.RoutePattern
	Stages     []Stage
	OptOuts    []OptOut
	ErrorMap   []ErrorMapEntry
	Span       ast.Span
	Source     *ast.HandlerBlock
}

// OptOut records an explicit `<stage> none` declaration at the level
// whose declaration won.
type OptOut struct {
	Kind StageKind
	Span ast.Span
}

// ErrorMapEntry is one entry in a handler's effective error map.
// SourceBlock points at the originating ast.ErrorsBlock; the entry
// that wins is the most-specific block's entry for that TypeName.
type ErrorMapEntry struct {
	TypeName      string
	TypeSpan      ast.Span
	Formatter     string
	FormatterSpan ast.Span
	IsDefault     bool
	SourceBlock   *ast.ErrorsBlock
}

// StageKind identifies a pipeline stage by category.
type StageKind int

const (
	StageLog StageKind = iota
	StageMeasure
	StageSession
	StageCSRF
	StageLimit
	StageApprove
	StageResolve
	StageCommit
	StageEmit
	StageLayout
	StageFormat
	StageRedirect
)

// CanonicalPosition returns the canonical pipeline position of a
// semantic stage kind (1-based: session=1, csrf=2, limit=3, approve=4,
// resolve=5, commit=6, emit=7, layout=8, format/redirect=9). Returns
// 0 for the observational kinds (log, measure), which are exempt from
// canonical ordering.
func (k StageKind) CanonicalPosition() int {
	switch k {
	case StageSession:
		return 1
	case StageCSRF:
		return 2
	case StageLimit:
		return 3
	case StageApprove:
		return 4
	case StageResolve:
		return 5
	case StageCommit:
		return 6
	case StageEmit:
		return 7
	case StageLayout:
		return 8
	case StageFormat, StageRedirect:
		return 9
	}
	return 0
}

// IsObservational reports whether the kind is log or measure.
func (k StageKind) IsObservational() bool {
	return k == StageLog || k == StageMeasure
}

// IsSingleInstance reports whether at most one declaration per level
// is allowed: session, csrf, limit, approve, layout. Terminators
// (format, redirect) are handled separately by IsTerminal.
func (k StageKind) IsSingleInstance() bool {
	switch k {
	case StageSession, StageCSRF, StageLimit, StageApprove, StageLayout:
		return true
	}
	return false
}

// IsMultiInstance reports whether multiple declarations per level are
// allowed for a semantic stage: resolve, commit, emit. Observational
// kinds are also multi-instance but classified separately by
// IsObservational.
func (k StageKind) IsMultiInstance() bool {
	switch k {
	case StageResolve, StageCommit, StageEmit:
		return true
	}
	return false
}

// IsTerminal reports whether the kind ends a pipeline: format,
// redirect.
func (k StageKind) IsTerminal() bool {
	return k == StageFormat || k == StageRedirect
}

// String returns the canonical lowercase keyword for the kind.
func (k StageKind) String() string {
	switch k {
	case StageLog:
		return "log"
	case StageMeasure:
		return "measure"
	case StageSession:
		return "session"
	case StageCSRF:
		return "csrf"
	case StageLimit:
		return "limit"
	case StageApprove:
		return "approve"
	case StageResolve:
		return "resolve"
	case StageCommit:
		return "commit"
	case StageEmit:
		return "emit"
	case StageLayout:
		return "layout"
	case StageFormat:
		return "format"
	case StageRedirect:
		return "redirect"
	}
	return fmt.Sprintf("StageKind(%d)", int(k))
}

// SourceLevel identifies which precedence level contributed a stage.
type SourceLevel int

const (
	SourceSystem SourceLevel = iota
	SourceGroup
	SourceHandler
)

// String returns the canonical lowercase name of the level.
func (l SourceLevel) String() string {
	switch l {
	case SourceSystem:
		return "system"
	case SourceGroup:
		return "group"
	case SourceHandler:
		return "handler"
	}
	return fmt.Sprintf("SourceLevel(%d)", int(l))
}
