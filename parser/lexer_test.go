package parser

import (
	"reflect"
	"testing"

	"github.com/stonean/writ/ast"
)

func lexAll(t *testing.T, src string) ([]Token, []Error) {
	t.Helper()
	s := &ast.Source{Path: "test.writ", Bytes: []byte(src)}
	l := newLexer(s)
	var toks []Token
	for {
		tok := l.next()
		toks = append(toks, tok)
		if tok.Kind == TokenEOF {
			break
		}
	}
	return toks, l.Errors()
}

func kindsOf(toks []Token) []TokenKind {
	out := make([]TokenKind, len(toks))
	for i, tok := range toks {
		out[i] = tok.Kind
	}
	return out
}

func TestLexerEachTokenKind(t *testing.T) {
	cases := []struct {
		name  string
		input string
		kinds []TokenKind
	}{
		{"identifier", "user", []TokenKind{TokenIdent, TokenEOF}},
		{"dotted identifier", "db.users.create", []TokenKind{TokenIdent, TokenEOF}},
		{"identifier with underscore", "user_name", []TokenKind{TokenIdent, TokenEOF}},
		{"identifier with keyword segment", "db.session.refresh", []TokenKind{TokenIdent, TokenEOF}},
		{"integer", "60", []TokenKind{TokenInt, TokenEOF}},
		{"string", `"hello"`, []TokenKind{TokenString, TokenEOF}},
		{"empty string", `""`, []TokenKind{TokenString, TokenEOF}},
		{"rate sec", "60/sec", []TokenKind{TokenRate, TokenEOF}},
		{"rate min", "60/min", []TokenKind{TokenRate, TokenEOF}},
		{"rate hour", "1/hour", []TokenKind{TokenRate, TokenEOF}},
		{"rate day", "100/day", []TokenKind{TokenRate, TokenEOF}},
		{"arrow", "->", []TokenKind{TokenArrow, TokenEOF}},
		{"lparen", "(", []TokenKind{TokenLParen, TokenEOF}},
		{"rparen", ")", []TokenKind{TokenRParen, TokenEOF}},
		{"comma", ",", []TokenKind{TokenComma, TokenEOF}},
		{"colon", ":", []TokenKind{TokenColon, TokenEOF}},
		{"equals", "=", []TokenKind{TokenEquals, TokenEOF}},
		{"star", "*", []TokenKind{TokenStar, TokenEOF}},
		{"slash", "/", []TokenKind{TokenSlash, TokenEOF}},
		{"minus", "-", []TokenKind{TokenMinus, TokenEOF}},
		{"newline", "\n", []TokenKind{TokenNewline, TokenEOF}},
		{"crlf newline", "\r\n", []TokenKind{TokenNewline, TokenEOF}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toks, errs := lexAll(t, tc.input)
			if len(errs) > 0 {
				t.Fatalf("unexpected lex errors: %v", errs)
			}
			got := kindsOf(toks)
			if !reflect.DeepEqual(got, tc.kinds) {
				t.Fatalf("kinds = %v, want %v", got, tc.kinds)
			}
		})
	}
}

