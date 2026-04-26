package writ

import (
	"net/http"
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
type Writ struct {
	state      atomic.Uint32
	resolvers  map[string]ResolverFunc
	formatters map[string]FormatterFunc
	table      atomic.Pointer[routingTable]
	writEnv    string
}

// New returns a fresh runtime instance with no registrations and no
// loaded program. WRIT_ENV is snapshotted at construction time;
// later mutations to the process environment are not observed.
func New() *Writ {
	env := os.Getenv("WRIT_ENV")
	if env == "" {
		env = defaultWritEnv
	}
	return &Writ{
		resolvers:  make(map[string]ResolverFunc),
		formatters: make(map[string]FormatterFunc),
		writEnv:    env,
	}
}

// Run loads the .writ program at path, then binds an HTTP listener
// on the port from PORT (default "8080") and serves until the
// process is interrupted. Run is sugar over Load + Handler +
// http.ListenAndServe; it returns whatever error Load or
// ListenAndServe produces.
//
// Run does not accept an address argument and configures no server
// timeouts. Callers needing a custom address or timeouts use
// [Writ.Load] plus their own &http.Server{} composition with
// [Writ.Handler].
func (w *Writ) Run(path string) error {
	if err := w.Load(path); err != nil {
		return err
	}
	// G114 flags ListenAndServe without a timeout-configured
	// server. The runtime intentionally imposes no per-request
	// timeout per spec 003 Q12; callers needing timeouts compose
	// their own &http.Server with [Writ.Handler].
	return http.ListenAndServe(":"+resolvePort(), w.Handler()) // #nosec G114 -- timeout policy deferred per spec 003 Q12
}

// resolvePort returns the port Run binds on. It reads PORT from
// the process environment and falls back to defaultPort when the
// variable is unset or empty.
func resolvePort() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return defaultPort
}
