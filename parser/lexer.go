package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stonean/writ/ast"
)

// TokenKind enumerates the lexical token kinds the lexer emits.
//
// Reserved words (system, group, with, OR, etc.) are NOT distinguished
// at lex time — they are lexed as TokenIdent and matched by lexeme at
// the relevant grammar position. This implements the spec's
// "contextual keywords" requirement.
type TokenKind int

const (
	TokenEOF TokenKind = iota
	TokenNewline
	TokenIdent
	TokenInt
	TokenString
	TokenRate
	TokenArrow  // ->
	TokenLParen // (
	TokenRParen // )
	TokenComma  // ,
	TokenColon  // :
	TokenEquals // =
	TokenStar   // *
	TokenSlash  // /
	TokenMinus  // -
	TokenError  // sentinel; the corresponding Error is in lexer.Errors()
)

// String returns a stable lowercase-ish name for a kind, suitable for
// test output and error messages.
func (k TokenKind) String() string {
	switch k {
	case TokenEOF:
		return "EOF"
	case TokenNewline:
		return "NEWLINE"
	case TokenIdent:
		return "IDENT"
	case TokenInt:
		return "INT"
	case TokenString:
		return "STRING"
	case TokenRate:
		return "RATE"
	case TokenArrow:
		return "ARROW"
	case TokenLParen:
		return "LPAREN"
	case TokenRParen:
		return "RPAREN"
	case TokenComma:
		return "COMMA"
	case TokenColon:
		return "COLON"
	case TokenEquals:
		return "EQUALS"
	case TokenStar:
		return "STAR"
	case TokenSlash:
		return "SLASH"
	case TokenMinus:
		return "MINUS"
	case TokenError:
		return "ERROR"
	}
	return "UNKNOWN"
}

// Token is one element in the lexer's output stream.
//
// Lexeme holds the canonical text of the token: for TokenString it is
// the post-escape-processed value; for every other kind it is the
// verbatim bytes the token covers (recoverable from Span.Text()). For
// TokenRate the parsed Count and Unit are also populated.
type Token struct {
	Kind   TokenKind
	Lexeme string
	Span   ast.Span

	RateCount int64  // populated for TokenRate
	RateUnit  string // populated for TokenRate ("sec" / "min" / "hour" / "day")
}

// lexer is a byte-oriented scanner over a single ast.Source.
//
// The lexer keeps the previously-emitted TokenKind so it can
// distinguish a rate literal (e.g. "60/min" inside a call) from a
// route segment sequence (e.g. "/60/min" in a route pattern). When
// the previous token was TokenSlash the int-followed-by-`/<unit>`
// shape is not collapsed into TokenRate; the components are emitted
// separately.
type lexer struct {
	src      *ast.Source
	pos      int // 0-based byte offset
	line     int // 1-based
	col      int // 1-based, byte position within the line
	prev     TokenKind
	havePrev bool
	errs     []Error
}

func newLexer(src *ast.Source) *lexer {
	return &lexer{src: src, line: 1, col: 1}
}

// Errors returns the lex errors accumulated so far.
func (l *lexer) Errors() []Error { return l.errs }

func (l *lexer) atEOF() bool { return l.pos >= len(l.src.Bytes) }

func (l *lexer) peek() byte {
	if l.atEOF() {
		return 0
	}
	return l.src.Bytes[l.pos]
}

func (l *lexer) peekAt(off int) byte {
	p := l.pos + off
	if p < 0 || p >= len(l.src.Bytes) {
		return 0
	}
	return l.src.Bytes[p]
}

func (l *lexer) advance() byte {
	if l.atEOF() {
		return 0
	}
	b := l.src.Bytes[l.pos]
	l.pos++
	if b == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return b
}

func (l *lexer) position() ast.Position {
	return ast.Position{Source: l.src, Line: l.line, Column: l.col, Offset: l.pos}
}

func (l *lexer) span(start, end ast.Position) ast.Span {
	return ast.Span{Start: start, End: end}
}

func (l *lexer) emit(kind TokenKind, span ast.Span, lexeme string) Token {
	l.prev = kind
	l.havePrev = true
	return Token{Kind: kind, Lexeme: lexeme, Span: span}
}

func (l *lexer) recordError(span ast.Span, format string, args ...any) {
	l.errs = append(l.errs, Error{
		File:    l.src.Path,
		Line:    span.Start.Line,
		Column:  span.Start.Column,
		Span:    span,
		Message: fmt.Sprintf(format, args...),
	})
}

// next returns the next token. After the source is exhausted, every
// further call returns a TokenEOF anchored at the end of the source.
func (l *lexer) next() Token {
	for {
		if l.atEOF() {
			start := l.position()
			return l.emit(TokenEOF, l.span(start, start), "")
		}
		b := l.peek()
		switch b {
		case ' ', '\t':
			l.advance()
			continue
		case '#':
			l.skipLineComment()
			continue
		case '\n':
			start := l.position()
			l.advance()
			end := l.position()
			return l.emit(TokenNewline, l.span(start, end), "\n")
		case '\r':
			start := l.position()
			l.advance()
			if l.peek() == '\n' {
				l.advance()
			}
			end := l.position()
			return l.emit(TokenNewline, l.span(start, end), "\n")
		}
		break
	}

	start := l.position()
	b := l.peek()

	switch b {
	case '(':
		l.advance()
		return l.emit(TokenLParen, l.span(start, l.position()), "(")
	case ')':
		l.advance()
		return l.emit(TokenRParen, l.span(start, l.position()), ")")
	case ',':
		l.advance()
		return l.emit(TokenComma, l.span(start, l.position()), ",")
	case ':':
		l.advance()
		return l.emit(TokenColon, l.span(start, l.position()), ":")
	case '=':
		l.advance()
		return l.emit(TokenEquals, l.span(start, l.position()), "=")
	case '*':
		l.advance()
		return l.emit(TokenStar, l.span(start, l.position()), "*")
	case '/':
		l.advance()
		return l.emit(TokenSlash, l.span(start, l.position()), "/")
	case '-':
		if l.peekAt(1) == '>' {
			l.advance()
			l.advance()
			return l.emit(TokenArrow, l.span(start, l.position()), "->")
		}
		l.advance()
		return l.emit(TokenMinus, l.span(start, l.position()), "-")
	case '"':
		return l.scanString(start)
	}

	if isDigit(b) {
		return l.scanNumberOrRate(start)
	}
	if isLetter(b) {
		return l.scanIdent(start)
	}

	l.advance()
	span := l.span(start, l.position())
	l.recordError(span, "unexpected character %q", string(b))
	return l.emit(TokenError, span, string(b))
}

