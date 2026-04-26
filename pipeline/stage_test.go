package pipeline

import (
	"testing"

	"github.com/stonean/writ/ast"
)

// TestStageAccessors exercises every concrete Stage's Kind, Span,
// SourceLevel, SourceGroup, and kind-specific accessors. The override
// engine constructs Stages from level entries; without these direct
// tests every accessor goes uncovered because the engine itself does
// not call them.
func TestStageAccessors(t *testing.T) {
	src := newSrc("p.writ")
	gb := ast.NewGroupBlock(span(src, 1))

	t.Run("LogStage", func(t *testing.T) {
		stmt := ast.NewLogStmt(span(src, 10))
		stmt.Args = []ast.Expr{}
		s := newLogStage(stmt, SourceSystem, nil)
		if s.Kind() != StageLog {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Span() != stmt.Span() {
			t.Errorf("Span mismatch")
		}
		if s.SourceLevel() != SourceSystem {
			t.Errorf("SourceLevel = %v", s.SourceLevel())
		}
		if s.SourceGroup() != nil {
			t.Errorf("SourceGroup should be nil for system level")
		}
		if got := s.Args(); got == nil || len(got) != 0 {
			t.Errorf("Args = %v, want empty slice", got)
		}
	})

	t.Run("MeasureStage", func(t *testing.T) {
		stmt := ast.NewMeasureStmt(span(src, 11))
		s := newMeasureStage(stmt, SourceGroup, gb)
		if s.Kind() != StageMeasure {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.SourceGroup() != gb {
			t.Errorf("SourceGroup mismatch")
		}
		_ = s.Args()
		_ = s.Span()
		_ = s.SourceLevel()
	})

	t.Run("SessionStage", func(t *testing.T) {
		stmt := ast.NewSessionStmt(span(src, 12))
		stmt.Storage = "cookie"
		stmt.StorageSpan = span(src, 12)
		s := newSessionStage(stmt, SourceHandler, nil)
		if s.Kind() != StageSession {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Storage() != "cookie" {
			t.Errorf("Storage = %q, want cookie", s.Storage())
		}
		if s.StorageSpan() != stmt.StorageSpan {
			t.Errorf("StorageSpan mismatch")
		}
		_ = s.Span()
		_ = s.SourceLevel()
		_ = s.SourceGroup()
	})

	t.Run("CSRFStage", func(t *testing.T) {
		stmt := ast.NewCSRFStmt(span(src, 13))
		stmt.Mode = "auto"
		stmt.ModeSpan = span(src, 13)
		s := newCSRFStage(stmt, SourceSystem, nil)
		if s.Kind() != StageCSRF {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Mode() != "auto" {
			t.Errorf("Mode = %q", s.Mode())
		}
		if s.ModeSpan() != stmt.ModeSpan {
			t.Errorf("ModeSpan mismatch")
		}
		_ = s.Span()
		_ = s.SourceLevel()
		_ = s.SourceGroup()
	})

	t.Run("LimitStage", func(t *testing.T) {
		stmt := ast.NewLimitStmt(span(src, 14))
		call := ast.NewCall(span(src, 14))
		stmt.Call = call
		s := newLimitStage(stmt, SourceSystem, nil)
		if s.Kind() != StageLimit {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Call() != call {
			t.Errorf("Call mismatch")
		}
		_ = s.Span()
		_ = s.SourceLevel()
	})

	t.Run("ApproveStage", func(t *testing.T) {
		stmt := ast.NewApproveStmt(span(src, 15))
		expr := ast.NewApproveCall(span(src, 15), ast.NewCall(span(src, 15)))
		stmt.Expr = expr
		s := newApproveStage(stmt, SourceHandler, nil)
		if s.Kind() != StageApprove {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Expr() != expr {
			t.Errorf("Expr mismatch")
		}
		_ = s.Span()
		_ = s.SourceLevel()
		_ = s.SourceGroup()
	})

	t.Run("ResolveStage", func(t *testing.T) {
		stmt := ast.NewResolveStmt(span(src, 16))
		stmt.Name = "user"
		stmt.NameSpan = span(src, 16)
		stmt.Call = ast.NewCall(span(src, 16))
		s := newResolveStage(stmt, SourceSystem, nil)
		if s.Kind() != StageResolve {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Name() != "user" {
			t.Errorf("Name = %q", s.Name())
		}
		if s.NameSpan() != stmt.NameSpan {
			t.Errorf("NameSpan mismatch")
		}
		if s.Call() != stmt.Call {
			t.Errorf("Call mismatch")
		}
		_ = s.Span()
		_ = s.SourceLevel()
		_ = s.SourceGroup()
	})

	t.Run("CommitStageNamed", func(t *testing.T) {
		stmt := ast.NewCommitStmt(span(src, 17))
		stmt.Name = "result"
		stmt.NameSpan = span(src, 17)
		stmt.Call = ast.NewCall(span(src, 17))
		s := newCommitStage(stmt, SourceHandler, nil)
		if s.Kind() != StageCommit {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Name() != "result" {
			t.Errorf("Name = %q", s.Name())
		}
		if s.IsFireAndForget() {
			t.Errorf("IsFireAndForget = true, want false for named commit")
		}
		_ = s.NameSpan()
		_ = s.Call()
		_ = s.Span()
		_ = s.SourceLevel()
		_ = s.SourceGroup()
	})

	t.Run("CommitStageFireAndForget", func(t *testing.T) {
		stmt := ast.NewCommitStmt(span(src, 18))
		stmt.Call = ast.NewCall(span(src, 18))
		s := newCommitStage(stmt, SourceHandler, nil)
		if !s.IsFireAndForget() {
			t.Errorf("IsFireAndForget = false, want true for unnamed commit")
		}
	})

	t.Run("EmitStageWithData", func(t *testing.T) {
		stmt := ast.NewEmitStmt(span(src, 19))
		stmt.Event = "user.created"
		stmt.EventSpan = span(src, 19)
		stmt.Data = "user"
		stmt.DataSpan = span(src, 19)
		s := newEmitStage(stmt, SourceHandler, nil)
		if s.Kind() != StageEmit {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Event() != "user.created" {
			t.Errorf("Event = %q", s.Event())
		}
		if s.Data() != "user" {
			t.Errorf("Data = %q", s.Data())
		}
		if !s.HasData() {
			t.Errorf("HasData = false, want true")
		}
		_ = s.EventSpan()
		_ = s.DataSpan()
		_ = s.Span()
		_ = s.SourceLevel()
		_ = s.SourceGroup()
	})

	t.Run("EmitStageNoData", func(t *testing.T) {
		stmt := ast.NewEmitStmt(span(src, 20))
		stmt.Event = "user.deleted"
		s := newEmitStage(stmt, SourceHandler, nil)
		if s.HasData() {
			t.Errorf("HasData = true, want false for emit without with-clause")
		}
	})

	t.Run("LayoutStage", func(t *testing.T) {
		stmt := ast.NewLayoutStmt(span(src, 21))
		stmt.Name = "app"
		stmt.NameSpan = span(src, 21)
		s := newLayoutStage(stmt, SourceGroup, gb)
		if s.Kind() != StageLayout {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Name() != "app" {
			t.Errorf("Name = %q", s.Name())
		}
		if s.NameSpan() != stmt.NameSpan {
			t.Errorf("NameSpan mismatch")
		}
		if s.SourceGroup() != gb {
			t.Errorf("SourceGroup mismatch")
		}
		_ = s.Span()
		_ = s.SourceLevel()
	})

	t.Run("FormatStage", func(t *testing.T) {
		stmt := ast.NewFormatStmt(span(src, 22))
		stmt.Template = "user.show.json"
		stmt.TemplateSpan = span(src, 22)
		stmt.Data = []ast.NamedRef{}
		stmt.Layout = "main"
		stmt.LayoutSpan = span(src, 22)
		s := newFormatStage(stmt, SourceHandler, nil)
		if s.Kind() != StageFormat {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.Template() != "user.show.json" {
			t.Errorf("Template = %q", s.Template())
		}
		if s.Layout() != "main" {
			t.Errorf("Layout = %q", s.Layout())
		}
		_ = s.TemplateSpan()
		_ = s.Data()
		_ = s.LayoutSpan()
		_ = s.Span()
		_ = s.SourceLevel()
		_ = s.SourceGroup()
	})

	t.Run("RedirectStage", func(t *testing.T) {
		stmt := ast.NewRedirectStmt(span(src, 23))
		stmt.URL = "/users"
		stmt.URLSpan = span(src, 23)
		s := newRedirectStage(stmt, SourceHandler, nil)
		if s.Kind() != StageRedirect {
			t.Errorf("Kind = %v", s.Kind())
		}
		if s.URL() != "/users" {
			t.Errorf("URL = %q", s.URL())
		}
		if s.URLSpan() != stmt.URLSpan {
			t.Errorf("URLSpan mismatch")
		}
		_ = s.Span()
		_ = s.SourceLevel()
		_ = s.SourceGroup()
	})
}
