package writ

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSmokeFixture round-trips the testdata/smoke.writ fixture
// through Load + httptest.NewServer and exercises every routing and
// dispatch outcome the runtime can produce: a 200 with parameter
// binding, a 200 with zero resolves, a 405 with sorted Allow, a 404,
// and a 500 produced by a deliberately failing resolver.
func TestSmokeFixture(t *testing.T) {
	w := New()
	mustRegister(t, w.Resolver("db.users", func(_ context.Context, p Params) (any, error) {
		id := p.String("id")
		if id == "boom" {
			return nil, errors.New("simulated resolver failure")
		}
		return map[string]string{"id": id, "name": "Alice"}, nil
	}))
	mustRegister(t, w.Formatter("users.list", func(_ context.Context, rw http.ResponseWriter, _ Results) error {
		_, _ = rw.Write([]byte(`["users"]`))
		return nil
	}))
	mustRegister(t, w.Formatter("users.show", func(_ context.Context, rw http.ResponseWriter, data Results) error {
		user := data.Get("user").(map[string]string)
		_, _ = fmt.Fprintf(rw, `{"id":%q,"name":%q}`, user["id"], user["name"])
		return nil
	}))

	if err := w.Load("testdata/smoke.writ"); err != nil {
		t.Fatalf("Load smoke.writ: %v", err)
	}
	srv := httptest.NewServer(w.Handler())
	t.Cleanup(srv.Close)

	t.Run("200 with zero resolves", func(t *testing.T) {
		resp, body := getBody(t, srv.URL+"/users")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if body != `["users"]` {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("200 with parameter binding", func(t *testing.T) {
		resp, body := getBody(t, srv.URL+"/users/42")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		want := `{"id":"42","name":"Alice"}`
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})

	t.Run("405 method not allowed with Allow header", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/users", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", resp.StatusCode)
		}
		if got := resp.Header.Get("Allow"); got != "GET" {
			t.Errorf("Allow = %q, want GET", got)
		}
	})

	t.Run("404 unknown path", func(t *testing.T) {
		resp, _ := getBody(t, srv.URL+"/nope")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("500 from deliberate resolver failure", func(t *testing.T) {
		resp, body := getBody(t, srv.URL+"/users/boom")
		if resp.StatusCode != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", resp.StatusCode)
		}
		if got := resp.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
		}
		if body != "500 Internal Server Error\n" {
			t.Errorf("body = %q", body)
		}
	})
}
