package writ

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// writeWritFile writes contents to a temp file and returns the path.
// The file is removed when the test ends.
func writeWritFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "app.writ")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write writ file: %v", err)
	}
	return path
}

func TestLoadHappyPath(t *testing.T) {
	path := writeWritFile(t, `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`)
	w := New()
	if err := w.Resolver("db.users", noopResolver); err != nil {
		t.Fatalf("Resolver: %v", err)
	}
	if err := w.Formatter("user.show", noopFormatter); err != nil {
		t.Fatalf("Formatter: %v", err)
	}
	if err := w.Load(path); err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if w.state.Load() != stateLoaded {
		t.Errorf("state = %d, want stateLoaded", w.state.Load())
	}
	if w.table.Load() == nil {
		t.Errorf("table not installed")
	}
}

func TestLoadParseFailureRollsBack(t *testing.T) {
	path := writeWritFile(t, `GET /users/:id @@@ broken
`)
	w := New()

	err := w.Load(path)
	if err == nil {
		t.Fatalf("Load on broken syntax returned nil; want error")
	}
	var werr *Error
	if !errors.As(err, &werr) {
		t.Fatalf("err is not *Error: %T", err)
	}
	if len(werr.Entries) == 0 {
		t.Fatalf("aggregate Error has no entries")
	}
	for _, entry := range werr.Entries {
		if entry.Kind != KindParseFailure {
			t.Errorf("entry kind = %v, want all KindParseFailure", entry.Kind)
		}
	}
	if w.state.Load() != stateInit {
		t.Errorf("state after failure = %d, want stateInit (rolled back)", w.state.Load())
	}
	if w.table.Load() != nil {
		t.Errorf("table installed after failure; should be nil")
	}
}

func TestLoadElaborationFailureRollsBack(t *testing.T) {
	// `format` in a `system` block is a stage-placement error per
	// spec 002 (terminators are handler-only). The parser accepts
	// the syntax; the elaborator rejects it. Load should produce
	// KindElaborationFailure entries and roll back to stateInit.
	path := writeWritFile(t, `system ->
  format global.html with x

GET /users ->
  format users.list
`)
	w := New()

	err := w.Load(path)
	if err == nil {
		t.Fatalf("Load returned nil; want elaboration error")
	}
	var werr *Error
	if !errors.As(err, &werr) {
		t.Fatalf("err is not *Error: %T", err)
	}
	found := false
	for _, entry := range werr.Entries {
		if entry.Kind == KindElaborationFailure {
			found = true
		}
	}
	if !found {
		t.Errorf("entries = %v, want at least one KindElaborationFailure", werr.Entries)
	}
	if w.state.Load() != stateInit {
		t.Errorf("state after elaboration failure = %d, want stateInit", w.state.Load())
	}
}

func TestLoadElaborationFailureShortCircuitsBeforeValidation(t *testing.T) {
	// When elaboration fails, validation entries (e.g., unregistered
	// formatter) should not leak through — the loader short-circuits.
	path := writeWritFile(t, `system ->
  format global.html with x

GET /users ->
  format users.list
`)
	w := New()
	// Deliberately don't register `users.list`. If validation ran,
	// we'd see a KindUnregisteredFormatter entry; we should not.
	err := w.Load(path)
	var werr *Error
	if !errors.As(err, &werr) {
		t.Fatalf("err is not *Error")
	}
	for _, entry := range werr.Entries {
		if entry.Kind == KindUnregisteredFormatter {
			t.Errorf("validation entry leaked through elaboration short-circuit: %s", entry.Message)
		}
	}
}

func TestLoadParseFailureShortCircuitsBeforeElaboration(t *testing.T) {
	// Parse failure should produce only KindParseFailure entries —
	// no elaboration or validation entries should leak through.
	path := writeWritFile(t, `GET /users/:id @@@`)
	w := New()

	err := w.Load(path)
	var werr *Error
	if !errors.As(err, &werr) {
		t.Fatalf("err is not *Error")
	}
	for _, entry := range werr.Entries {
		switch entry.Kind {
		case KindParseFailure:
			// expected
		default:
			t.Errorf("unexpected entry kind %v after parse failure: %s", entry.Kind, entry.Message)
		}
	}
}

