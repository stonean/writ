package pipeline

import (
	"sort"

	"github.com/stonean/writ/ast"
)

// findKept partitions candidates into kept and conflicting sets for a
// given handler pattern.
//
// A candidate is "matching" when its pattern contains the handler's
// pattern (every route the handler matches is also matched by the
// candidate). The kept set is the maximum subset of matching
// candidates that forms a containment chain (pairwise comparable).
// Outliers that break the chain are returned in conflicting.
//
// When no chain of length two or more survives — for example, two
// matching groups that overlap each other without containment — all
// matching candidates are conflicting and kept is empty, matching the
// spec's "the handler's resolved entry inherits only from the system
// block (skipping every conflicting group)" rule.
//
// Both returned slices preserve input declaration order.
func findKept[T any](
	candidates []T,
	patternOf func(T) *ast.RoutePattern,
	target *ast.RoutePattern,
) (kept, conflicting []T) {
	matching := make([]T, 0, len(candidates))
	for _, c := range candidates {
		if containsAll(target, patternOf(c)) {
			matching = append(matching, c)
		}
	}
	n := len(matching)
	if n == 0 {
		return nil, nil
	}

	active := make([]bool, n)
	for i := range active {
		active[i] = true
	}
	conflictsExisted := false

	for {
		conflictCount := make([]int, n)
		chainCount := make([]int, n)
		anyConflict := false
		for i := range n {
			if !active[i] {
				continue
			}
			for j := range n {
				if i == j || !active[j] {
					continue
				}
				pi, pj := patternOf(matching[i]), patternOf(matching[j])
				if !containsAll(pi, pj) && !containsAll(pj, pi) {
					conflictCount[i]++
					anyConflict = true
				} else {
					chainCount[i]++
				}
			}
		}
		if !anyConflict {
			break
		}
		conflictsExisted = true

		pick := -1
		for i := range n {
			if !active[i] || conflictCount[i] == 0 {
				continue
			}
			if pick == -1 {
				pick = i
				continue
			}
			switch {
			case conflictCount[i] > conflictCount[pick]:
				pick = i
			case conflictCount[i] == conflictCount[pick] && chainCount[i] < chainCount[pick]:
				pick = i
			case conflictCount[i] == conflictCount[pick] && chainCount[i] == chainCount[pick] && i > pick:
				pick = i
			}
		}
		active[pick] = false
	}

	activeCount := 0
	for _, a := range active {
		if a {
			activeCount++
		}
	}
	if conflictsExisted && activeCount < 2 {
		for i := range active {
			active[i] = false
		}
	}

	for i, m := range matching {
		if active[i] {
			kept = append(kept, m)
		} else {
			conflicting = append(conflicting, m)
		}
	}
	return kept, conflicting
}

// sortBySpecificity sorts the slice in place from least-specific to
// most-specific. Two candidates with equal patterns are ordered by
// declOrder (earlier declaration first) for a deterministic tiebreak.
//
// Precondition: the slice must form a containment chain (any two
// elements are comparable). findKept guarantees this for its kept
// return value.
func sortBySpecificity[T any](
	kept []T,
	patternOf func(T) *ast.RoutePattern,
	declOrder func(T) int,
) {
	sort.SliceStable(kept, func(i, j int) bool {
		pi, pj := patternOf(kept[i]), patternOf(kept[j])
		switch {
		case strictlyContains(pj, pi):
			return true
		case strictlyContains(pi, pj):
			return false
		default:
			return declOrder(kept[i]) < declOrder(kept[j])
		}
	})
}
