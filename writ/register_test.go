package writ

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func okResolver(context.Context, Params) (any, error)                 { return nil, nil }
func okFormatter(context.Context, http.ResponseWriter, Results) error { return nil }

func TestResolverRegistersUniqueName(t *testing.T) {
	w := New()
	if err := w.Resolver("db.users", okResolver); err != nil {
		t.Fatalf("Resolver returned error on first registration: %v", err)
	}
}

func TestResolverDuplicateNameReturnsError(t *testing.T) {
	w := New()
	if err := w.Resolver("db.users", okResolver); err != nil {
		t.Fatalf("first Resolver returned error: %v", err)
	}
	err := w.Resolver("db.users", okResolver)
	if err == nil {
		t.Fatalf("second Resolver under same name returned nil; want error")
	}
	if !strings.Contains(err.Error(), "db.users") {
		t.Errorf("error message %q does not mention the duplicate name", err.Error())
	}
}

func TestFormatterRegistersUniqueName(t *testing.T) {
	w := New()
	if err := w.Formatter("users.list", okFormatter); err != nil {
		t.Fatalf("Formatter returned error on first registration: %v", err)
	}
}

func TestFormatterDuplicateNameReturnsError(t *testing.T) {
	w := New()
	if err := w.Formatter("users.list", okFormatter); err != nil {
		t.Fatalf("first Formatter returned error: %v", err)
	}
	err := w.Formatter("users.list", okFormatter)
	if err == nil {
		t.Fatalf("second Formatter under same name returned nil; want error")
	}
	if !strings.Contains(err.Error(), "users.list") {
		t.Errorf("error message %q does not mention the duplicate name", err.Error())
	}
}

func TestResolverPanicsAfterLoad(t *testing.T) {
	w := New()
	w.state.Store(stateLoaded)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Resolver did not panic after Load; want panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("recover returned non-string %T", r)
		}
		if !strings.Contains(msg, "Resolver") {
			t.Errorf("panic message %q does not mention the registration call", msg)
		}
	}()

	_ = w.Resolver("db.users", okResolver)
}

func TestFormatterPanicsAfterLoad(t *testing.T) {
	w := New()
	w.state.Store(stateLoaded)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Formatter did not panic after Load; want panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("recover returned non-string %T", r)
		}
		if !strings.Contains(msg, "Formatter") {
			t.Errorf("panic message %q does not mention the registration call", msg)
		}
	}()

	_ = w.Formatter("users.list", okFormatter)
}

func TestRegistrationDuringLoadingPanics(t *testing.T) {
	w := New()
	w.state.Store(stateLoading)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("Resolver did not panic during stateLoading; want panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "Load is in progress") {
			t.Errorf("panic message %q does not name the in-progress Load", msg)
		}
	}()

	_ = w.Resolver("db.users", okResolver)
}
