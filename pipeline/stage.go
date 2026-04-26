package pipeline

import "github.com/stonean/writ/ast"

// Stage is one entry in a Handler's effective pipeline. Concrete
// types expose accessors specific to their kind.
type Stage interface {
	Kind() StageKind
	Span() ast.Span
	SourceLevel() SourceLevel
	SourceGroup() *ast.GroupBlock
}

// LogStage corresponds to a `log <args>` declaration.
type LogStage struct {
	src   *ast.LogStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newLogStage(src *ast.LogStmt, level SourceLevel, group *ast.GroupBlock) *LogStage {
	return &LogStage{src: src, level: level, group: group}
}

func (s *LogStage) Kind() StageKind              { return StageLog }
func (s *LogStage) Span() ast.Span               { return s.src.Span() }
func (s *LogStage) SourceLevel() SourceLevel     { return s.level }
func (s *LogStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *LogStage) Args() []ast.Expr             { return s.src.Args }

// MeasureStage corresponds to a `measure <args>` declaration.
type MeasureStage struct {
	src   *ast.MeasureStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newMeasureStage(src *ast.MeasureStmt, level SourceLevel, group *ast.GroupBlock) *MeasureStage {
	return &MeasureStage{src: src, level: level, group: group}
}

func (s *MeasureStage) Kind() StageKind              { return StageMeasure }
func (s *MeasureStage) Span() ast.Span               { return s.src.Span() }
func (s *MeasureStage) SourceLevel() SourceLevel     { return s.level }
func (s *MeasureStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *MeasureStage) Args() []ast.Expr             { return s.src.Args }

// SessionStage corresponds to a `session <storage>` declaration.
type SessionStage struct {
	src   *ast.SessionStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newSessionStage(src *ast.SessionStmt, level SourceLevel, group *ast.GroupBlock) *SessionStage {
	return &SessionStage{src: src, level: level, group: group}
}

func (s *SessionStage) Kind() StageKind              { return StageSession }
func (s *SessionStage) Span() ast.Span               { return s.src.Span() }
func (s *SessionStage) SourceLevel() SourceLevel     { return s.level }
func (s *SessionStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *SessionStage) Storage() string              { return s.src.Storage }
func (s *SessionStage) StorageSpan() ast.Span        { return s.src.StorageSpan }

// CSRFStage corresponds to a `csrf <mode>` declaration.
type CSRFStage struct {
	src   *ast.CSRFStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newCSRFStage(src *ast.CSRFStmt, level SourceLevel, group *ast.GroupBlock) *CSRFStage {
	return &CSRFStage{src: src, level: level, group: group}
}

func (s *CSRFStage) Kind() StageKind              { return StageCSRF }
func (s *CSRFStage) Span() ast.Span               { return s.src.Span() }
func (s *CSRFStage) SourceLevel() SourceLevel     { return s.level }
func (s *CSRFStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *CSRFStage) Mode() string                 { return s.src.Mode }
func (s *CSRFStage) ModeSpan() ast.Span           { return s.src.ModeSpan }

// LimitStage corresponds to a `limit <call>` declaration.
type LimitStage struct {
	src   *ast.LimitStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newLimitStage(src *ast.LimitStmt, level SourceLevel, group *ast.GroupBlock) *LimitStage {
	return &LimitStage{src: src, level: level, group: group}
}

func (s *LimitStage) Kind() StageKind              { return StageLimit }
func (s *LimitStage) Span() ast.Span               { return s.src.Span() }
func (s *LimitStage) SourceLevel() SourceLevel     { return s.level }
func (s *LimitStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *LimitStage) Call() *ast.Call              { return s.src.Call }

// ApproveStage corresponds to an `approve <expression>` declaration.
type ApproveStage struct {
	src   *ast.ApproveStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newApproveStage(src *ast.ApproveStmt, level SourceLevel, group *ast.GroupBlock) *ApproveStage {
	return &ApproveStage{src: src, level: level, group: group}
}

func (s *ApproveStage) Kind() StageKind              { return StageApprove }
func (s *ApproveStage) Span() ast.Span               { return s.src.Span() }
func (s *ApproveStage) SourceLevel() SourceLevel     { return s.level }
func (s *ApproveStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *ApproveStage) Expr() ast.ApproveExpr        { return s.src.Expr }

// ResolveStage corresponds to a `resolve <name> = <call>` declaration.
type ResolveStage struct {
	src   *ast.ResolveStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newResolveStage(src *ast.ResolveStmt, level SourceLevel, group *ast.GroupBlock) *ResolveStage {
	return &ResolveStage{src: src, level: level, group: group}
}

func (s *ResolveStage) Kind() StageKind              { return StageResolve }
func (s *ResolveStage) Span() ast.Span               { return s.src.Span() }
func (s *ResolveStage) SourceLevel() SourceLevel     { return s.level }
func (s *ResolveStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *ResolveStage) Name() string                 { return s.src.Name }
func (s *ResolveStage) NameSpan() ast.Span           { return s.src.NameSpan }
func (s *ResolveStage) Call() *ast.Call              { return s.src.Call }

// CommitStage corresponds to a `commit [<name> =] <call>` declaration.
// Name is empty for fire-and-forget commits.
type CommitStage struct {
	src   *ast.CommitStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newCommitStage(src *ast.CommitStmt, level SourceLevel, group *ast.GroupBlock) *CommitStage {
	return &CommitStage{src: src, level: level, group: group}
}

func (s *CommitStage) Kind() StageKind              { return StageCommit }
func (s *CommitStage) Span() ast.Span               { return s.src.Span() }
func (s *CommitStage) SourceLevel() SourceLevel     { return s.level }
func (s *CommitStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *CommitStage) Name() string                 { return s.src.Name }
func (s *CommitStage) NameSpan() ast.Span           { return s.src.NameSpan }
func (s *CommitStage) Call() *ast.Call              { return s.src.Call }
func (s *CommitStage) IsFireAndForget() bool        { return s.src.Name == "" }

// EmitStage corresponds to an `emit <event-name> [with <data-name>]`
// declaration.
type EmitStage struct {
	src   *ast.EmitStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newEmitStage(src *ast.EmitStmt, level SourceLevel, group *ast.GroupBlock) *EmitStage {
	return &EmitStage{src: src, level: level, group: group}
}

func (s *EmitStage) Kind() StageKind              { return StageEmit }
func (s *EmitStage) Span() ast.Span               { return s.src.Span() }
func (s *EmitStage) SourceLevel() SourceLevel     { return s.level }
func (s *EmitStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *EmitStage) Event() string                { return s.src.Event }
func (s *EmitStage) EventSpan() ast.Span          { return s.src.EventSpan }
func (s *EmitStage) Data() string                 { return s.src.Data }
func (s *EmitStage) DataSpan() ast.Span           { return s.src.DataSpan }
func (s *EmitStage) HasData() bool                { return s.src.Data != "" }

// LayoutStage corresponds to a `layout <name>` declaration.
type LayoutStage struct {
	src   *ast.LayoutStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newLayoutStage(src *ast.LayoutStmt, level SourceLevel, group *ast.GroupBlock) *LayoutStage {
	return &LayoutStage{src: src, level: level, group: group}
}

func (s *LayoutStage) Kind() StageKind              { return StageLayout }
func (s *LayoutStage) Span() ast.Span               { return s.src.Span() }
func (s *LayoutStage) SourceLevel() SourceLevel     { return s.level }
func (s *LayoutStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *LayoutStage) Name() string                 { return s.src.Name }
func (s *LayoutStage) NameSpan() ast.Span           { return s.src.NameSpan }

// FormatStage corresponds to a `format <template> with <data-list>
// [using layout <name>]` declaration.
type FormatStage struct {
	src   *ast.FormatStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newFormatStage(src *ast.FormatStmt, level SourceLevel, group *ast.GroupBlock) *FormatStage {
	return &FormatStage{src: src, level: level, group: group}
}

func (s *FormatStage) Kind() StageKind              { return StageFormat }
func (s *FormatStage) Span() ast.Span               { return s.src.Span() }
func (s *FormatStage) SourceLevel() SourceLevel     { return s.level }
func (s *FormatStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *FormatStage) Template() string             { return s.src.Template }
func (s *FormatStage) TemplateSpan() ast.Span       { return s.src.TemplateSpan }
func (s *FormatStage) Data() []ast.NamedRef         { return s.src.Data }
func (s *FormatStage) Layout() string               { return s.src.Layout }
func (s *FormatStage) LayoutSpan() ast.Span         { return s.src.LayoutSpan }

// RedirectStage corresponds to a `redirect <url-template>` declaration.
type RedirectStage struct {
	src   *ast.RedirectStmt
	level SourceLevel
	group *ast.GroupBlock
}

func newRedirectStage(src *ast.RedirectStmt, level SourceLevel, group *ast.GroupBlock) *RedirectStage {
	return &RedirectStage{src: src, level: level, group: group}
}

func (s *RedirectStage) Kind() StageKind              { return StageRedirect }
func (s *RedirectStage) Span() ast.Span               { return s.src.Span() }
func (s *RedirectStage) SourceLevel() SourceLevel     { return s.level }
func (s *RedirectStage) SourceGroup() *ast.GroupBlock { return s.group }
func (s *RedirectStage) URL() string                  { return s.src.URL }
func (s *RedirectStage) URLSpan() ast.Span            { return s.src.URLSpan }
