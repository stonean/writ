package writ

import "errors"

// ErrorType registers the Go type T under a DSL identifier. The
// runtime invokes a closure of the form
//
//	func(err error) bool { var t T; return errors.As(err, &t) }
//
// at dispatch time to determine whether a returned error matches the
// registered type. errors.As handles pointer-vs-value, embedded
// wrapping, and Unwrap-chain walking automatically.
//
// ErrorType is a top-level function rather than a method on [Writ]
// because Go does not permit type parameters on methods. The
// asymmetry with [Writ.ErrorFormatter] is a Go-language constraint.
//
// Returns an error when name is already registered as an error type.
// Panics when called after [Writ.Load].
func ErrorType[T error](w *Writ, name string) error {
	return w.registerErrorType(name, func(err error) bool {
		var t T
		return errors.As(err, &t)
	})
}
