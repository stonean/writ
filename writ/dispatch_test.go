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
	defer func() { _ = resp.Body.Close() }()

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

// ---- errors-block dispatch tests (spec 004) ----

// dispatchNotFoundErr is a typed error used by the errors-block
// dispatch tests. It implements StatusCode() so the runtime resolves
// to 404; tests assert that registered error formatters receive that
// status via ErrorData.Status().
type dispatchNotFoundErr struct{ key string }

func (dispatchNotFoundErr) Error() string   { return "not found" }
func (dispatchNotFoundErr) StatusCode() int { return http.StatusNotFound }

// dispatchValidationErr is a typed error without StatusCode(); the
// runtime defaults to 500 when the errors-block formatter does not
// override it.
type dispatchValidationErr struct{ field string }

func (dispatchValidationErr) Error() string { return "invalid" }

func TestDispatchErrorsBlockConcreteTypeMatchInvokesFormatter(t *testing.T) {
	src := `errors /users/* ->
  NotFound notFoundJSON

GET /users/:id ->
  resolve user = thing.lookup(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, p Params) (any, error) {
			return nil, dispatchNotFoundErr{key: p.String("id")}
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			t.Fatalf("success formatter should not run when resolver errored")
			return nil
		}))
		mustRegister(t, w.ErrorFormatter("notFoundJSON", func(_ context.Context, rw http.ResponseWriter, data ErrorData) error {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(data.Status())
			_, _ = fmt.Fprintf(rw, `{"err":%q,"status":%d}`, data.Err().Error(), data.Status())
			return nil
		}))
		mustRegister(t, ErrorType[dispatchNotFoundErr](w, "NotFound"))
	})

	resp, body := getBody(t, srv.URL+"/users/42")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (formatter wrote ErrorData.Status())", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	want := `{"err":"not found","status":404}`
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestDispatchErrorsBlockDefaultMatchInvokesFormatter(t *testing.T) {
	src := `errors /users/* ->
  default fallbackJSON

GET /users/:id ->
  resolve user = thing.lookup(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, _ Params) (any, error) {
			return nil, errors.New("anything goes")
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			return nil
		}))
		mustRegister(t, w.ErrorFormatter("fallbackJSON", func(_ context.Context, rw http.ResponseWriter, data ErrorData) error {
			rw.WriteHeader(data.Status())
			_, _ = fmt.Fprintf(rw, "default:%s", data.Err().Error())
			return nil
		}))
	})

	resp, body := getBody(t, srv.URL+"/users/42")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 (no StatusCode on plain error)", resp.StatusCode)
	}
	if body != "default:anything goes" {
		t.Errorf("body = %q, want %q", body, "default:anything goes")
	}
}

func TestDispatchErrorsBlockConcreteWinsOverDefault(t *testing.T) {
	src := `errors /users/* ->
  NotFound notFoundJSON
  default fallbackJSON

GET /users/:id ->
  resolve user = thing.lookup(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, _ Params) (any, error) {
			return nil, dispatchNotFoundErr{key: "x"}
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			return nil
		}))
		mustRegister(t, w.ErrorFormatter("notFoundJSON", func(_ context.Context, rw http.ResponseWriter, _ ErrorData) error {
			_, _ = rw.Write([]byte("concrete"))
			return nil
		}))
		mustRegister(t, w.ErrorFormatter("fallbackJSON", func(_ context.Context, rw http.ResponseWriter, _ ErrorData) error {
			_, _ = rw.Write([]byte("default"))
			return nil
		}))
		mustRegister(t, ErrorType[dispatchNotFoundErr](w, "NotFound"))
	})

	_, body := getBody(t, srv.URL+"/users/42")
	if body != "concrete" {
		t.Errorf("body = %q, want %q (concrete-type entry must win over default)", body, "concrete")
	}
}

func TestDispatchErrorsBlockNoMatchWithStatusCodeWritesPlainText(t *testing.T) {
	// No errors block — the runtime falls through to the Q3 plain-text
	// fallback because the error implements StatusCode().
	src := `GET /users/:id ->
  resolve user = thing.lookup(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, _ Params) (any, error) {
			return nil, dispatchNotFoundErr{key: "x"}
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			return nil
		}))
	})

	resp, body := getBody(t, srv.URL+"/users/42")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (StatusCode() honored on no-match path)", resp.StatusCode)
	}
	if body != "404 Not Found\n" {
		t.Errorf("body = %q, want %q", body, "404 Not Found\n")
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
	}
}

func TestDispatchErrorsBlockNoMatchWithoutStatusCodeWrites500(t *testing.T) {
	// No errors block — the runtime falls through to spec-003's
	// generic 500 because the error has no StatusCode().
	src := `GET /users/:id ->
  resolve user = thing.lookup(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, _ Params) (any, error) {
			return nil, dispatchValidationErr{field: "x"}
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			return nil
		}))
	})

	resp, body := getBody(t, srv.URL+"/users/42")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if body != "500 Internal Server Error\n" {
		t.Errorf("body = %q, want spec-003 generic 500", body)
	}
}

func TestDispatchErrorFormatterPreWriteErrorWrites500(t *testing.T) {
	src := `errors /users/* ->
  default fallbackJSON

GET /users/:id ->
  resolve user = thing.lookup(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, _ Params) (any, error) {
			return nil, errors.New("boom")
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			return nil
		}))
		mustRegister(t, w.ErrorFormatter("fallbackJSON", func(_ context.Context, _ http.ResponseWriter, _ ErrorData) error {
			return errors.New("formatter exploded before writing")
		}))
	})

	resp, body := getBody(t, srv.URL+"/users/42")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if body != "500 Internal Server Error\n" {
		t.Errorf("body = %q, want generic 500", body)
	}
}

func TestDispatchErrorFormatterPostWriteErrorPreservesPartialResponse(t *testing.T) {
	src := `errors /users/* ->
  default fallbackJSON

GET /users/:id ->
  resolve user = thing.lookup(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, _ Params) (any, error) {
			return nil, errors.New("boom")
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			return nil
		}))
		mustRegister(t, w.ErrorFormatter("fallbackJSON", func(_ context.Context, rw http.ResponseWriter, _ ErrorData) error {
			rw.WriteHeader(http.StatusTeapot)
			_, _ = rw.Write([]byte("partial err body"))
			return errors.New("late failure")
		}))
	})

	resp, body := getBody(t, srv.URL+"/users/42")
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418 (partial response preserved)", resp.StatusCode)
	}
	if body != "partial err body" {
		t.Errorf("body = %q, want %q", body, "partial err body")
	}
}

func TestDispatchErrorDataRequestReturnsOriginatingRequest(t *testing.T) {
	src := `errors /users/* ->
  default echoJSON

GET /users/:id ->
  resolve user = thing.lookup(:id)
  format user.show with user
`
	srv := loadTestRuntime(t, src, func(w *Writ) {
		mustRegister(t, w.Resolver("thing.lookup", func(_ context.Context, _ Params) (any, error) {
			return nil, errors.New("boom")
		}))
		mustRegister(t, w.Formatter("user.show", func(_ context.Context, _ http.ResponseWriter, _ Results) error {
			return nil
		}))
		mustRegister(t, w.ErrorFormatter("echoJSON", func(_ context.Context, rw http.ResponseWriter, data ErrorData) error {
			req := data.Request()
			if req == nil {
				t.Fatalf("ErrorData.Request() returned nil")
			}
			rw.Header().Set("X-Trace", req.Method+" "+req.URL.Path)
			rw.WriteHeader(data.Status())
			_, _ = rw.Write([]byte("ok"))
			return nil
		}))
	})

	resp, _ := getBody(t, srv.URL+"/users/42")
	if got := resp.Header.Get("X-Trace"); got != "GET /users/42" {
		t.Errorf("X-Trace = %q, want %q", got, "GET /users/42")
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
	_ = resp.Body.Close()
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
	_ = resp.Body.Close()
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
