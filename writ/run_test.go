package writ

import (
	"errors"
	"testing"
)

func TestResolvePortDefaultsTo8080(t *testing.T) {
	t.Setenv("PORT", "")
	if got := resolvePort(); got != "8080" {
		t.Errorf("resolvePort with empty PORT = %q, want 8080", got)
	}
}

func TestResolvePortReadsEnvVar(t *testing.T) {
	t.Setenv("PORT", "9090")
	if got := resolvePort(); got != "9090" {
		t.Errorf("resolvePort = %q, want 9090", got)
	}
}

func TestDefaultPortConstant(t *testing.T) {
	if defaultPort != "8080" {
		t.Errorf("defaultPort = %q, want 8080 (system.md reserved-var default)", defaultPort)
	}
}

// TestRunReturnsLoadError verifies that Run does not bind a listener
// when Load fails. This is the only path the runtime can exercise
// deterministically without spinning up a goroutine to listen on a
// port — the happy path of Run is exercised end-to-end by tests
// that compose Load + http.Handler with httptest.NewServer in
// dispatch_test.go.
func TestRunReturnsLoadError(t *testing.T) {
	w := New()
	err := w.Run("/no/such/file.writ")
	if err == nil {
		t.Fatalf("Run on missing file returned nil; want Load error")
	}
	// Load on a missing file returns a non-*Error (the parser's own
	// "cannot read" message). Verify Run propagates it unchanged.
	var werr *Error
	if errors.As(err, &werr) {
		// If we got an *Error, it should at least carry parse-failure
		// entries (the parser surfaces missing-file as a parse error
		// per spec 001).
		if len(werr.Entries) == 0 {
			t.Errorf("Run returned an empty *Error aggregate")
		}
	}
	// State must remain stateInit so subsequent Run/Load calls
	// don't see a half-initialized runtime.
	if w.state.Load() != stateInit {
		t.Errorf("state after Run failure = %d, want stateInit", w.state.Load())
	}
}
