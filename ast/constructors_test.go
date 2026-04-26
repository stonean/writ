package ast

import "testing"

// TestConstructors exercises every exported New… constructor and the
// marker-method satisfactions they imply. The AST package is mostly
// data definitions and constructors; without a direct test like this,
// coverage stays at the percent of types that happen to be touched by
// downstream packages, which is far below the project's 80% per-
// package target.
func TestConstructors(t *testing.T) {
	src := &Source{Path: "p.writ"}
	pos := Position{Source: src, Line: 1, Column: 1}
	sp := Span{Start: pos, End: pos}

	t.Run("Program", func(t *testing.T) {
		p := NewProgram(sp)
		if p.Span() != sp {
			t.Errorf("Program.Span mismatch")
		}
	})

	t.Run("SystemBlock", func(t *testing.T) {
		b := NewSystemBlock(sp)
		if b.Span() != sp {
			t.Errorf("SystemBlock.Span mismatch")
		}
	})

	t.Run("GroupBlock", func(t *testing.T) {
		b := NewGroupBlock(sp)
		if b.Span() != sp {
			t.Errorf("GroupBlock.Span mismatch")
		}
	})

	t.Run("HandlerBlock", func(t *testing.T) {
		b := NewHandlerBlock(sp)
		if b.Span() != sp {
			t.Errorf("HandlerBlock.Span mismatch")
		}
	})

	t.Run("ErrorsBlock", func(t *testing.T) {
		b := NewErrorsBlock(sp)
		if b.Span() != sp {
			t.Errorf("ErrorsBlock.Span mismatch")
		}
	})

	t.Run("ErrorsEntry", func(t *testing.T) {
		e := NewErrorsEntry(sp)
		if e.Span() != sp {
			t.Errorf("ErrorsEntry.Span mismatch")
		}
	})

	t.Run("IncludeStmt", func(t *testing.T) {
		s := NewIncludeStmt(sp)
		if s.Span() != sp {
			t.Errorf("IncludeStmt.Span mismatch")
		}
	})

	t.Run("RoutePattern", func(t *testing.T) {
		p := NewRoutePattern(sp)
		if p.Span() != sp {
			t.Errorf("RoutePattern.Span mismatch")
		}
	})

	t.Run("LiteralSegment", func(t *testing.T) {
		s := NewLiteralSegment(sp, "users")
		if s.Text != "users" || s.Span() != sp {
			t.Errorf("LiteralSegment mismatch")
		}
		s.routeSegment()
	})

	t.Run("ParameterSegment", func(t *testing.T) {
		s := NewParameterSegment(sp, "id")
		if s.Name != "id" || s.Span() != sp {
			t.Errorf("ParameterSegment mismatch")
		}
		s.routeSegment()
	})

	t.Run("WildcardSegment", func(t *testing.T) {
		s := NewWildcardSegment(sp)
		if s.Span() != sp {
			t.Errorf("WildcardSegment mismatch")
		}
		s.routeSegment()
	})
}

// TestStmtConstructors exercises every Stmt constructor and its
// stmt() marker.
func TestStmtConstructors(t *testing.T) {
	src := &Source{Path: "p.writ"}
	pos := Position{Source: src, Line: 1, Column: 1}
	sp := Span{Start: pos, End: pos}

	logS := NewLogStmt(sp)
	measureS := NewMeasureStmt(sp)
	sessionS := NewSessionStmt(sp)
	csrfS := NewCSRFStmt(sp)
	limitS := NewLimitStmt(sp)
	approveS := NewApproveStmt(sp)
	resolveS := NewResolveStmt(sp)
	commitS := NewCommitStmt(sp)
	emitS := NewEmitStmt(sp)
	formatS := NewFormatStmt(sp)
	redirectS := NewRedirectStmt(sp)
	layoutS := NewLayoutStmt(sp)
	noneS := NewNoneStmt(sp)

	stmts := []Stmt{
		logS, measureS, sessionS, csrfS, limitS, approveS,
		resolveS, commitS, emitS, formatS, redirectS, layoutS, noneS,
	}
	for _, s := range stmts {
		if s.Span() != sp {
			t.Errorf("%T.Span mismatch", s)
		}
	}

	// Exercise the stmt() markers on real values (value receivers).
	logS.stmt()
	measureS.stmt()
	sessionS.stmt()
	csrfS.stmt()
	limitS.stmt()
	approveS.stmt()
	resolveS.stmt()
	commitS.stmt()
	emitS.stmt()
	formatS.stmt()
	redirectS.stmt()
	layoutS.stmt()
	noneS.stmt()
}

// TestExprConstructors exercises every Expr constructor and the
// expr()/literal() markers.
func TestExprConstructors(t *testing.T) {
	src := &Source{Path: "p.writ"}
	pos := Position{Source: src, Line: 1, Column: 1}
	sp := Span{Start: pos, End: pos}

	c := NewCall(sp)
	if c.Span() != sp {
		t.Errorf("Call.Span mismatch")
	}

	rp := NewRouteParamRef(sp)
	if rp.Span() != sp {
		t.Errorf("RouteParamRef.Span mismatch")
	}
	rp.expr()

	fr := NewFieldRef(sp)
	if fr.Span() != sp {
		t.Errorf("FieldRef.Span mismatch")
	}
	fr.expr()

	na := NewNamedArg(sp)
	if na.Span() != sp {
		t.Errorf("NamedArg.Span mismatch")
	}
	na.expr()

	br := NewBodyRef(sp)
	if br.Span() != sp {
		t.Errorf("BodyRef.Span mismatch")
	}
	br.expr()

	qr := NewQueryRef(sp)
	if qr.Span() != sp {
		t.Errorf("QueryRef.Span mismatch")
	}
	qr.expr()

	il := NewIntLit(sp, 42)
	if il.Span() != sp || il.Value != 42 {
		t.Errorf("IntLit mismatch")
	}
	il.expr()
	il.literal()

	sl := NewStringLit(sp, "hello")
	if sl.Span() != sp || sl.Value != "hello" {
		t.Errorf("StringLit mismatch")
	}
	sl.expr()
	sl.literal()

	rl := NewRateLit(sp, 60, "min")
	if rl.Span() != sp || rl.Count != 60 || rl.Unit != "min" {
		t.Errorf("RateLit mismatch")
	}
	rl.expr()
	rl.literal()

	nr := NewNamedRef(sp)
	if nr.Span() != sp {
		t.Errorf("NamedRef.Span mismatch")
	}

	ac := NewApproveCall(sp, c)
	if ac.Span() != sp {
		t.Errorf("ApproveCall.Span mismatch")
	}
	ac.approveExpr()

	an := NewApproveNot(sp, ac)
	if an.Span() != sp {
		t.Errorf("ApproveNot.Span mismatch")
	}
	an.approveExpr()

	aa := NewApproveAnd(sp, ac, ac)
	if aa.Span() != sp {
		t.Errorf("ApproveAnd.Span mismatch")
	}
	aa.approveExpr()

	ao := NewApproveOr(sp, ac, ac)
	if ao.Span() != sp {
		t.Errorf("ApproveOr.Span mismatch")
	}
	ao.approveExpr()
}

// TestNodeBaseSpan verifies the embedded nodeBase.Span() works.
func TestNodeBaseSpan(t *testing.T) {
	src := &Source{Path: "p.writ"}
	pos := Position{Source: src, Line: 5, Column: 3}
	sp := Span{Start: pos, End: pos}
	n := NewLogStmt(sp)
	if n.Span() != sp {
		t.Errorf("nodeBase.Span via LogStmt mismatch")
	}
}
