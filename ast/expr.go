package ast

// Call is `<name>(<args>)` — a dotted-identifier callable plus its
// argument list.
type Call struct {
	nodeBase
	Name     string
	NameSpan Span
	Args     []Expr
}

// NewCall constructs a Call with the given span.
func NewCall(span Span) *Call {
	return &Call{nodeBase: nodeBase{span: span}}
}

// Expr is anything that can appear in a call argument list or as a
// `with` data reference.
type Expr interface {
	Node
	expr()
}

// RouteParamRef is `:<identifier>` — a route parameter reference.
type RouteParamRef struct {
	nodeBase
	Name     string
	NameSpan Span
}

func (RouteParamRef) expr() {}

// NewRouteParamRef constructs a RouteParamRef with the given span.
func NewRouteParamRef(span Span) *RouteParamRef {
	return &RouteParamRef{nodeBase: nodeBase{span: span}}
}

// FieldRef is `:<root>.<segment>...` — field access on a previous
// resolve/commit result. Path always has at least one segment;
// otherwise the parser produces a RouteParamRef.
type FieldRef struct {
	nodeBase
	Root      string
	RootSpan  Span
	Path      []string
	PathSpans []Span
}

func (FieldRef) expr() {}

// NewFieldRef constructs a FieldRef with the given span.
func NewFieldRef(span Span) *FieldRef {
	return &FieldRef{nodeBase: nodeBase{span: span}}
}

// NamedArg is `<identifier>=<literal>` — a static named argument such
// as `limit=10` or `status="active"`.
type NamedArg struct {
	nodeBase
	Name     string
	NameSpan Span
	Value    Literal
}

func (NamedArg) expr() {}

// NewNamedArg constructs a NamedArg with the given span.
func NewNamedArg(span Span) *NamedArg {
	return &NamedArg{nodeBase: nodeBase{span: span}}
}

// BodyRef is `body <TypeName>` — a typed request-body reference.
type BodyRef struct {
	nodeBase
	TypeName string
	TypeSpan Span
}

func (BodyRef) expr() {}

// NewBodyRef constructs a BodyRef with the given span.
func NewBodyRef(span Span) *BodyRef {
	return &BodyRef{nodeBase: nodeBase{span: span}}
}

// QueryRef is `query <TypeName>` — a typed query-parameters reference.
type QueryRef struct {
	nodeBase
	TypeName string
	TypeSpan Span
}

func (QueryRef) expr() {}

// NewQueryRef constructs a QueryRef with the given span.
func NewQueryRef(span Span) *QueryRef {
	return &QueryRef{nodeBase: nodeBase{span: span}}
}

// Literal is a literal value usable as a standalone argument or as
// the value of a NamedArg.
type Literal interface {
	Expr
	literal()
}

// IntLit is a decimal integer literal, optionally negative.
type IntLit struct {
	nodeBase
	Value int64
}

func (IntLit) expr()    {}
func (IntLit) literal() {}

// NewIntLit constructs an IntLit with the given span.
func NewIntLit(span Span, value int64) *IntLit {
	return &IntLit{nodeBase: nodeBase{span: span}, Value: value}
}

// StringLit is a double-quoted string literal with escape sequences
// already processed.
type StringLit struct {
	nodeBase
	Value string
}

func (StringLit) expr()    {}
func (StringLit) literal() {}

// NewStringLit constructs a StringLit with the given span.
func NewStringLit(span Span, value string) *StringLit {
	return &StringLit{nodeBase: nodeBase{span: span}, Value: value}
}

// RateLit is `<count>/<unit>` where Unit is one of "sec", "min",
// "hour", "day".
type RateLit struct {
	nodeBase
	Count int64
	Unit  string
}

func (RateLit) expr()    {}
func (RateLit) literal() {}

// NewRateLit constructs a RateLit with the given span.
func NewRateLit(span Span, count int64, unit string) *RateLit {
	return &RateLit{nodeBase: nodeBase{span: span}, Count: count, Unit: unit}
}

// NamedRef is one entry in a `with <data-list>` clause — either a
// bare name (Path empty) or a dotted path. Used by FormatStmt.Data.
type NamedRef struct {
	nodeBase
	Name      string
	Path      []string
	PathSpans []Span
}

// NewNamedRef constructs a NamedRef with the given span.
func NewNamedRef(span Span) NamedRef {
	return NamedRef{nodeBase: nodeBase{span: span}}
}

// ApproveExpr is the boolean-expression tree built from `approve`
// statements. Precedence (NOT > AND > OR) and associativity (NOT
// right; AND/OR left) are encoded in the tree shape; parentheses
// from the source are flattened by the precedence-climbing parser.
type ApproveExpr interface {
	Node
	approveExpr()
}

// ApproveOr is `<left> OR <right>`.
type ApproveOr struct {
	nodeBase
	Left, Right ApproveExpr
}

func (ApproveOr) approveExpr() {}

// NewApproveOr constructs an ApproveOr with the given span.
func NewApproveOr(span Span, left, right ApproveExpr) *ApproveOr {
	return &ApproveOr{nodeBase: nodeBase{span: span}, Left: left, Right: right}
}

// ApproveAnd is `<left> AND <right>`.
type ApproveAnd struct {
	nodeBase
	Left, Right ApproveExpr
}

func (ApproveAnd) approveExpr() {}

// NewApproveAnd constructs an ApproveAnd with the given span.
func NewApproveAnd(span Span, left, right ApproveExpr) *ApproveAnd {
	return &ApproveAnd{nodeBase: nodeBase{span: span}, Left: left, Right: right}
}

// ApproveNot is `NOT <inner>`.
type ApproveNot struct {
	nodeBase
	Inner ApproveExpr
}

func (ApproveNot) approveExpr() {}

// NewApproveNot constructs an ApproveNot with the given span.
func NewApproveNot(span Span, inner ApproveExpr) *ApproveNot {
	return &ApproveNot{nodeBase: nodeBase{span: span}, Inner: inner}
}

// ApproveCall is a leaf — one approver invocation such as
// `auth.isOwner(:id)`.
type ApproveCall struct {
	nodeBase
	Call *Call
}

func (ApproveCall) approveExpr() {}

// NewApproveCall constructs an ApproveCall with the given span.
func NewApproveCall(span Span, call *Call) *ApproveCall {
	return &ApproveCall{nodeBase: nodeBase{span: span}, Call: call}
}
