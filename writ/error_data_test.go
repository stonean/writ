package writ

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorDataAccessors(t *testing.T) {
	wantErr := errors.New("boom")
	req := httptest.NewRequest(http.MethodGet, "/widgets/42", nil)
	d := ErrorData{err: wantErr, status: 418, request: req}

	if got := d.Err(); got != wantErr {
		t.Errorf("Err() = %v, want %v", got, wantErr)
	}
	if got := d.Status(); got != 418 {
		t.Errorf("Status() = %d, want 418", got)
	}
	if got := d.Request(); got != req {
		t.Errorf("Request() = %p, want %p", got, req)
	}
}

func TestErrorDataZeroValue(t *testing.T) {
	var d ErrorData
	if got := d.Err(); got != nil {
		t.Errorf("zero Err() = %v, want nil", got)
	}
	if got := d.Status(); got != 0 {
		t.Errorf("zero Status() = %d, want 0", got)
	}
	if got := d.Request(); got != nil {
		t.Errorf("zero Request() = %v, want nil", got)
	}
}
