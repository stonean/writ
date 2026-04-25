package ast

// Stmt is the marker interface for every pipeline statement.
type Stmt interface {
	Node
	stmt()
}

// LogStmt is `log <args>`.
type LogStmt struct {
	nodeBase
	Args []Expr
}

func (LogStmt) stmt() {}

// NewLogStmt constructs a LogStmt with the given span.
func NewLogStmt(span Span) *LogStmt {
	return &LogStmt{nodeBase: nodeBase{span: span}}
}

// MeasureStmt is `measure <args>`.
type MeasureStmt struct {
	nodeBase
	Args []Expr
}

func (MeasureStmt) stmt() {}

// NewMeasureStmt constructs a MeasureStmt with the given span.
func NewMeasureStmt(span Span) *MeasureStmt {
	return &MeasureStmt{nodeBase: nodeBase{span: span}}
}

// SessionStmt is `session <storage>`.
type SessionStmt struct {
	nodeBase
	Storage     string
	StorageSpan Span
}

func (SessionStmt) stmt() {}

// NewSessionStmt constructs a SessionStmt with the given span.
func NewSessionStmt(span Span) *SessionStmt {
	return &SessionStmt{nodeBase: nodeBase{span: span}}
}

// CSRFStmt is `csrf <mode>`.
type CSRFStmt struct {
	nodeBase
	Mode     string
	ModeSpan Span
}

func (CSRFStmt) stmt() {}

// NewCSRFStmt constructs a CSRFStmt with the given span.
func NewCSRFStmt(span Span) *CSRFStmt {
	return &CSRFStmt{nodeBase: nodeBase{span: span}}
}

// LimitStmt is `limit <call>`.
type LimitStmt struct {
	nodeBase
	Call *Call
}

func (LimitStmt) stmt() {}

// NewLimitStmt constructs a LimitStmt with the given span.
func NewLimitStmt(span Span) *LimitStmt {
	return &LimitStmt{nodeBase: nodeBase{span: span}}
}

// ApproveStmt is `approve <expression>`.
type ApproveStmt struct {
	nodeBase
	Expr ApproveExpr
}

func (ApproveStmt) stmt() {}

// NewApproveStmt constructs an ApproveStmt with the given span.
func NewApproveStmt(span Span) *ApproveStmt {
	return &ApproveStmt{nodeBase: nodeBase{span: span}}
}

// ResolveStmt is `resolve <name> = <call>`.
type ResolveStmt struct {
	nodeBase
	Name     string
	NameSpan Span
	Call     *Call
}

func (ResolveStmt) stmt() {}

// NewResolveStmt constructs a ResolveStmt with the given span.
func NewResolveStmt(span Span) *ResolveStmt {
	return &ResolveStmt{nodeBase: nodeBase{span: span}}
}

// CommitStmt is `commit [<name> =] <call>`. Name and NameSpan are
// the zero value for fire-and-forget commits.
type CommitStmt struct {
	nodeBase
	Name     string
	NameSpan Span
	Call     *Call
}

func (CommitStmt) stmt() {}

// NewCommitStmt constructs a CommitStmt with the given span.
func NewCommitStmt(span Span) *CommitStmt {
	return &CommitStmt{nodeBase: nodeBase{span: span}}
}

// EmitStmt is `emit <event-name> [with <data-name>]`. Data and
// DataSpan are the zero value when no `with` clause is present.
type EmitStmt struct {
	nodeBase
	Event     string
	EventSpan Span
	Data      string
	DataSpan  Span
}

func (EmitStmt) stmt() {}

// NewEmitStmt constructs an EmitStmt with the given span.
func NewEmitStmt(span Span) *EmitStmt {
	return &EmitStmt{nodeBase: nodeBase{span: span}}
}

// FormatStmt is `format <template> with <data-list> [using layout <name>]`.
// Layout and LayoutSpan are the zero value when no `using layout`
// clause is present.
type FormatStmt struct {
	nodeBase
	Template     string
	TemplateSpan Span
	Data         []NamedRef
	Layout       string
	LayoutSpan   Span
}

func (FormatStmt) stmt() {}

// NewFormatStmt constructs a FormatStmt with the given span.
func NewFormatStmt(span Span) *FormatStmt {
	return &FormatStmt{nodeBase: nodeBase{span: span}}
}

// RedirectStmt is `redirect <url-template>`.
type RedirectStmt struct {
	nodeBase
	URL     string
	URLSpan Span
}

func (RedirectStmt) stmt() {}

// NewRedirectStmt constructs a RedirectStmt with the given span.
func NewRedirectStmt(span Span) *RedirectStmt {
	return &RedirectStmt{nodeBase: nodeBase{span: span}}
}

// LayoutStmt is `layout <name>`.
type LayoutStmt struct {
	nodeBase
	Name     string
	NameSpan Span
}

func (LayoutStmt) stmt() {}

// NewLayoutStmt constructs a LayoutStmt with the given span.
func NewLayoutStmt(span Span) *LayoutStmt {
	return &LayoutStmt{nodeBase: nodeBase{span: span}}
}

// NoneStmt is `<stage> none` — explicit opt-out, distinct from
// `stage not declared`. The runtime relies on node *kind* to
// distinguish opt-out from inheritance.
type NoneStmt struct {
	nodeBase
	Stage     string
	StageSpan Span
}

func (NoneStmt) stmt() {}

// NewNoneStmt constructs a NoneStmt with the given span.
func NewNoneStmt(span Span) *NoneStmt {
	return &NoneStmt{nodeBase: nodeBase{span: span}}
}
