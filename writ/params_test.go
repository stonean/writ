package writ

import "testing"

func TestParamsStringPresent(t *testing.T) {
	p := Params{values: map[string]string{"id": "42"}}
	if got := p.String("id"); got != "42" {
		t.Errorf("String(id) = %q, want %q", got, "42")
	}
}

func TestParamsStringAbsent(t *testing.T) {
	p := Params{values: map[string]string{"id": "42"}}
	if got := p.String("missing"); got != "" {
		t.Errorf("String(missing) = %q, want zero value", got)
	}
}

func TestParamsStringZeroValueParams(t *testing.T) {
	var p Params
	if got := p.String("anything"); got != "" {
		t.Errorf("String on zero Params returned %q, want empty", got)
	}
}

func TestParamsHasPresent(t *testing.T) {
	p := Params{values: map[string]string{"id": "42"}}
	if !p.Has("id") {
		t.Errorf("Has(id) = false, want true")
	}
}

func TestParamsHasAbsent(t *testing.T) {
	p := Params{values: map[string]string{"id": "42"}}
	if p.Has("missing") {
		t.Errorf("Has(missing) = true, want false")
	}
}

func TestParamsHasZeroValueParams(t *testing.T) {
	var p Params
	if p.Has("anything") {
		t.Errorf("Has on zero Params returned true, want false")
	}
}

func TestParamsEmptyStringValueIsPresent(t *testing.T) {
	p := Params{values: map[string]string{"empty": ""}}
	if !p.Has("empty") {
		t.Errorf("Has(empty) = false; presence must distinguish from absence even when value is \"\"")
	}
	if got := p.String("empty"); got != "" {
		t.Errorf("String(empty) = %q, want \"\"", got)
	}
}
