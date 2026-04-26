// Package pipeline elaborates a parsed ast.Program into per-handler
// effective pipelines: applying system → group → handler override
// rules, choosing the matching errors block, enforcing stage placement
// and canonical stage order, and recording explicit `<stage> none`
// opt-outs.
//
// Pipeline elaboration is purely structural. It does not perform name
// resolution, type checking, cross-stage pairing checks, or any I/O.
// Those concerns belong to startup validation.
//
// Pre-1.0 the pipeline surface (Elaborate, Resolved, Handler, Stage,
// Error) is stable enough to use but not promised to be source-
// compatible across minor versions, matching the AST package's stance.
// Internal Writ components (runtime, code generator, CLI) are the
// primary consumers; third-party use is welcome but accepts that
// instability.
package pipeline
