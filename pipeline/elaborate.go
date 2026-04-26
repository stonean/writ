package pipeline

import (
	"fmt"

	"github.com/stonean/writ/ast"
)

// Elaborate resolves a parsed *ast.Program into a per-handler effective
// pipeline structure plus a flat list of structured elaboration errors.
//
// The returned *Resolved is always non-nil; on partial failure it
// contains entries for every handler that elaborated cleanly. The
// returned error slice is the authoritative success indicator —
// `len(errs) == 0` means elaboration succeeded.
//
// Elaborate accepts a partial program. Per the spec's *Inputs*
// contract, partial nodes (e.g. handlers with no Pattern, system
// blocks with nil Statements) are skipped silently — the parser
// already named the failure location and reason.
//
// Errors are emitted in walk order: system-level placement and order
// errors first, then per-group placement and order errors in
// declaration order, then per-handler errors in declaration order
// (placement and order errors from the handler's own block, followed
// by ambiguous-group and ambiguous-errors-block errors for that
// handler).
func Elaborate(prog *ast.Program) (*Resolved, []Error) {
	resolved := &Resolved{}
	if prog == nil {
		return resolved, nil
	}

	var errs []Error

	var sysEntries []levelEntry
	if prog.System != nil {
		var sysErrs []Error
		sysEntries, sysErrs = composeLevel(prog.System.Statements, SourceSystem)
		errs = append(errs, sysErrs...)
	}

	groupEntriesAll := make([][]levelEntry, len(prog.Groups))
	for i, g := range prog.Groups {
		if g == nil {
			continue
		}
		gentries, gerrs := composeLevel(g.Statements, SourceGroup)
		groupEntriesAll[i] = gentries
		errs = append(errs, gerrs...)
	}

	for _, h := range prog.Handlers {
		if h == nil || h.Pattern == nil || h.Method == "" {
			continue
		}

		handlerEntries, hErrs := composeLevel(h.Statements, SourceHandler)
		errs = append(errs, hErrs...)

		patternOf := func(g *ast.GroupBlock) *ast.RoutePattern { return g.Pattern }
		validGroups := make([]*ast.GroupBlock, 0, len(prog.Groups))
		validGroupIdx := make([]int, 0, len(prog.Groups))
		for i, g := range prog.Groups {
			if g == nil || g.Pattern == nil {
				continue
			}
			validGroups = append(validGroups, g)
			validGroupIdx = append(validGroupIdx, i)
		}
		keptGroups, conflictingGroups := findKept(validGroups, patternOf, h.Pattern)

		if len(conflictingGroups) > 0 {
			spans := make([]ast.Span, 0, len(conflictingGroups))
			for _, c := range conflictingGroups {
				spans = append(spans, c.Span())
			}
			errs = append(errs, Error{
				File:    h.Span().Start.Source.Path,
				Line:    h.Span().Start.Line,
				Column:  h.Span().Start.Column,
				Span:    h.Span(),
				Spans:   spans,
				Kind:    AmbiguousGroup,
				Message: fmt.Sprintf("handler matches %d groups whose patterns overlap without containment", len(conflictingGroups)),
			})
		}

		declIdx := make(map[*ast.GroupBlock]int, len(validGroups))
		for j, g := range validGroups {
			declIdx[g] = validGroupIdx[j]
		}
		sortBySpecificity(keptGroups, patternOf, func(g *ast.GroupBlock) int { return declIdx[g] })

		groupEntries := make([][]levelEntry, len(keptGroups))
		for j, g := range keptGroups {
			groupEntries[j] = groupEntriesAll[declIdx[g]]
		}

		stages, optOuts := buildEffectiveStages(sysEntries, groupEntries, keptGroups, handlerEntries)

		errMap, errMapErrs := buildErrorMap(h, prog.Errors)
		errs = append(errs, errMapErrs...)

		resolved.Handlers = append(resolved.Handlers, &Handler{
			Method:     h.Method,
			MethodSpan: h.MethodSpan,
			Pattern:    h.Pattern,
			Stages:     stages,
			OptOuts:    optOuts,
			ErrorMap:   errMap,
			Span:       h.Span(),
			Source:     h,
		})
	}

	return resolved, errs
}
