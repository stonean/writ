package writ

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// smokeNotFoundErr is the typed error matched by the errors_smoke.writ
// fixture's NotFound entry. It implements StatusCode() so the runtime
// passes 404 to the formatter as ErrorData.Status().
type smokeNotFoundErr struct{ id string }

func (e smokeNotFoundErr) Error() string { return "not found: " + e.id }
func (smokeNotFoundErr) StatusCode() int { return http.StatusNotFound }

// smokeRetryErr has StatusCode() but is not registered as a typed
// error; the dispatcher must fall through to the Q3 plain-text
// fallback for the /ping handler (which has no errors block).
type smokeRetryErr struct{}

func (smokeRetryErr) Error() string   { return "retry" }
func (smokeRetryErr) StatusCode() int { return http.StatusServiceUnavailable }

// TestErrorsSmokeFixture round-trips testdata/errors_smoke.writ
// through Load + httptest.NewServer and exercises every dispatch
// outcome the spec-004 errors-block runtime can produce: a
// concrete-type formatter match, a default formatter match, the Q3
// plain-text fallback when a handler has no errors block but the
// returned error implements StatusCode, and the spec-003 generic 500
// when neither errors-block match nor StatusCode applies.
func TestErrorsSmokeFixture(t *testing.T) {
	w := New()
	mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, p Params) (any, error) {
		switch p.String("id") {
		case "missing":
			return nil, smokeNotFoundErr{id: "missing"}
		case "boom":
			return nil, errors.New("internal: kaboom")
		default:
			return map[string]string{"id": p.String("id")}, nil
		}
	}))
	mustRegister(t, w.Resolver("thing.ping", func(_ context.Context, _ Params) (any, error) {
		return nil, smokeRetryErr{}
	}))
	mustRegister(t, w.Formatter("widget.show", func(_ context.Context, rw http.ResponseWriter, data Results) error {
		widget := data.Get("widget").(map[string]string)
		_, _ = fmt.Fprintf(rw, `{"id":%q}`, widget["id"])
		return nil
	}))
	mustRegister(t, w.Formatter("pong", func(_ context.Context, rw http.ResponseWriter, _ Results) error {
		_, _ = rw.Write([]byte("pong"))
		return nil
	}))
	mustRegister(t, w.ErrorFormatter("notFoundJSON", func(_ context.Context, rw http.ResponseWriter, data ErrorData) error {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(data.Status())
		_, _ = fmt.Fprintf(rw, `{"err":%q,"status":%d}`, data.Err().Error(), data.Status())
		return nil
	}))
	mustRegister(t, w.ErrorFormatter("fallbackJSON", func(_ context.Context, rw http.ResponseWriter, data ErrorData) error {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(data.Status())
		_, _ = fmt.Fprintf(rw, `{"fallback":%q}`, data.Err().Error())
		return nil
	}))
	mustRegister(t, ErrorType[smokeNotFoundErr](w, "NotFound"))

	if err := w.Load("testdata/errors_smoke.writ"); err != nil {
		t.Fatalf("Load errors_smoke.writ: %v", err)
	}
	srv := httptest.NewServer(w.Handler())
	t.Cleanup(srv.Close)

	t.Run("typed match invokes registered formatter", func(t *testing.T) {
		resp, body := getBody(t, srv.URL+"/widgets/missing")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		want := `{"err":"not found: missing","status":404}`
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})

	t.Run("default match invokes fallback formatter", func(t *testing.T) {
		resp, body := getBody(t, srv.URL+"/widgets/boom")
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500 (no StatusCode on plain error)", resp.StatusCode)
		}
		want := `{"fallback":"internal: kaboom"}`
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})

	t.Run("no errors block falls back to Q3 plain-text status", func(t *testing.T) {
		resp, body := getBody(t, srv.URL+"/ping")
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
		}
		if body != "503 Service Unavailable\n" {
			t.Errorf("body = %q, want plain-text status line", body)
		}
	})

	t.Run("happy path still works", func(t *testing.T) {
		resp, body := getBody(t, srv.URL+"/widgets/42")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if body != `{"id":"42"}` {
			t.Errorf("body = %q", body)
		}
	})
}