func TestLoadValidationFailureRollsBack(t *testing.T) {
	// Resolver `db.users` is referenced but not registered.
	path := writeWritFile(t, `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`)
	w := New()
	if err := w.Formatter("user.show", noopFormatter); err != nil {
		t.Fatalf("Formatter: %v", err)
	}

	err := w.Load(path)
	var werr *Error
	if !errors.As(err, &werr) {
		t.Fatalf("err is not *Error: %T", err)
	}
	found := false
	for _, entry := range werr.Entries {
		if entry.Kind == KindUnregisteredResolver {
			found = true
		}
	}
	if !found {
		t.Errorf("entries = %v, want a KindUnregisteredResolver", werr.Entries)
	}
	if w.state.Load() != stateInit {
		t.Errorf("state after validation failure = %d, want stateInit", w.state.Load())
	}
}

func TestLoadDoubleLoadReturnsAlreadyLoaded(t *testing.T) {
	path := writeWritFile(t, `GET /users ->
  format users.list with x
`)
	w := New()
	if err := w.Formatter("users.list", noopFormatter); err != nil {
		t.Fatalf("Formatter: %v", err)
	}
	if err := w.Load(path); err != nil {
		t.Fatalf("first Load: %v", err)
	}

	err := w.Load(path)
	if err == nil {
		t.Fatalf("second Load returned nil; want ErrAlreadyLoaded")
	}
	if !errors.Is(err, ErrAlreadyLoaded) {
		t.Errorf("second Load returned %v, want ErrAlreadyLoaded", err)
	}
}

func TestLoadConcurrentInProgressPanics(t *testing.T) {
	w := New()
	w.state.Store(stateLoading)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Load did not panic when stateLoading; want panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "Load") {
			t.Errorf("panic message %v does not name Load", r)
		}
	}()

	_ = w.Load("ignored.writ")
}

func TestLoadConcurrentRaceUsingGoroutines(t *testing.T) {
	// Drive a real concurrent race: many goroutines call Load on
	// the same instance, all racing on the CAS. Exactly one
	// goroutine wins and serves the load; the rest must observe
	// either a panic (state was stateLoading at CAS-fail time) or
	// ErrAlreadyLoaded (the winner finished first).
	//
	// Because the load is fast on a small in-memory file, a real
	// concurrent race typically produces ErrAlreadyLoaded for the
	// losers; the panic path is exercised deterministically by the
	// preceding test that pre-sets state to stateLoading.
	path := writeWritFile(t, `GET /users ->
  format users.list with x
`)
	w := New()
	if err := w.Formatter("users.list", noopFormatter); err != nil {
		t.Fatalf("Formatter: %v", err)
	}

	const goroutines = 8
	var (
		start    sync.WaitGroup
		done     sync.WaitGroup
		successN int
		failureN int
		mu       sync.Mutex
	)
	start.Add(1)
	done.Add(goroutines)

	for range goroutines {
		go func() {
			defer done.Done()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					failureN++
					mu.Unlock()
				}
			}()
			start.Wait()
			err := w.Load(path)
			mu.Lock()
			if err == nil {
				successN++
			} else {
				failureN++
			}
			mu.Unlock()
		}()
	}

	start.Done()
	done.Wait()

	if successN != 1 {
		t.Errorf("exactly one goroutine should succeed; got %d successes (and %d failures)", successN, failureN)
	}
	if successN+failureN != goroutines {
		t.Errorf("goroutines accounted for = %d, want %d", successN+failureN, goroutines)
	}
}

func TestLoadMissingFileFails(t *testing.T) {
	w := New()
	err := w.Load("/no/such/file.writ")
	if err == nil {
		t.Fatalf("Load missing file returned nil; want error")
	}
	if w.state.Load() != stateInit {
		t.Errorf("state after failure = %d, want stateInit", w.state.Load())
	}
}
