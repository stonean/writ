package writ

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type notFoundErr struct{ key string }

func (notFoundErr) Error() string { return "not found" }

type validationErr struct{}

func (validationErr) Error() string { return "invalid" }

func okErrorFormatter(context.Context, http.ResponseWriter, ErrorData) error { return nil }

func TestErrorFormatterRegistersUniqueName(t *testing.T) {
	w := New()
	if err := w.ErrorFormatter("notFoundJSON", okErrorFormatter); err != nil {
		t.Fatalf("ErrorFormatter returned error on first registration: %v", err)
	}
}

func TestErrorFormatterDuplicateNameReturnsError(t *testing.T) {
	w := New()
	if err := w.ErrorFormatter("notFoundJSON", okErrorFormatter); err != nil {
		t.Fatalf("first ErrorFormatter returned error: %v", err)
	}
	err := w.ErrorFormatter("notFoundJSON", okErrorFormatter)
	if err == nil {
		t.Fatalf("second ErrorFormatter under same name returned nil; want error")
	}
	if !strings.Contains(err.Error(), "notFoundJSON") {
		t.Errorf("error message %q does not mention duplicate name", err.Error())
	}
}

func TestErrorFormatterPanicsAfterLoad(t *testing.T) {
	w := New()
	w.state.Store(stateLoaded)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("ErrorFormatter did not panic after Load; want panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "ErrorFormatter") {
			t.Errorf("panic message %v does not name the registration call", r)
		}
	}()

	_ = w.ErrorFormatter("notFoundJSON", okErrorFormatter)
}

func TestErrorTypeRegistersUniqueName(t *testing.T) {
	w := New()
	if err := ErrorType[notFoundErr](w, "NotFound"); err != nil {
		t.Fatalf("ErrorType returned error on first registration: %v", err)
	}
}

func TestErrorTypeDuplicateNameReturnsError(t *testing.T) {
	w := New()
	if err := ErrorType[notFoundErr](w, "NotFound"); err != nil {
		t.Fatalf("first ErrorType returned error: %v", err)
	}
	err := ErrorType[validationErr](w, "NotFound")
	if err == nil {
		t.Fatalf("second ErrorType under same name returned nil; want error")
	}
	if !strings.Contains(err.Error(), "NotFound") {
		t.Errorf("error message %q does not mention duplicate name", err.Error())
	}
}

func TestErrorTypePanicsAfterLoad(t *testing.T) {
	w := New()
	w.state.Store(stateLoaded)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("ErrorType did not panic after Load; want panic")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "ErrorType") {
			t.Errorf("panic message %v does not name the registration call", r)
		}
	}()

	_ = ErrorType[notFoundErr](w, "NotFound")
}

func TestThreeNamespacesAreIndependent(t *testing.T) {
	w := New()
	const name = "shared"
	if err := w.Formatter(name, okFormatter); err != nil {
		t.Fatalf("Formatter: %v", err)
	}
	if err := w.ErrorFormatter(name, okErrorFormatter); err != nil {
		t.Fatalf("ErrorFormatter: %v", err)
	}
	if err := ErrorType[notFoundErr](w, name); err != nil {
		t.Fatalf("ErrorType: %v", err)
	}
}

func TestErrorTypeMatcherUsesErrorsAs(t *testing.T) {
	w := New()
	if err := ErrorType[notFoundErr](w, "NotFound"); err != nil {
		t.Fatalf("ErrorType: %v", err)
	}
	matcher := w.errorTypes["NotFound"]
	if matcher == nil {
		t.Fatalf("matcher closure was not stored under name")
	}
	if !matcher(notFoundErr{key: "u/42"}) {
		t.Errorf("matcher did not match value of registered type")
	}
	wrapped := errors.New("plain")
	if matcher(wrapped) {
		t.Errorf("matcher matched an unrelated error")
	}
}
