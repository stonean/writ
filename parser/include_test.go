package parser

import (
	"testing"
	"testing/fstest"
)

func TestIncludeFlattensIntoSingleProgram(t *testing.T) {
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`system ->
  log :id
include admin.writ
GET /home ->
  format home.html with user
`)},
		"admin.writ": &fstest.MapFile{Data: []byte(`group /admin/* ->
  approve auth.isAdmin
`)},
	}
	prog, errs := Parse("app.writ", WithFS(fsys))
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if prog.System == nil {
		t.Fatalf("expected system block from app.writ")
	}
	if len(prog.Groups) != 1 {
		t.Fatalf("groups = %d, want 1 (from admin.writ)", len(prog.Groups))
	}
	if len(prog.Handlers) != 1 {
		t.Fatalf("handlers = %d, want 1", len(prog.Handlers))
	}
	if len(prog.Sources) != 2 {
		t.Fatalf("sources = %d, want 2 (root + admin.writ)", len(prog.Sources))
	}
	if prog.Sources[0].Path != "app.writ" {
		t.Errorf("sources[0].Path = %q, want app.writ", prog.Sources[0].Path)
	}
	if prog.Sources[1].Path != "admin.writ" {
		t.Errorf("sources[1].Path = %q, want admin.writ", prog.Sources[1].Path)
	}
}

func TestIncludeSpansReferenceOriginatingSource(t *testing.T) {
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`system ->
  log :id
include admin.writ
`)},
		"admin.writ": &fstest.MapFile{Data: []byte(`group /admin/* ->
  approve auth.isAdmin
`)},
	}
	prog, errs := Parse("app.writ", WithFS(fsys))
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(prog.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(prog.Groups))
	}
	groupSpan := prog.Groups[0].Span()
	if groupSpan.Start.Source == nil {
		t.Fatalf("group span has nil source")
	}
	if groupSpan.Start.Source.Path != "admin.writ" {
		t.Errorf("group span source = %q, want admin.writ", groupSpan.Start.Source.Path)
	}
	if groupSpan.Start.Line != 1 {
		t.Errorf("group span starts at line %d, want 1 (within admin.writ)", groupSpan.Start.Line)
	}
}

func TestIncludeCycleIsReported(t *testing.T) {
	fsys := fstest.MapFS{
		"a.writ": &fstest.MapFile{Data: []byte(`include b.writ
GET /a ->
  log :id
`)},
		"b.writ": &fstest.MapFile{Data: []byte(`include a.writ
GET /b ->
  log :id
`)},
	}
	_, errs := Parse("a.writ", WithFS(fsys))
	if len(errs) == 0 {
		t.Fatalf("expected include-cycle error, got none")
	}
	found := false
	for _, e := range errs {
		if contains(e.Message, "include cycle") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no include-cycle error in: %v", errs)
	}
}

func TestIncludeMissingFileIsReported(t *testing.T) {
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`include nope.writ
GET /home ->
  log :id
`)},
	}
	_, errs := Parse("app.writ", WithFS(fsys))
	if len(errs) == 0 {
		t.Fatalf("expected missing-file error")
	}
	found := false
	for _, e := range errs {
		if contains(e.Message, "file not found") || contains(e.Message, "cannot read") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no missing-file error in: %v", errs)
	}
}

func TestIncludeSystemBlockInsideIsRejected(t *testing.T) {
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`include lib.writ
GET /home ->
  log :id
`)},
		"lib.writ": &fstest.MapFile{Data: []byte(`system ->
  log :id
`)},
	}
	_, errs := Parse("app.writ", WithFS(fsys))
	if len(errs) == 0 {
		t.Fatalf("expected system-in-include error")
	}
	found := false
	for _, e := range errs {
		if contains(e.Message, "system block is not allowed in an included file") {
			found = true
			if e.File != "lib.writ" {
				t.Errorf("error file = %q, want lib.writ", e.File)
			}
			break
		}
	}
	if !found {
		t.Fatalf("no system-in-include error in: %v", errs)
	}
}

func TestIncludePathExtensionEnforced(t *testing.T) {
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`include admin.txt
GET /home ->
  log :id
`)},
		"admin.txt": &fstest.MapFile{Data: []byte(`group /admin/* ->
  log :id
`)},
	}
	_, errs := Parse("app.writ", WithFS(fsys))
	if len(errs) == 0 {
		t.Fatalf("expected extension error")
	}
	found := false
	for _, e := range errs {
		if contains(e.Message, `must end in ".writ"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no extension error in: %v", errs)
	}
}

func TestIncludePathsAreRelativeToCurrentFile(t *testing.T) {
	// app.writ includes subdir/foo.writ; foo.writ includes bar.writ
	// (which means subdir/bar.writ, not bar.writ at the root).
	fsys := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`include subdir/foo.writ
GET /home ->
  log :id
`)},
		"subdir/foo.writ": &fstest.MapFile{Data: []byte(`include bar.writ
group /foo ->
  log :id
`)},
		"subdir/bar.writ": &fstest.MapFile{Data: []byte(`group /bar ->
  log :id
`)},
	}
	prog, errs := Parse("app.writ", WithFS(fsys))
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(prog.Groups) != 2 {
		t.Fatalf("groups = %d, want 2 (foo + bar)", len(prog.Groups))
	}
	if len(prog.Sources) != 3 {
		t.Fatalf("sources = %d, want 3 (root + foo + bar)", len(prog.Sources))
	}
}

func TestIncludeProgramFlattenedEquivalentToSingleFile(t *testing.T) {
	combined := `system ->
  log :id
group /admin/* ->
  approve auth.isAdmin
GET /home ->
  log :id
`
	split := fstest.MapFS{
		"app.writ": &fstest.MapFile{Data: []byte(`system ->
  log :id
include admin.writ
GET /home ->
  log :id
`)},
		"admin.writ": &fstest.MapFile{Data: []byte(`group /admin/* ->
  approve auth.isAdmin
`)},
	}
	a, errsA := ParseString("combined.writ", combined)
	b, errsB := Parse("app.writ", WithFS(split))
	if len(errsA) > 0 || len(errsB) > 0 {
		t.Fatalf("unexpected errors: combined=%v split=%v", errsA, errsB)
	}
	if (a.System != nil) != (b.System != nil) {
		t.Errorf("system presence differs (combined=%v split=%v)", a.System != nil, b.System != nil)
	}
	if len(a.Groups) != len(b.Groups) {
		t.Errorf("groups: combined=%d split=%d", len(a.Groups), len(b.Groups))
	}
	if len(a.Handlers) != len(b.Handlers) {
		t.Errorf("handlers: combined=%d split=%d", len(a.Handlers), len(b.Handlers))
	}
}
