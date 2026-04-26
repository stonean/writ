package pipeline

import (
	"slices"

	"github.com/stonean/writ/ast"
)

// buildEffectiveStages applies the cross-level override rules to a
// handler's per-level entry lists and produces the effective stage list
// plus any explicit opt-outs.
//
// Inputs are the per-level outputs of composeLevel for each level in
// precedence order: system → kept groups (least- to most-specific) →
// handler. groupBlocks is parallel to groupEntries and provides the
// originating *ast.GroupBlock for each kept group.
//
// Override rules (per spec 002):
//
//   - Single-instance kinds (session, csrf, limit, approve, layout):
//     the latest declaration wins. `<kind> none` clears the current
//     value and records an opt-out, which a later declaration can
//     overwrite.
//   - Multi-instance kinds (resolve, commit, emit) and terminal kinds
//     (format, redirect): all declarations accumulate in cross-level
//     order. `<kind> none` clears the accumulated list at and below the
//     declaring level for that stage.
//   - Observational kinds (log, measure): same accumulating rule as
//     multi-instance, with each entry retaining its source-position
//     anchor within its level so the final list interleaves
//     observationals at the canonical position they belong to.
//
// The returned stages slice is in canonical pipeline order with
// observationals interleaved. The optOuts slice is ordered by
// StageKind for determinism.
func buildEffectiveStages(
	sysEntries []levelEntry,
	groupEntries [][]levelEntry,
	groupBlocks []*ast.GroupBlock,
	handlerEntries []levelEntry,
) (stages []Stage, optOuts []OptOut) {
	type leveledEntry struct {
		entry levelEntry
		level SourceLevel
		group *ast.GroupBlock
	}

	stream := make([]leveledEntry, 0,
		len(sysEntries)+len(handlerEntries)+sumLen(groupEntries))
	for _, e := range sysEntries {
		stream = append(stream, leveledEntry{e, SourceSystem, nil})
	}
	for i, gentries := range groupEntries {
		for _, e := range gentries {
			stream = append(stream, leveledEntry{e, SourceGroup, groupBlocks[i]})
		}
	}
	for _, e := range handlerEntries {
		stream = append(stream, leveledEntry{e, SourceHandler, nil})
	}

	n := len(stream)
	if n == 0 {
		return nil, nil
	}

	survives := make([]bool, n)
	for i := range survives {
		survives[i] = true
	}

	currentSingleIdx := make(map[StageKind]int)
	lastNoneIdx := make(map[StageKind]int)

	for i, le := range stream {
		k := le.entry.kind

		if le.entry.isNone {
			survives[i] = false

			if k.IsSingleInstance() {
				if prev, ok := currentSingleIdx[k]; ok {
					survives[prev] = false
					delete(currentSingleIdx, k)
				}
			} else {
				for j := range i {
					if survives[j] && stream[j].entry.kind == k && !stream[j].entry.isNone {
						survives[j] = false
					}
				}
			}
			lastNoneIdx[k] = i
			continue
		}

		if k.IsSingleInstance() {
			if prev, ok := currentSingleIdx[k]; ok {
				survives[prev] = false
			}
			currentSingleIdx[k] = i
		}
		delete(lastNoneIdx, k)
	}

	optKinds := make([]StageKind, 0, len(lastNoneIdx))
	for k := range lastNoneIdx {
		optKinds = append(optKinds, k)
	}
	slices.Sort(optKinds)
	for _, k := range optKinds {
		idx := lastNoneIdx[k]
		optOuts = append(optOuts, OptOut{Kind: k, Span: stream[idx].entry.stmt.Span()})
	}

	const numSlots = 10
	var buckets [numSlots][]leveledEntry
	for i, le := range stream {
		if !survives[i] {
			continue
		}
		buckets[le.entry.canonicalPos] = append(buckets[le.entry.canonicalPos], le)
	}
	for p := range numSlots {
		for _, le := range buckets[p] {
			if s := makeStage(le.entry, le.level, le.group); s != nil {
				stages = append(stages, s)
			}
		}
	}
	return stages, optOuts
}

func sumLen(slices [][]levelEntry) int {
	n := 0
	for _, s := range slices {
		n += len(s)
	}
	return n
}

// makeStage constructs the right concrete Stage for a surviving level
// entry. Returns nil if the statement type is not recognized (which
// should not happen for entries produced by classifyStmt).
func makeStage(entry levelEntry, level SourceLevel, group *ast.GroupBlock) Stage {
	switch s := entry.stmt.(type) {
	case *ast.LogStmt:
		return newLogStage(s, level, group)
	case *ast.MeasureStmt:
		return newMeasureStage(s, level, group)
	case *ast.SessionStmt:
		return newSessionStage(s, level, group)
	case *ast.CSRFStmt:
		return newCSRFStage(s, level, group)
	case *ast.LimitStmt:
		return newLimitStage(s, level, group)
	case *ast.ApproveStmt:
		return newApproveStage(s, level, group)
	case *ast.ResolveStmt:
		return newResolveStage(s, level, group)
	case *ast.CommitStmt:
		return newCommitStage(s, level, group)
	case *ast.EmitStmt:
		return newEmitStage(s, level, group)
	case *ast.LayoutStmt:
		return newLayoutStage(s, level, group)
	case *ast.FormatStmt:
		return newFormatStage(s, level, group)
	case *ast.RedirectStmt:
		return newRedirectStage(s, level, group)
	}
	return nil
}
