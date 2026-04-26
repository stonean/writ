package writ

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// loadTestRuntime parses src, registers the supplied resolvers and
// formatters, runs Load against a temp file, and returns a server
// + client + cleanup. Tests use this helper for end-to-end
// dispatch coverage.
func loadTestRuntime(t *testing.T, src string, register func(*Writ)) *httptest.Server {
	t.Helper()
	path := writeWritFile(t, src)
	w := New()
	if register != nil {
		register(w)
	}
	if err := w.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv := httptest.NewServer(w.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestHandlerPanicsBeforeLoad(t *testing.T) {
	w := New()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Handler did not panic before Load; want panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "Load") {
			t.Errorf("panic message %v does not name Load", r)
		}
	}()
	_ = w.Handler()
}

func TestDispatchHappyPath(t *testing.T) {
	src := `GET /users/:id ->
  resolve user = db.users(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("db.users", func(_ context.Context, p Params) (any, error) {
			return map[string]string{"id": p.String("id"), "name": "Alice"}, nil
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, rw http.ResponseWriter, data Results) error {
			user := data.Get("user").(map[string]string)
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(rw, `{"id":%q,"name":%q}`, user["id"], user["name"])
			return nil
		}))
	})

	resp, body := getBody(t, srv.URL+"/users/42")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	want := `{"id":"42","name":"Alice"}`
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestDispatchMultipleResolvesInOrder(t *testing.T) {
	src := `GET /users/:id ->
  resolve a = thing.first(:id)
  resolve b = thing.second(:id)
  format combine with a, b
`
	var callOrder []string
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.first", func(_ context.Context, _ Params) (any, error) {
			callOrder = append(callOrder, "first")
			return "A", nil
		}))
		mustRegister(t, w.Resolver("thing.second", func(_ context.Context, _ Params) (any, error) {
			callOrder = append(callOrder, "second")
			return "B", nil
		}))
		mustRegister(t, w.Formatter("combine", func(_ context.Context, rw http.ResponseWriter, data Results) error {
			callOrder = append(callOrder, "format")
			_, _ = fmt.Fprintf(rw, "%s+%s", data.Get("a"), data.Get("b"))
			return nil
		}))
	})

	_, body := getBody(t, srv.URL+"/users/42")
	if body != "A+B" {
		t.Errorf("body = %q, want %q", body, "A+B")
	}
	want := []string{"first", "second", "format"}
	if !equalStringSlices(callOrder, want) {
		t.Errorf("call order = %v, want %v", callOrder, want)
	}
}

func TestDispatchResolverErrorWritesGeneric500(t *testing.T) {
	src := `GET /users ->
  resolve out = thing.boom()
  format ok with out
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.boom", func(_ context.Context, _ Params) (any, error) {
			return nil, errors.New("internal: kaboom")
		}))
		mustRegister(t, w.Formatter("ok", func(_ context.Context, rw http.ResponseWriter, _ Results) error {
			t.Fatalf("formatter should not be invoked when a resolver errored")
			return nil
		}))
	})

	resp, body := getBody(t, srv.URL+"/users")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if !strings.Contains(body, "500") {
		t.Errorf("body = %q, want generic 500 text", body)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
	if strings.Contains(body, "kaboom") {
		t.Errorf("internal error detail leaked into body: %q", body)
	}
}

func TestDispatchFormatterErrorBeforeWriteWrites500(t *testing.T) {
	src := `GET /users ->
  format failer with x
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Formatter("failer", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			return errors.New("formatter exploded")
		}))
	})

	resp, _ := getBody(t, srv.URL+"/users")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestDispatchFormatterErrorAfterWritePreservesPartialResponse(t *testing.T) {
	src := `GET /users ->
  format partial with x
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Formatter("partial", func(_ context.Context, rw http.ResponseWriter, _ Results) error {
			rw.WriteHeader(http.StatusOK)
			_, _ = rw.Write([]byte("partial body"))
			return errors.New("late failure")
		}))
	})

	resp, body := getBody(t, srv.URL+"/users")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (formatter started writing before erroring)", resp.StatusCode)
	}
	if body != "partial body" {
		t.Errorf("body = %q, want %q", body, "partial body")
	}
}

