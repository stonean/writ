package writ

import (
	"os"
	"sync/atomic"
)

// Lifecycle states held in Writ.state as atomic.Uint32. Transitions:
//
//	stateInit    → stateLoading  (Load enters; CAS-protected)
//	stateLoading → stateLoaded   (Load succeeds)
//	stateLoading → stateInit     (Load fails; rolled back)
//	stateLoaded  → —             (terminal)
const (
	stateInit uint32 = iota
	stateLoading
	stateLoaded
)

// Reserved environment variable defaults per specs/system.md. PORT
// is the HTTP listen port; WRIT_ENV is the runtime mode
// (development, test, production). The skeleton iteration consumes
// only these two.
const (
	defaultPort    = "8080"
	defaultWritEnv = "production"
)

// Writ is a runtime instance. Construct with [New], register
// resolvers and formatters, call [Writ.Load] to compile a .writ
// program, then call [Writ.Handler] or [Writ.Run] to serve.
//
// A Writ instance is the unit of isolation; tests build a fresh
// instance per case. Registrations are accepted while the instance
// is in its initial state and must be complete before [Writ.Load].
//
// Subsequent tasks attach resolver/formatter registration tables
// (task 3) and the compiled routing table (task 5) to this struct.
type Writ struct {
	state   atomic.Uint32
	writEnv string
}

// New returns a fresh runtime instance with no registrations and no
// loaded program. WRIT_ENV is snapshotted at construction time;
// later mutations to the process environment are not observed.
func New() *Writ {
	env := os.Getenv("WRIT_ENV")
	if env == "" {
		env = defaultWritEnv
	}
	return &Writ{writEnv: env}
}
