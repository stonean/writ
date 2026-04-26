package pipeline

import "testing"

func TestStageKindCanonicalPosition(t *testing.T) {
	cases := []struct {
		kind StageKind
		want int
	}{
		{StageLog, 0},
		{StageMeasure, 0},
		{StageSession, 1},
		{StageCSRF, 2},
		{StageLimit, 3},
		{StageApprove, 4},
		{StageResolve, 5},
		{StageCommit, 6},
		{StageEmit, 7},
		{StageLayout, 8},
		{StageFormat, 9},
		{StageRedirect, 9},
	}
	for _, c := range cases {
		if got := c.kind.CanonicalPosition(); got != c.want {
			t.Errorf("%s.CanonicalPosition() = %d, want %d", c.kind, got, c.want)
		}
	}
}

func TestStageKindIsObservational(t *testing.T) {
	for _, kind := range allKinds() {
		want := kind == StageLog || kind == StageMeasure
		if got := kind.IsObservational(); got != want {
			t.Errorf("%s.IsObservational() = %v, want %v", kind, got, want)
		}
	}
}

func TestStageKindIsSingleInstance(t *testing.T) {
	singletons := map[StageKind]bool{
		StageSession: true,
		StageCSRF:    true,
		StageLimit:   true,
		StageApprove: true,
		StageLayout:  true,
	}
	for _, kind := range allKinds() {
		want := singletons[kind]
		if got := kind.IsSingleInstance(); got != want {
			t.Errorf("%s.IsSingleInstance() = %v, want %v", kind, got, want)
		}
	}
}

func TestStageKindIsMultiInstance(t *testing.T) {
	multis := map[StageKind]bool{
		StageResolve: true,
		StageCommit:  true,
		StageEmit:    true,
	}
	for _, kind := range allKinds() {
		want := multis[kind]
		if got := kind.IsMultiInstance(); got != want {
			t.Errorf("%s.IsMultiInstance() = %v, want %v", kind, got, want)
		}
	}
}

func TestStageKindIsTerminal(t *testing.T) {
	terminals := map[StageKind]bool{
		StageFormat:   true,
		StageRedirect: true,
	}
	for _, kind := range allKinds() {
		want := terminals[kind]
		if got := kind.IsTerminal(); got != want {
			t.Errorf("%s.IsTerminal() = %v, want %v", kind, got, want)
		}
	}
}

func TestStageKindString(t *testing.T) {
	cases := []struct {
		kind StageKind
		want string
	}{
		{StageLog, "log"},
		{StageMeasure, "measure"},
		{StageSession, "session"},
		{StageCSRF, "csrf"},
		{StageLimit, "limit"},
		{StageApprove, "approve"},
		{StageResolve, "resolve"},
		{StageCommit, "commit"},
		{StageEmit, "emit"},
		{StageLayout, "layout"},
		{StageFormat, "format"},
		{StageRedirect, "redirect"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("StageKind(%d).String() = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestStageKindClassificationDisjoint(t *testing.T) {
	for _, kind := range allKinds() {
		categories := 0
		if kind.IsObservational() {
			categories++
		}
		if kind.IsSingleInstance() {
			categories++
		}
		if kind.IsMultiInstance() {
			categories++
		}
		if kind.IsTerminal() {
			categories++
		}
		if categories != 1 {
			t.Errorf("%s belongs to %d categories, want exactly 1", kind, categories)
		}
	}
}

func TestSourceLevelString(t *testing.T) {
	cases := []struct {
		level SourceLevel
		want  string
	}{
		{SourceSystem, "system"},
		{SourceGroup, "group"},
		{SourceHandler, "handler"},
	}
	for _, c := range cases {
		if got := c.level.String(); got != c.want {
			t.Errorf("SourceLevel(%d).String() = %q, want %q", c.level, got, c.want)
		}
	}
}

func allKinds() []StageKind {
	return []StageKind{
		StageLog, StageMeasure,
		StageSession, StageCSRF, StageLimit, StageApprove,
		StageResolve, StageCommit, StageEmit,
		StageLayout, StageFormat, StageRedirect,
	}
}
