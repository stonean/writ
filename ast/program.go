package ast

// Program is the AST root. Parse always returns a non-nil Program,
// even when errors are present. Top-level constructs are kept in
// declaration order within their slice.
type Program struct {
	nodeBase
	Sources  []*Source
	System   *SystemBlock
	Groups   []*GroupBlock
	Errors   []*ErrorsBlock
	Handlers []*HandlerBlock
}

// NewProgram constructs a Program with the given span.
func NewProgram(span Span) *Program {
	return &Program{nodeBase: nodeBase{span: span}}
}

// SystemBlock is the root-level pipeline-defaults block.
type SystemBlock struct {
	nodeBase
	Statements []Stmt
}

// NewSystemBlock constructs a SystemBlock with the given span.
func NewSystemBlock(span Span) *SystemBlock {
	return &SystemBlock{nodeBase: nodeBase{span: span}}
}

// GroupBlock is a route-prefix-scoped block.
type GroupBlock struct {
	nodeBase
	Pattern    *RoutePattern
	Statements []Stmt
}

// NewGroupBlock constructs a GroupBlock with the given span.
func NewGroupBlock(span Span) *GroupBlock {
	return &GroupBlock{nodeBase: nodeBase{span: span}}
}

// HandlerBlock is a method-and-route-scoped handler block.
type HandlerBlock struct {
	nodeBase
	Method     string
	MethodSpan Span
	Pattern    *RoutePattern
	Statements []Stmt
}

// NewHandlerBlock constructs a HandlerBlock with the given span.
func NewHandlerBlock(span Span) *HandlerBlock {
	return &HandlerBlock{nodeBase: nodeBase{span: span}}
}

// ErrorsBlock maps error type names to formatter names within a route scope.
type ErrorsBlock struct {
	nodeBase
	Pattern *RoutePattern
	Entries []*ErrorsEntry
}

// NewErrorsBlock constructs an ErrorsBlock with the given span.
func NewErrorsBlock(span Span) *ErrorsBlock {
	return &ErrorsBlock{nodeBase: nodeBase{span: span}}
}

// ErrorsEntry is one line inside an ErrorsBlock — `<TypeName> <formatter-name>`
// or `default <formatter-name>`.
type ErrorsEntry struct {
	nodeBase
	TypeName      string
	TypeSpan      Span
	Formatter     string
	FormatterSpan Span
	IsDefault     bool
}

// NewErrorsEntry constructs an ErrorsEntry with the given span.
func NewErrorsEntry(span Span) *ErrorsEntry {
	return &ErrorsEntry{nodeBase: nodeBase{span: span}}
}

// IncludeStmt is `include <path>`. After include resolution the
// included file's top-level constructs are inlined at the include
// point and IncludeStmt does not appear in the final Program. The type
// exists so error messages and partial-AST consumers can refer to the
// include site.
type IncludeStmt struct {
	nodeBase
	Path     string
	PathSpan Span
}

// NewIncludeStmt constructs an IncludeStmt with the given span.
func NewIncludeStmt(span Span) *IncludeStmt {
	return &IncludeStmt{nodeBase: nodeBase{span: span}}
}
