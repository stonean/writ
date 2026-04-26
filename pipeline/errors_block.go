package pipeline

import (
	"fmt"

	"github.com/stonean/writ/ast"
)

// buildErrorMap produces a handler's effective error map by layering
// every `errors` block whose route pattern contains the handler's
// route. The layering rule (per spec 002 *Errors Block Selection*):
//
//   - Each Go error type name is resolved to the most-specific block's
//     entry that declares it.
//   - Less-specific blocks fill in error type names that more-specific
//     blocks do not declare.
//
// Ambiguous errors-block membership (two or more matching blocks
// overlap without containment) is reported as a single
// AmbiguousErrorsBlock error whose Span points at the handler block
// and whose Spans lists every conflicting block. Conflicting blocks
// are skipped; the effective map is built from the remaining clean
// containment chain.
func buildErrorMap(handler *ast.HandlerBlock, blocks []*ast.ErrorsBlock) (entries []ErrorMapEntry, errs []Error) {
	if handler == nil || handler.Pattern == nil || len(blocks) == 0 {
		return nil, nil
	}

	patternOf := func(b *ast.ErrorsBlock) *ast.RoutePattern { return b.Pattern }
	kept, conflicting := findKept(blocks, patternOf, handler.Pattern)

	if len(conflicting) > 0 {
		spans := make([]ast.Span, 0, len(conflicting))
		for _, c := range conflicting {
			spans = append(spans, c.Span())
		}
		errs = append(errs, Error{
			File:    handler.Span().Start.Source.Path,
			Line:    handler.Span().Start.Line,
			Column:  handler.Span().Start.Column,
			Span:    handler.Span(),
			Spans:   spans,
			Kind:    AmbiguousErrorsBlock,
			Message: fmt.Sprintf("handler matches %d errors blocks whose patterns overlap without containment", len(conflicting)),
		})
	}

	if len(kept) == 0 {
		return nil, errs
	}

	declIdx := make(map[*ast.ErrorsBlock]int, len(blocks))
	for i, b := range blocks {
		declIdx[b] = i
	}
	sortBySpecificity(kept, patternOf, func(b *ast.ErrorsBlock) int { return declIdx[b] })

	// Walk kept from most-specific to least-specific. For each
	// TypeName, the first occurrence wins; entries from less-specific
	// blocks fill in any TypeNames not yet seen.
	seen := make(map[string]struct{})
	for i := len(kept) - 1; i >= 0; i-- {
		block := kept[i]
		for _, entry := range block.Entries {
			if entry == nil {
				continue
			}
			if _, ok := seen[entry.TypeName]; ok {
				continue
			}
			seen[entry.TypeName] = struct{}{}
			entries = append(entries, ErrorMapEntry{
				TypeName:      entry.TypeName,
				TypeSpan:      entry.TypeSpan,
				Formatter:     entry.Formatter,
				FormatterSpan: entry.FormatterSpan,
				IsDefault:     entry.IsDefault,
				SourceBlock:   block,
			})
		}
	}
	return entries, errs
}