func TestLexerStringEscapes(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"escape quote", `"\""`, `"`},
		{"escape backslash", `"\\"`, `\`},
		{"escape newline", `"\n"`, "\n"},
		{"escape tab", `"\t"`, "\t"},
		{"escape carriage return", `"\r"`, "\r"},
		{"mixed", `"a\nb"`, "a\nb"},
		{"plain text", `"hello world"`, "hello world"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toks, errs := lexAll(t, tc.input)
			if len(errs) > 0 {
				t.Fatalf("unexpected lex errors: %v", errs)
			}
			if toks[0].Kind != TokenString {
				t.Fatalf("kind = %v, want STRING", toks[0].Kind)
			}
			if toks[0].Lexeme != tc.want {
				t.Fatalf("lexeme = %q, want %q", toks[0].Lexeme, tc.want)
			}
		})
	}
}

func TestLexerStringErrors(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantSubst string
	}{
		{"unterminated at EOF", `"hello`, "unterminated"},
		{"raw newline", "\"hello\nworld\"", "raw newline"},
		{"unknown escape", `"\q"`, "unknown escape"},
		{"backslash at EOF", `"\`, "unterminated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := lexAll(t, tc.input)
			if len(errs) == 0 {
				t.Fatalf("expected lex error for %q", tc.input)
			}
			if !containsString(errs[0].Message, tc.wantSubst) {
				t.Fatalf("error %q does not contain %q", errs[0].Message, tc.wantSubst)
			}
		})
	}
}

func TestLexerRateValuesParsed(t *testing.T) {
	toks, errs := lexAll(t, "60/min")
	if len(errs) > 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	if toks[0].Kind != TokenRate {
		t.Fatalf("kind = %v, want RATE", toks[0].Kind)
	}
	if toks[0].RateCount != 60 || toks[0].RateUnit != "min" {
		t.Fatalf("rate = %d/%s, want 60/min", toks[0].RateCount, toks[0].RateUnit)
	}
	if toks[0].Lexeme != "60/min" {
		t.Fatalf("lexeme = %q, want %q", toks[0].Lexeme, "60/min")
	}
}

func TestLexerBadRateUnit(t *testing.T) {
	_, errs := lexAll(t, "(60/foo)")
	if len(errs) == 0 {
		t.Fatalf("expected lex error for invalid rate unit")
	}
	if !containsString(errs[0].Message, "invalid rate unit") {
		t.Fatalf("error %q does not mention invalid rate unit", errs[0].Message)
	}
}

func TestLexerSlashThenIntDoesNotCombineIntoRate(t *testing.T) {
	// /60/min must lex as SLASH INT SLASH IDENT — the route-pattern
	// shape — not SLASH RATE.
	toks, errs := lexAll(t, "/60/min")
	if len(errs) > 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	want := []TokenKind{TokenSlash, TokenInt, TokenSlash, TokenIdent, TokenEOF}
	if got := kindsOf(toks); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
}

func TestLexerIntFollowedByDottedIdentNotRate(t *testing.T) {
	// 60/min.foo isn't a rate — back out to INT and continue.
	toks, errs := lexAll(t, "(60/min.foo)")
	if len(errs) > 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	want := []TokenKind{TokenLParen, TokenInt, TokenSlash, TokenIdent, TokenRParen, TokenEOF}
	if got := kindsOf(toks); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
}

func TestLexerStrayCharacter(t *testing.T) {
	_, errs := lexAll(t, "a@b")
	if len(errs) == 0 {
		t.Fatalf("expected stray-char lex error")
	}
	if !containsString(errs[0].Message, "unexpected character") {
		t.Fatalf("error %q does not mention unexpected character", errs[0].Message)
	}
}

func TestLexerCommentsStripped(t *testing.T) {
	toks, errs := lexAll(t, "user # trailing comment\nmore\n# whole line\nlast")
	if len(errs) > 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	want := []TokenKind{
		TokenIdent, TokenNewline,
		TokenIdent, TokenNewline,
		TokenNewline,
		TokenIdent,
		TokenEOF,
	}
	if got := kindsOf(toks); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for _, tok := range toks {
		if tok.Kind == TokenIdent &&
			(tok.Lexeme == "comment" || tok.Lexeme == "trailing" || tok.Lexeme == "whole") {
			t.Fatalf("comment tokens leaked into stream: %#v", tok)
		}
	}
}

func TestLexerArrowVsMinus(t *testing.T) {
	toks, errs := lexAll(t, "- ->")
	if len(errs) > 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	want := []TokenKind{TokenMinus, TokenArrow, TokenEOF}
	if got := kindsOf(toks); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
}

func TestLexerPositionTracking(t *testing.T) {
	src := "system ->\n  log :id\n"
	toks, errs := lexAll(t, src)
	if len(errs) > 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}

	expected := []struct {
		kind        TokenKind
		startLine   int
		startColumn int
		lexeme      string
	}{
		{TokenIdent, 1, 1, "system"},
		{TokenArrow, 1, 8, "->"},
		{TokenNewline, 1, 10, "\n"},
		{TokenIdent, 2, 3, "log"},
		{TokenColon, 2, 7, ":"},
		{TokenIdent, 2, 8, "id"},
		{TokenNewline, 2, 10, "\n"},
		{TokenEOF, 3, 1, ""},
	}
	if len(toks) != len(expected) {
		t.Fatalf("got %d tokens, want %d: %#v", len(toks), len(expected), toks)
	}
	for i, ex := range expected {
		tok := toks[i]
		if tok.Kind != ex.kind {
			t.Errorf("tok[%d] kind = %v, want %v", i, tok.Kind, ex.kind)
		}
		if tok.Span.Start.Line != ex.startLine || tok.Span.Start.Column != ex.startColumn {
			t.Errorf("tok[%d] start = (%d,%d), want (%d,%d)",
				i, tok.Span.Start.Line, tok.Span.Start.Column, ex.startLine, ex.startColumn)
		}
		if tok.Lexeme != ex.lexeme {
			t.Errorf("tok[%d] lexeme = %q, want %q", i, tok.Lexeme, ex.lexeme)
		}
	}
}

func TestLexerSpanTextRoundTripsForSimpleTokens(t *testing.T) {
	src := "GET /users/:id\n"
	toks, errs := lexAll(t, src)
	if len(errs) > 0 {
		t.Fatalf("unexpected lex errors: %v", errs)
	}
	for _, tok := range toks {
		if tok.Kind == TokenEOF || tok.Kind == TokenNewline {
			continue
		}
		got := string(tok.Span.Text())
		if got != tok.Lexeme {
			t.Errorf("Span.Text() = %q, Lexeme = %q (mismatch on simple token)", got, tok.Lexeme)
		}
	}
}

func TestLexerErrorCarriesSpan(t *testing.T) {
	_, errs := lexAll(t, "  @\n")
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1: %v", len(errs), errs)
	}
	e := errs[0]
	if e.File != "test.writ" {
		t.Errorf("File = %q, want test.writ", e.File)
	}
	if e.Line != 1 {
		t.Errorf("Line = %d, want 1", e.Line)
	}
	if e.Column != 3 {
		t.Errorf("Column = %d, want 3", e.Column)
	}
	if e.Span.Start.Source == nil {
		t.Errorf("Span has nil Source")
	}
	want := "test.writ:1:3: unexpected character \"@\""
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestLexerMultipleErrorsInOnePass(t *testing.T) {
	_, errs := lexAll(t, "@ # ignored\n? # ignored\n")
	if len(errs) < 2 {
		t.Fatalf("expected multiple errors, got %d: %v", len(errs), errs)
	}
}

func containsString(s, substr string) bool {
	if substr == "" {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
