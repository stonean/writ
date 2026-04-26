package writ

// Results is the formatter-side accessor for named resolver results.
// The runtime constructs a fresh Results per request, restricted to
// the variable names listed in the format line's `with` clause.
// Values resolved by the handler but not listed in `with` are not
// visible here.
//
// Get returns the raw any value because resolvers in the skeleton
// iteration return any. Future code-generation features layer typed
// accessors on top without changing this signature.
type Results struct {
	values map[string]any
}

// Get returns the value stored under name, or nil when name is not
// present in the with-clause projection.
func (r Results) Get(name string) any {
	return r.values[name]
}

// Has reports whether name is present in the with-clause projection.
// It distinguishes "absent" from "present with a nil value".
func (r Results) Has(name string) bool {
	_, ok := r.values[name]
	return ok
}