func TestDispatchFormatterCustomStatusPreserved(t *testing.T) {
	src := `POST /users ->
  format created with x
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Formatter("created", func(_ context.Context, rw http.ResponseWriter, _ Results) error {
			rw.WriteHeader(http.StatusCreated)
			_, _ = rw.Write([]byte(`{"created":true}`))
			return nil
		}))
	})

	resp, _ := postBody(t, srv.URL+"/users")
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
}

func TestDispatchFormatterDefaultStatusIs200(t *testing.T) {
	src := `GET /ping ->
  format pong with x
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Formatter("pong", func(_ context.Context, rw http.ResponseWriter, _ Results) error {
			_, _ = rw.Write([]byte("pong"))
			return nil
		}))
	})

	resp, body := getBody(t, srv.URL+"/ping")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (default when formatter does not call WriteHeader)", resp.StatusCode)
	}
	if body != "pong" {
		t.Errorf("body = %q, want pong", body)
	}
}

func TestDispatchZeroResolveHandlerProducesEmptyResults(t *testing.T) {
	src := `GET /static ->
  format static
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Formatter("static", func(_ context.Context, rw http.ResponseWriter, data Results) error {
			if data.Has("anything") {
				t.Errorf("Results.Has on an empty handler should be false")
			}
			_, _ = rw.Write([]byte("static body"))
			return nil
		}))
	})

	_, body := getBody(t, srv.URL+"/static")
	if body != "static body" {
		t.Errorf("body = %q, want %q", body, "static body")
	}
}

func TestDispatchFormatterReceivesOnlyWithListedNames(t *testing.T) {
	src := `GET /users/:id ->
  resolve user = thing.user(:id)
  resolve secret = thing.secret()
  format show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.user", func(_ context.Context, _ Params) (any, error) {
			return "alice", nil
		}))
		mustRegister(t, w.Resolver("thing.secret", func(_ context.Context, _ Params) (any, error) {
			return "do not leak", nil
		}))
		mustRegister(t, w.Formatter("show", func(_ context.Context, rw http.ResponseWriter, data Results) error {
			if data.Has("secret") {
				t.Errorf("formatter saw `secret` even though it was not in the with-clause")
			}
			_, _ = fmt.Fprintf(rw, "user=%v", data.Get("user"))
			return nil
		}))
	})

	_, body := getBody(t, srv.URL+"/users/42")
	if body != "user=alice" {
		t.Errorf("body = %q, want %q", body, "user=alice")
	}
}

func TestDispatch404OnUnknownPath(t *testing.T) {
	src := `GET /users ->
  format ok with x
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Formatter("ok", func(_ context.Context, rw http.ResponseWriter, _ Results) error {
			_, _ = rw.Write([]byte("ok"))
			return nil
		}))
	})

	resp, _ := getBody(t, srv.URL+"/missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDispatch405WithSortedAllow(t *testing.T) {
	src := `GET /users/:id ->
  format ok with x

DELETE /users/:id ->
  format ok with x
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Formatter("ok", func(_ context.Context, rw http.ResponseWriter, _ Results) error {
			_, _ = rw.Write([]byte("ok"))
			return nil
		}))
	})

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/users/42", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
	if got := resp.Header.Get("Allow"); got != "DELETE, GET" {
		t.Errorf("Allow = %q, want %q", got, "DELETE, GET")
	}
}

func TestDispatchRequestIsolation(t *testing.T) {
	// Two concurrent requests to the same handler must not share
	// per-request state. The resolver writes the bound :id into a
	// channel; the formatter reads its own Results.Get("user") and
	// echoes it back. If state leaked we'd see one request's id in
	// the other's response.
	src := `GET /users/:id ->
  resolve user = thing.user(:id)
  format show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.user", func(_ context.Context, p Params) (any, error) {
			return p.String("id"), nil
		}))
		mustRegister(t, w.Formatter("show", func(_ context.Context, rw http.ResponseWriter, data Results) error {
			_, _ = fmt.Fprintf(rw, "id=%v", data.Get("user"))
			return nil
		}))
	})

	const N = 16
	var wg sync.WaitGroup
	wg.Add(N)
	results := make([]string, N)

	for i := range N {
		go func(i int) {
			defer wg.Done()
			_, body := getBody(t, fmt.Sprintf("%s/users/%d", srv.URL, i))
			results[i] = body
		}(i)
	}
	wg.Wait()

	for i, body := range results {
		want := fmt.Sprintf("id=%d", i)
		if body != want {
			t.Errorf("request %d body = %q, want %q (state leak across requests)", i, body, want)
		}
	}
}

// ---- test helpers ----

func mustRegister(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("register: %v", err)
	}
}

func getBody(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	resp.Body.Close()
	return resp, string(body)
}

func postBody(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	resp.Body.Close()
	return resp, string(body)
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