func (l *lexer) skipLineComment() {
	for !l.atEOF() && l.src.Bytes[l.pos] != '\n' {
		l.advance()
	}
}

func (l *lexer) scanIdent(start ast.Position) Token {
	l.advance() // leading letter
	for {
		b := l.peek()
		if isLetter(b) || isDigit(b) || b == '_' {
			l.advance()
			continue
		}
		if b == '.' && isLetter(l.peekAt(1)) {
			l.advance() // '.'
			l.advance() // segment-leading letter
			continue
		}
		break
	}
	end := l.position()
	span := l.span(start, end)
	return l.emit(TokenIdent, span, string(l.src.Bytes[start.Offset:end.Offset]))
}

// scanNumberOrRate scans an unsigned decimal integer, then peeks for a
// rate-literal continuation `/<unit>`. A rate is recognized only when
// the previously emitted token was not TokenSlash; this lets route
// patterns of the form "/60/min" tokenize as SLASH INT SLASH IDENT
// while a call argument like "60/min" tokenizes as a single TokenRate.
func (l *lexer) scanNumberOrRate(start ast.Position) Token {
	digitStart := l.pos
	for isDigit(l.peek()) {
		l.advance()
	}
	digitEnd := l.pos
	intLexeme := string(l.src.Bytes[digitStart:digitEnd])
	intSpan := l.span(start, l.position())

	if l.peek() != '/' || !isLetter(l.peekAt(1)) {
		return l.emit(TokenInt, intSpan, intLexeme)
	}
	if l.havePrev && l.prev == TokenSlash {
		return l.emit(TokenInt, intSpan, intLexeme)
	}

	// Tentatively consume `/` and the trailing letter run; if what
	// follows extends into a longer identifier (`.` or `_` or digits),
	// the candidate isn't a clean rate — back out to plain INT.
	savedPos, savedLine, savedCol := l.pos, l.line, l.col
	l.advance() // '/'
	unitStart := l.pos
	for isLetter(l.peek()) {
		l.advance()
	}
	unit := string(l.src.Bytes[unitStart:l.pos])
	if next := l.peek(); next == '.' || next == '_' || isDigit(next) {
		l.pos, l.line, l.col = savedPos, savedLine, savedCol
		return l.emit(TokenInt, intSpan, intLexeme)
	}

	end := l.position()
	span := l.span(start, end)
	verbatim := string(l.src.Bytes[start.Offset:end.Offset])
	switch unit {
	case "sec", "min", "hour", "day":
		count, err := strconv.ParseInt(intLexeme, 10, 64)
		if err != nil {
			l.recordError(span, "rate count %q out of range", intLexeme)
			return l.emit(TokenError, span, verbatim)
		}
		tok := l.emit(TokenRate, span, verbatim)
		tok.RateCount = count
		tok.RateUnit = unit
		return tok
	default:
		l.recordError(span, "invalid rate unit %q; expected sec, min, hour, or day", unit)
		return l.emit(TokenError, span, verbatim)
	}
}

func (l *lexer) scanString(start ast.Position) Token {
	l.advance() // opening quote
	var value strings.Builder
	for {
		if l.atEOF() {
			end := l.position()
			span := l.span(start, end)
			l.recordError(span, "unterminated string literal")
			return l.emit(TokenError, span, value.String())
		}
		b := l.peek()
		switch b {
		case '\n', '\r':
			end := l.position()
			span := l.span(start, end)
			l.recordError(span, "unterminated string literal: raw newline in string")
			return l.emit(TokenError, span, value.String())
		case '"':
			l.advance()
			end := l.position()
			span := l.span(start, end)
			return l.emit(TokenString, span, value.String())
		case '\\':
			escStart := l.position()
			l.advance() // backslash
			if l.atEOF() {
				end := l.position()
				span := l.span(start, end)
				l.recordError(span, "unterminated string literal")
				return l.emit(TokenError, span, value.String())
			}
			esc := l.peek()
			switch esc {
			case '"':
				value.WriteByte('"')
				l.advance()
			case '\\':
				value.WriteByte('\\')
				l.advance()
			case 'n':
				value.WriteByte('\n')
				l.advance()
			case 't':
				value.WriteByte('\t')
				l.advance()
			case 'r':
				value.WriteByte('\r')
				l.advance()
			default:
				l.advance()
				escSpan := l.span(escStart, l.position())
				l.recordError(escSpan, "unknown escape sequence: \\%c", esc)
			}
		default:
			value.WriteByte(b)
			l.advance()
		}
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
