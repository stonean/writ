package pipeline

import (
	"fmt"

	"github.com/stonean/writ/ast"
)

// levelEntry is one entry in a per-level pipeline list. The level
// composer produces entries in canonical pipeline order with
// observational stages floated to the source positions they occupied
// relative to the surrounding semantic stages.
type levelEntry struct {
	kind   StageKind
	stmt   ast.Stmt
	isNone bool
}

// composeLevel walks one level's source statements (system, a group,
// or a handler) and produces:
//
//   - entries: an ordered list of per-level entries with semantic
//     stages canonicalized and observational stages floated to their
//     source positions. Statements that fail the stage-placement
//     check are excluded.
//   - errs: stage-placement errors (format/redirect outside handler,
//     format/redirect none at any level) and stage-order errors
//     (semantic stage in non-canonical source order). Stage-order
//     violations do not drop the offending statement; it still
//     appears at its canonical position.
//
// composeLevel is deterministic given the same input slice.
func composeLevel(stmts []ast.Stmt, level SourceLevel) ([]levelEntry, []Error) {
	const numSlots = 10 // index 0 = "before any semantic stage"; 1..9 = canonical positions
	var slots [numSlots][]levelEntry
	var errs []Error

	currentSlot := 0
	highestCanonical := 0

	for _, stmt := range stmts {
		kind, isNone, ok := classifyStmt(stmt)
		if !ok {
			continue
		}

		if kind.IsTerminal() {
			if level != SourceHandler {
				errs = append(errs, newError(StagePlacement, stmt.Span(),
					fmt.Sprintf("%s is only valid in a handler block", kind)))
				continue
			}
			if isNone {
				errs = append(errs, newError(StagePlacement, stmt.Span(),
					fmt.Sprintf("%s none is not a valid declaration; terminators do not support none", kind)))
				continue
			}
		}

		entry := levelEntry{kind: kind, stmt: stmt, isNone: isNone}

		if kind.IsObservational() {
			slots[currentSlot] = append(slots[currentSlot], entry)
			continue
		}

		canonical := kind.CanonicalPosition()
		if canonical < highestCanonical {
			errs = append(errs, newError(StageOrder, stmt.Span(),
				fmt.Sprintf("%s must appear at canonical position %d but follows a stage at position %d",
					kind, canonical, highestCanonical)))
		}
		if canonical > highestCanonical {
			highestCanonical = canonical
		}
		slots[canonical] = append(slots[canonical], entry)
		currentSlot = canonical
	}

	var entries []levelEntry
	for i := range numSlots {
		entries = append(entries, slots[i]...)
	}
	return entries, errs
}

// classifyStmt returns the StageKind, isNone flag, and an ok flag
// indicating whether the statement was recognized.
func classifyStmt(s ast.Stmt) (kind StageKind, isNone bool, ok bool) {
	switch v := s.(type) {
	case *ast.LogStmt:
		return StageLog, false, true
	case *ast.MeasureStmt:
		return StageMeasure, false, true
	case *ast.SessionStmt:
		return StageSession, false, true
	case *ast.CSRFStmt:
		return StageCSRF, false, true
	case *ast.LimitStmt:
		return StageLimit, false, true
	case *ast.ApproveStmt:
		return StageApprove, false, true
	case *ast.ResolveStmt:
		return StageResolve, false, true
	case *ast.CommitStmt:
		return StageCommit, false, true
	case *ast.EmitStmt:
		return StageEmit, false, true
	case *ast.LayoutStmt:
		return StageLayout, false, true
	case *ast.FormatStmt:
		return StageFormat, false, true
	case *ast.RedirectStmt:
		return StageRedirect, false, true
	case *ast.NoneStmt:
		k, kok := stageKindFromKeyword(v.Stage)
		return k, true, kok
	}
	return 0, false, false
}

// stageKindFromKeyword maps the verbatim stage keyword (as written in
// `<stage> none`) to its StageKind.
func stageKindFromKeyword(s string) (StageKind, bool) {
	switch s {
	case "log":
		return StageLog, true
	case "measure":
		return StageMeasure, true
	case "session":
		return StageSession, true
	case "csrf":
		return StageCSRF, true
	case "limit":
		return StageLimit, true
	case "approve":
		return StageApprove, true
	case "resolve":
		return StageResolve, true
	case "commit":
		return StageCommit, true
	case "emit":
		return StageEmit, true
	case "layout":
		return StageLayout, true
	case "format":
		return StageFormat, true
	case "redirect":
		return StageRedirect, true
	}
	return 0, false
}
