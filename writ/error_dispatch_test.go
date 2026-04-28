package writ

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type statusErr struct {
	code int
	msg  string
}

func (s statusErr) Error() string   { return s.msg }
func (s statusErr) StatusCode() int { return s.code }

func TestResolveStatusOnTypedError(t *testing.T) {
	got, ok := resolveStatus(statusErr{code: 404, msg: "missing"})
	if !ok {
		t.Errorf("hasStatusCode = false, want true for type implementing StatusCode")
	}
	if got != 404 {
		t.Errorf("status = %d, want 404", got)
	}
}

func TestResolveStatusOnPlainError(t *testing.T) {
	got, ok := resolveStatus(errors.New("plain"))
	if ok {
		t.Errorf("hasStatusCode = true, want false for plain error")
	}
	if got != 500 {
		t.Errorf("status = %d, want 500 (default)", got)
	}
}

func TestResolveStatusOnNilDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("resolveStatus(nil) panicked: %v", r)
		}
	}()
	got, ok := resolveStatus(nil)
	if ok {
		t.Errorf("hasStatusCode = true, want false on nil error")
	}
	if got != 500 {
		t.Errorf("status = %d, want 500", got)
	}
}

func TestWriteStatusTextKnownStatuses(t *testing.T) {
	cases := []struct {
		status   int
		wantBody string
	}{
		{200, "200 OK\n"},
		{404, "404 Not Found\n"},
		{422, "422 Unprocessable Entity\n"},
		{500, "500 Internal Server Error\n"},
		{503, "503 Service Unavailable\n"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeStatusText(rec, tc.status)
		if got := rec.Code; got != tc.status {
			t.Errorf("status %d: code = %d, want %d", tc.status, got, tc.status)
		}
		if got := rec.Body.String(); got != tc.wantBody {
			t.Errorf("status %d: body = %q, want %q", tc.status, got, tc.wantBody)
		}
		if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("status %d: Content-Type = %q, want text/plain; charset=utf-8", tc.status, got)
		}
	}
}

func TestWriteStatusTextUnknownStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStatusText(rec, 599)
	if rec.Code != 599 {
		t.Errorf("code = %d, want 599", rec.Code)
	}
	if !strings.HasPrefix(rec.Body.String(), "599 ") {
		t.Errorf("body = %q, want to start with %q", rec.Body.String(), "599 ")
	}
	if !strings.HasSuffix(rec.Body.String(), "\n") {
		t.Errorf("body = %q, want trailing newline", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Error") {
		t.Errorf("body = %q, want fallback %q reason phrase", rec.Body.String(), "Error")
	}
}
