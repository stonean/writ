package ast

// RoutePattern is a parsed route pattern such as "/users/:id" or "/admin/*".
// An empty Segments slice represents the root pattern "/".
type RoutePattern struct {
	nodeBase
	Segments []RouteSegment
}

// NewRoutePattern constructs a RoutePattern with the given span.
func NewRoutePattern(span Span) *RoutePattern {
	return &RoutePattern{nodeBase: nodeBase{span: span}}
}

// RouteSegment is one of: literal, parameter, wildcard.
// The parser enforces that WildcardSegment may only appear as the
// final element.
type RouteSegment interface {
	Node
	routeSegment()
}

// LiteralSegment is a static path component such as "users".
type LiteralSegment struct {
	nodeBase
	Text string
}

func (LiteralSegment) routeSegment() {}

// NewLiteralSegment constructs a LiteralSegment with the given span.
func NewLiteralSegment(span Span, text string) *LiteralSegment {
	return &LiteralSegment{nodeBase: nodeBase{span: span}, Text: text}
}

// ParameterSegment is a `:name` capture such as ":id" in "/users/:id".
type ParameterSegment struct {
	nodeBase
	Name string
}

func (ParameterSegment) routeSegment() {}

// NewParameterSegment constructs a ParameterSegment with the given span.
func NewParameterSegment(span Span, name string) *ParameterSegment {
	return &ParameterSegment{nodeBase: nodeBase{span: span}, Name: name}
}

// WildcardSegment is the trailing "*" in patterns such as "/admin/*".
type WildcardSegment struct {
	nodeBase
}

func (WildcardSegment) routeSegment() {}

// NewWildcardSegment constructs a WildcardSegment with the given span.
func NewWildcardSegment(span Span) *WildcardSegment {
	return &WildcardSegment{nodeBase: nodeBase{span: span}}
}
