package writ

import "testing"

func TestResultsGetPresent(t *testing.T) {
	r := Results{values: map[string]any{"user": "alice"}}
	if got := r.Get("user"); got != "alice" {
		t.Errorf("Get(user) = %v, want %q", got, "alice")
	}
}

func TestResultsGetAbsent(t *testing.T) {
	r := Results{values: map[string]any{"user": "alice"}}
	if got := r.Get("missing"); got != nil {
		t.Errorf("Get(missing) = %v, want nil", got)
	}
}

func TestResultsGetZeroValueResults(t *testing.T) {
	var r Results
	if got := r.Get("anything"); got != nil {
		t.Errorf("Get on zero Results returned %v, want nil", got)
	}
}

func TestResultsHasPresent(t *testing.T) {
	r := Results{values: map[string]any{"user": "alice"}}
	if !r.Has("user") {
		t.Errorf("Has(user) = false, want true")
	}
}

func TestResultsHasAbsent(t *testing.T) {
	r := Results{values: map[string]any{"user": "alice"}}
	if r.Has("missing") {
		t.Errorf("Has(missing) = true, want false")
	}
}

func TestResultsHasDistinguishesNilValueFromAbsence(t *testing.T) {
	// A resolver may legitimately return (nil, nil); presence under
	// the with-clause must still be detectable.
	r := Results{values: map[string]any{"maybe": nil}}
	if !r.Has("maybe") {
		t.Errorf("Has(maybe) = false; presence must distinguish from absence even when value is nil")
	}
	if got := r.Get("maybe"); got != nil {
		t.Errorf("Get(maybe) = %v, want nil", got)
	}
}
