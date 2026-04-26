// Package writ is the runtime entry point for the Writ web framework.
//
// A program creates a runtime instance with [New], registers Go
// resolvers and formatters, calls [Writ.Load] to compile a .writ file
// (parsing, pipeline elaboration, and startup validation in one
// pass), then either calls [Writ.Handler] to obtain a net/http
// handler or [Writ.Run] to bind a listener and serve.
//
// The skeleton iteration supports the resolve and format pipeline
// stages only; every other stage (session, csrf, limit, approve,
// commit, emit, log, measure, layout, redirect) is rejected at
// startup with a structured error. Later features layer the
// remaining stages, code generation, templates, and the testing DSL
// on top without redesigning the runtime's bones.
//
// Pre-1.0 the writ surface is stable enough to use but not promised
// to be source-compatible across minor versions, matching the ast
// and pipeline packages' stance.
package writ
