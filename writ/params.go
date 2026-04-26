package writ

// Params is the resolver-side accessor for route parameters bound at
// request time. The runtime constructs a fresh Params per resolver
// call, restricted to the parameter names listed as `:name`
// arguments in the resolver's DSL call site. A resolver does not
// observe parameters it did not ask for.
//
// Missing-key access returns the zero value rather than panicking;
// startup validation already checks that every `:name` referenced in
// a resolver argument is declared in the handler's route, so a miss
// at request time can only happen via a test harness that bypasses
// validation.
type Params struct {
	values map[string]string
}

// String returns the value bound to name, or "" when name is not
// present.
func (p Params) String(name string) string {
	return p.values[name]
}

// Has reports whether name has a bound value.
func (p Params) Has(name string) bool {
	_, ok := p.values[name]
	return ok
}
