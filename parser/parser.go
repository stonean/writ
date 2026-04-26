package parser

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stonean/writ/ast"
)

// Option configures Parse and ParseString.
type Option func(*config)

type config struct {
	fsys fs.FS
	root string
}

// WithFS overrides the filesystem used for include resolution. The
// default is os.DirFS rooted at the directory of the file passed to
// Parse.
func WithFS(fsys fs.FS) Option {
	return func(c *config) { c.fsys = fsys }
}

// WithRoot overrides the include-resolution root directory.
func WithRoot(dir string) Option {
	return func(c *config) { c.root = dir }
}

// Parse reads the .writ file at rootPath and returns the resulting
// Program plus any errors. The Program is always non-nil.
func Parse(rootPath string, opts ...Option) (*ast.Program, []Error) {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.fsys == nil {
		cfg.fsys = os.DirFS(filepath.Dir(rootPath))
	}
	if cfg.root == "" {
		cfg.root = filepath.Dir(rootPath)
	}

	relPath := filepath.Base(rootPath)
	bytes, err := fs.ReadFile(cfg.fsys, relPath)
	if err != nil {
		empty := &ast.Source{Path: rootPath, Bytes: nil}
		zero := ast.Position{Source: empty}
		program := ast.NewProgram(ast.Span{Start: zero, End: zero})
		program.Sources = []*ast.Source{empty}
		return program, []Error{{
			File:    rootPath,
			Line:    1,
			Column:  1,
			Span:    ast.Span{Start: zero, End: zero},
			Message: fmt.Sprintf("cannot read %s: %v", rootPath, err),
		}}
	}

	src := &ast.Source{Path: rootPath, Bytes: bytes}
	session := &parseSession{
		cfg:        cfg,
		cycleStack: []string{relPath},
		sources:    []*ast.Source{src},
	}
	p := newParser(src, relPath, session)
	program := p.parseProgram()
	program.Sources = session.sources
	return program, session.errors
}

// ParseString parses an in-memory source string. virtualPath is the
// path the source will appear under in error messages and AST spans.
// Includes from a ParseString'd source are not supported (no
// filesystem is configured) and are reported as errors.
func ParseString(virtualPath, source string) (*ast.Program, []Error) {
	src := &ast.Source{Path: virtualPath, Bytes: []byte(source)}
	session := &parseSession{
		cfg:     &config{},
		sources: []*ast.Source{src},
	}
	p := newParser(src, "", session)
	program := p.parseProgram()
	program.Sources = session.sources
	return program, session.errors
}

// parseSession holds state shared by every parser instance in one
// Parse / ParseString call: the configured filesystem, the include
// cycle stack, the cumulative Sources registry, and the merged error
// list. Each included file gets its own *parser, but they all
// contribute to the same session.
type parseSession struct {
	cfg        *config
	cycleStack []string // fsys-relative paths currently on the include stack
	sources    []*ast.Source
	errors     []Error
}

// newParser creates a parser for src. relPath is src's path within
// session.cfg.fsys (used to resolve includes); it is empty for
// ParseString'd sources.
func newParser(src *ast.Source, relPath string, session *parseSession) *parser {
	tokens, lexErrs := tokenize(src)
	session.errors = append(session.errors, lexErrs...)
	return &parser{
		src:     src,
		relPath: relPath,
		tokens:  tokens,
		session: session,
	}
}

func tokenize(src *ast.Source) ([]Token, []Error) {
	l := newLexer(src)
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

// parser holds the parse state for a single source. The token
// stream is fully materialized up front so peekAt(k) is O(1).
// Errors, the include cycle stack, and the source registry live on
// the shared *parseSession so include resolution can recurse without
// losing state.
type parser struct {
	src     *ast.Source
	relPath string // src's path within session.cfg.fsys (empty for ParseString)
	tokens  []Token
	pos     int
	session *parseSession
}

func (p *parser) atEOF() bool { return p.peek().Kind == TokenEOF }

func (p *parser) peek() Token { return p.tokens[p.pos] }

func (p *parser) peekAt(off int) Token {
	i := p.pos + off
	if i >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1] // EOF sentinel
	}
	if i < 0 {
		return p.tokens[0]
	}
	return p.tokens[i]
}

func (p *parser) advance() Token {
	tok := p.tokens[p.pos]
	if p.pos < len(p.tokens)-1 {
		p.pos++
	}
	return tok
}

func (p *parser) errAt(span ast.Span, format string, args ...any) {
	p.session.errors = append(p.session.errors, Error{
		File:    p.src.Path,
		Line:    span.Start.Line,
		Column:  span.Start.Column,
		Span:    span,
		Message: fmt.Sprintf(format, args...),
	})
}

func (p *parser) errCurrent(format string, args ...any) {
	p.errAt(p.peek().Span, format, args...)
}

func (p *parser) expect(kind TokenKind, what string) (Token, bool) {
	if p.peek().Kind == kind {
		return p.advance(), true
	}
	p.errCurrent("expected %s, got %s", what, describeToken(p.peek()))
	return Token{}, false
}

func describeToken(tok Token) string {
	switch tok.Kind {
	case TokenIdent, TokenInt, TokenString, TokenRate:
		return fmt.Sprintf("%s %q", tok.Kind, tok.Lexeme)
	case TokenEOF:
		return "end of file"
	case TokenNewline:
		return "end of line"
	default:
		return tok.Kind.String()
	}
}

// syncToNewline consumes tokens up to (but not including) the next
// newline or EOF.
func (p *parser) syncToNewline() {
	for {
		k := p.peek().Kind
		if k == TokenNewline || k == TokenEOF {
			return
		}
		p.advance()
	}
}

// abandonStatement is the statement-level recovery helper: consume
// the remainder of the current line and the trailing newline so the
// next call to parseStatement sees a fresh line.
func (p *parser) abandonStatement() {
	p.syncToNewline()
	if p.peek().Kind == TokenNewline {
		p.advance()
	}
}

// syncTopLevel consumes tokens until the next column-1 token that
// could begin a top-level construct, or EOF. It always advances past
// the current token first so callers that synchronize after failing
// to handle the column-1 token they were dispatched on cannot loop.
func (p *parser) syncTopLevel() {
	if p.peek().Kind != TokenEOF {
		p.advance()
	}
	for {
		cur := p.peek()
		if cur.Kind == TokenEOF {
			return
		}
		if cur.Kind == TokenNewline {
			p.advance()
			continue
		}
		if cur.Span.Start.Column == 1 {
			return
		}
		p.advance()
	}
}

// skipBlankLines consumes consecutive newline tokens.
func (p *parser) skipBlankLines() {
	for p.peek().Kind == TokenNewline {
		p.advance()
	}
}

// expectNewline consumes a NEWLINE if the next token is one;
// otherwise it records an error but does NOT consume an unexpected
// token. EOF satisfies the requirement implicitly.
func (p *parser) expectNewline() bool {
	switch p.peek().Kind {
	case TokenNewline:
		p.advance()
		return true
	case TokenEOF:
		return true
	default:
		p.errCurrent("expected end of line, got %s", describeToken(p.peek()))
		p.abandonStatement()
		return false
	}
}

// parseProgram is the top-level entry point. It always returns a
// non-nil *ast.Program.
func (p *parser) parseProgram() *ast.Program {
	var startPos, endPos ast.Position
	if len(p.tokens) > 0 {
		startPos = p.tokens[0].Span.Start
		endPos = p.tokens[len(p.tokens)-1].Span.End
	} else {
		zero := ast.Position{Source: p.src}
		startPos, endPos = zero, zero
	}
	program := ast.NewProgram(ast.Span{Start: startPos, End: endPos})

	for {
		p.skipBlankLines()
		if p.atEOF() {
			break
		}

		cur := p.peek()
		if cur.Span.Start.Column != 1 {
			p.errAt(cur.Span, "unexpected indentation; top-level constructs start at column 1")
			p.syncTopLevel()
			continue
		}

		if cur.Kind == TokenError {
			p.advance() // already reported by lexer
			continue
		}

		if cur.Kind != TokenIdent {
			p.errAt(cur.Span, "expected top-level construct, got %s", describeToken(cur))
			p.syncTopLevel()
			continue
		}

		switch {
		case cur.Lexeme == "system":
			tok := p.advance()
			if blk := p.parseSystemBlock(tok); blk != nil {
				if program.System != nil {
					p.errAt(blk.Span(), "duplicate system block")
				} else {
					program.System = blk
				}
			}
		case cur.Lexeme == "group":
			tok := p.advance()
			if blk := p.parseGroupBlock(tok); blk != nil {
				program.Groups = append(program.Groups, blk)
			}
		case cur.Lexeme == "errors":
			tok := p.advance()
			if blk := p.parseErrorsBlock(tok); blk != nil {
				program.Errors = append(program.Errors, blk)
			}
		case cur.Lexeme == "include":
			tok := p.advance()
			p.parseIncludeStmt(tok, program)
		case isMethodCandidate(cur):
			tok := p.advance()
			if blk := p.parseHandlerBlock(tok); blk != nil {
				program.Handlers = append(program.Handlers, blk)
			}
		default:
			p.errAt(cur.Span,
				"expected top-level construct (system, group, errors, include, or uppercase HTTP method), got %q",
				cur.Lexeme)
			p.syncTopLevel()
		}
	}

	return program
}

// parseSystemBlock is `system -> NEWLINE Statements`.
func (p *parser) parseSystemBlock(systemTok Token) *ast.SystemBlock {
	if !p.consumeArrowAndNewline("system block") {
		p.syncTopLevel()
		return nil
	}
	stmts := p.parseStatements()
	end := blockEnd(systemTok, nil, stmts)
	if len(stmts) == 0 {
		p.errAt(systemTok.Span, "empty system block: expected at least one statement")
	}
	span := ast.Span{Start: systemTok.Span.Start, End: end}
	blk := ast.NewSystemBlock(span)
	blk.Statements = stmts
	return blk
}

// parseGroupBlock is `group <route> -> NEWLINE Statements`.
func (p *parser) parseGroupBlock(groupTok Token) *ast.GroupBlock {
	pattern := p.parseRoutePattern()
	if !p.consumeArrowAndNewline("group block") {
		p.syncTopLevel()
		return nil
	}
	stmts := p.parseStatements()
	end := blockEnd(groupTok, pattern, stmts)
	if len(stmts) == 0 {
		p.errAt(groupTok.Span, "empty group block: expected at least one statement")
	}
	span := ast.Span{Start: groupTok.Span.Start, End: end}
	blk := ast.NewGroupBlock(span)
	blk.Pattern = pattern
	blk.Statements = stmts
	return blk
}

// parseErrorsBlock is `errors <route> -> NEWLINE ErrorsEntries`.
func (p *parser) parseErrorsBlock(errorsTok Token) *ast.ErrorsBlock {
	pattern := p.parseRoutePattern()
	if !p.consumeArrowAndNewline("errors block") {
		p.syncTopLevel()
		return nil
	}

	var entries []*ast.ErrorsEntry
	for {
		p.skipBlankLines()
		if p.atEOF() {
			break
		}
		cur := p.peek()
		if cur.Span.Start.Column == 1 {
			break
		}
		if entry := p.parseErrorsEntry(); entry != nil {
			entries = append(entries, entry)
		}
	}

	if len(entries) == 0 {
		p.errAt(errorsTok.Span, "empty errors block: expected at least one entry")
	}

	end := errorsTok.Span.End
	if pattern != nil {
		end = pattern.Span().End
	}
	if len(entries) > 0 {
		end = entries[len(entries)-1].Span().End
	}
	span := ast.Span{Start: errorsTok.Span.Start, End: end}
	blk := ast.NewErrorsBlock(span)
	blk.Pattern = pattern
	blk.Entries = entries
	return blk
}

// parseErrorsEntry parses one line of the form `<TypeName> <formatter>`
// or `default <formatter>`. Both type and formatter are simple
// IDENT tokens.
func (p *parser) parseErrorsEntry() *ast.ErrorsEntry {
	typeTok, ok := p.expect(TokenIdent, "error type name")
	if !ok {
		p.abandonStatement()
		return nil
	}
	fmtTok, ok := p.expect(TokenIdent, "formatter name")
	if !ok {
		p.abandonStatement()
		return nil
	}
	p.expectNewline()

	span := ast.Span{Start: typeTok.Span.Start, End: fmtTok.Span.End}
	entry := ast.NewErrorsEntry(span)
	entry.TypeName = typeTok.Lexeme
	entry.TypeSpan = typeTok.Span
	entry.Formatter = fmtTok.Lexeme
	entry.FormatterSpan = fmtTok.Span
	entry.IsDefault = typeTok.Lexeme == "default"
	return entry
}

// parseHandlerBlock is `<METHOD> <route> -> NEWLINE Statements`. The
// method may be hyphenated (e.g., M-SEARCH); methodTok holds the
// first uppercase IDENT, and adjacent MINUS+IDENT pairs extend it.
func (p *parser) parseHandlerBlock(methodTok Token) *ast.HandlerBlock {
	methodName, methodSpan := p.collectMethodName(methodTok)
	pattern := p.parseRoutePattern()
	if !p.consumeArrowAndNewline("handler block") {
		p.syncTopLevel()
		return nil
	}
	stmts := p.parseStatements()
	if len(stmts) == 0 {
		p.errAt(methodSpan, "empty handler block: expected at least one statement")
	}

	var end ast.Position
	switch {
	case len(stmts) > 0:
		end = stmts[len(stmts)-1].Span().End
	case pattern != nil:
		end = pattern.Span().End
	default:
		end = methodSpan.End
	}
	span := ast.Span{Start: methodSpan.Start, End: end}
	blk := ast.NewHandlerBlock(span)
	blk.Method = methodName
	blk.MethodSpan = methodSpan
	blk.Pattern = pattern
	blk.Statements = stmts
	return blk
}

// collectMethodName extends a leading uppercase IDENT with any
// adjacent -IDENT or -INT continuations to support methods like
// M-SEARCH.
func (p *parser) collectMethodName(start Token) (string, ast.Span) {
	var sb strings.Builder
	sb.WriteString(start.Lexeme)
	span := start.Span
	for {
		hyphen := p.peek()
		if hyphen.Kind != TokenMinus || hyphen.Span.Start.Offset != span.End.Offset {
			break
		}
		next := p.peekAt(1)
		if !isMethodPart(next) || next.Span.Start.Offset != hyphen.Span.End.Offset {
			break
		}
		p.advance() // -
		consumed := p.advance()
		sb.WriteByte('-')
		sb.WriteString(consumed.Lexeme)
		span.End = consumed.Span.End
	}
	return sb.String(), span
}

// parseStatements reads zero or more pipeline statements until the
// next column-1 token or EOF.
func (p *parser) parseStatements() []ast.Stmt {
	var stmts []ast.Stmt
	for {
		p.skipBlankLines()
		if p.atEOF() {
			return stmts
		}
		cur := p.peek()
		if cur.Span.Start.Column == 1 {
			return stmts
		}
		if cur.Kind == TokenError {
			p.advance()
			continue
		}
		if stmt := p.parseStatement(); stmt != nil {
			stmts = append(stmts, stmt)
		}
	}
}

// parseStatement dispatches on the leading IDENT lexeme and parses
// one pipeline statement.
func (p *parser) parseStatement() ast.Stmt {
	leading := p.peek()
	if leading.Kind != TokenIdent {
		p.errCurrent("expected statement keyword, got %s", describeToken(leading))
		p.abandonStatement()
		return nil
	}

	switch leading.Lexeme {
	case "log":
		return p.parseArgsStmt(p.advance(), "log")
	case "measure":
		return p.parseArgsStmt(p.advance(), "measure")
	case "session":
		return p.parseSessionStmt(p.advance())
	case "csrf":
		return p.parseCSRFStmt(p.advance())
	case "limit":
		return p.parseLimitStmt(p.advance())
	case "approve":
		return p.parseApproveStmt(p.advance())
	case "resolve":
		return p.parseResolveStmt(p.advance())
	case "commit":
		return p.parseCommitStmt(p.advance())
	case "emit":
		return p.parseEmitStmt(p.advance())
	case "format":
		return p.parseFormatStmt(p.advance())
	case "redirect":
		return p.parseRedirectStmt(p.advance())
	case "layout":
		return p.parseLayoutStmt(p.advance())
	default:
		p.errAt(leading.Span, "unknown statement keyword %q", leading.Lexeme)
		p.abandonStatement()
		return nil
	}
}

// tryParseNoneStmt recognizes the `<stage> none` opt-out form. It
// returns a NoneStmt only when the very next tokens are
// IDENT("none") followed by a newline or EOF.
func (p *parser) tryParseNoneStmt(stage string, stageTok Token) *ast.NoneStmt {
	cur := p.peek()
	if cur.Kind != TokenIdent || cur.Lexeme != "none" {
		return nil
	}
	nxt := p.peekAt(1)
	if nxt.Kind != TokenNewline && nxt.Kind != TokenEOF {
		return nil
	}
	noneTok := p.advance()
	p.expectNewline()
	span := ast.Span{Start: stageTok.Span.Start, End: noneTok.Span.End}
	stmt := ast.NewNoneStmt(span)
	stmt.Stage = stage
	stmt.StageSpan = stageTok.Span
	return stmt
}

// parseArgsStmt handles `log <args>` and `measure <args>`. Args are
// a comma-separated Expr list terminated by NEWLINE.
func (p *parser) parseArgsStmt(stageTok Token, stage string) ast.Stmt {
	if none := p.tryParseNoneStmt(stage, stageTok); none != nil {
		return none
	}
	args := p.parseExprList()
	p.expectNewline()
	end := stageTok.Span.End
	if len(args) > 0 {
		end = args[len(args)-1].Span().End
	}
	span := ast.Span{Start: stageTok.Span.Start, End: end}
	switch stage {
	case "log":
		stmt := ast.NewLogStmt(span)
		stmt.Args = args
		return stmt
	default:
		stmt := ast.NewMeasureStmt(span)
		stmt.Args = args
		return stmt
	}
}

// parseSessionStmt is `session <storage>`.
func (p *parser) parseSessionStmt(sessionTok Token) ast.Stmt {
	if none := p.tryParseNoneStmt("session", sessionTok); none != nil {
		return none
	}
	storageTok, ok := p.expect(TokenIdent, "session storage name")
	if !ok {
		p.abandonStatement()
		return nil
	}
	p.expectNewline()
	span := ast.Span{Start: sessionTok.Span.Start, End: storageTok.Span.End}
	stmt := ast.NewSessionStmt(span)
	stmt.Storage = storageTok.Lexeme
	stmt.StorageSpan = storageTok.Span
	return stmt
}

// parseCSRFStmt is `csrf <mode>`.
func (p *parser) parseCSRFStmt(csrfTok Token) ast.Stmt {
	if none := p.tryParseNoneStmt("csrf", csrfTok); none != nil {
		return none
	}
	modeTok, ok := p.expect(TokenIdent, "csrf mode")
	if !ok {
		p.abandonStatement()
		return nil
	}
	p.expectNewline()
	span := ast.Span{Start: csrfTok.Span.Start, End: modeTok.Span.End}
	stmt := ast.NewCSRFStmt(span)
	stmt.Mode = modeTok.Lexeme
	stmt.ModeSpan = modeTok.Span
	return stmt
}

// parseLimitStmt is `limit <call>`.
func (p *parser) parseLimitStmt(limitTok Token) ast.Stmt {
	if none := p.tryParseNoneStmt("limit", limitTok); none != nil {
		return none
	}
	call := p.parseCall()
	p.expectNewline()
	if call == nil {
		return nil
	}
	span := ast.Span{Start: limitTok.Span.Start, End: call.Span().End}
	stmt := ast.NewLimitStmt(span)
	stmt.Call = call
	return stmt
}

// parseApproveStmt is `approve <expression>` or `approve none`.
func (p *parser) parseApproveStmt(approveTok Token) ast.Stmt {
	if none := p.tryParseNoneStmt("approve", approveTok); none != nil {
		return none
	}
	expr := p.parseApproveExpr()
	p.expectNewline()
	if expr == nil {
		return nil
	}
	span := ast.Span{Start: approveTok.Span.Start, End: expr.Span().End}
	stmt := ast.NewApproveStmt(span)
	stmt.Expr = expr
	return stmt
}

// parseResolveStmt is `resolve <name> = <call>`.
func (p *parser) parseResolveStmt(resolveTok Token) ast.Stmt {
	nameTok, ok := p.expect(TokenIdent, "resolve name")
	if !ok {
		p.abandonStatement()
		return nil
	}
	if _, ok := p.expect(TokenEquals, "'='"); !ok {
		p.abandonStatement()
		return nil
	}
	call := p.parseCall()
	p.expectNewline()
	if call == nil {
		return nil
	}
	span := ast.Span{Start: resolveTok.Span.Start, End: call.Span().End}
	stmt := ast.NewResolveStmt(span)
	stmt.Name = nameTok.Lexeme
	stmt.NameSpan = nameTok.Span
	stmt.Call = call
	return stmt
}

// parseCommitStmt is `commit [<name> =] <call>`.
func (p *parser) parseCommitStmt(commitTok Token) ast.Stmt {
	var nameTok Token
	hasName := false
	if p.peek().Kind == TokenIdent && p.peekAt(1).Kind == TokenEquals {
		nameTok = p.advance()
		p.advance() // '='
		hasName = true
	}
	call := p.parseCall()
	p.expectNewline()
	if call == nil {
		return nil
	}
	stmt := ast.NewCommitStmt(ast.Span{Start: commitTok.Span.Start, End: call.Span().End})
	if hasName {
		stmt.Name = nameTok.Lexeme
		stmt.NameSpan = nameTok.Span
	}
	stmt.Call = call
	return stmt
}

// parseEmitStmt is `emit <event-name> [with <data-name>]`.
func (p *parser) parseEmitStmt(emitTok Token) ast.Stmt {
	eventTok, ok := p.expect(TokenIdent, "event name")
	if !ok {
		p.abandonStatement()
		return nil
	}
	endPos := eventTok.Span.End
	var dataTok Token
	hasData := false
	if p.peek().Kind == TokenIdent && p.peek().Lexeme == "with" {
		p.advance()
		t, ok := p.expect(TokenIdent, "event data name")
		if ok {
			dataTok = t
			hasData = true
			endPos = t.Span.End
		}
	}
	p.expectNewline()

	span := ast.Span{Start: emitTok.Span.Start, End: endPos}
	stmt := ast.NewEmitStmt(span)
	stmt.Event = eventTok.Lexeme
	stmt.EventSpan = eventTok.Span
	if hasData {
		stmt.Data = dataTok.Lexeme
		stmt.DataSpan = dataTok.Span
	}
	return stmt
}

// parseFormatStmt is `format <template> [with <data-list>] [using layout <name>]`.
func (p *parser) parseFormatStmt(formatTok Token) ast.Stmt {
	if none := p.tryParseNoneStmt("format", formatTok); none != nil {
		return none
	}
	tmplTok, ok := p.expect(TokenIdent, "format template name")
	if !ok {
		p.abandonStatement()
		return nil
	}

	var data []ast.NamedRef
	endPos := tmplTok.Span.End
	if p.peek().Kind == TokenIdent && p.peek().Lexeme == "with" {
		p.advance()
		for {
			ref, ok := p.parseNamedRef()
			if !ok {
				break
			}
			data = append(data, ref)
			endPos = ref.Span().End
			if p.peek().Kind != TokenComma {
				break
			}
			p.advance()
		}
	}

	var layoutName string
	var layoutSpan ast.Span
	if p.peek().Kind == TokenIdent && p.peek().Lexeme == "using" {
		p.advance()
		if p.peek().Kind == TokenIdent && p.peek().Lexeme == "layout" {
			p.advance()
		} else {
			p.errCurrent("expected 'layout' after 'using'")
		}
		layoutTok, ok := p.expect(TokenIdent, "layout name")
		if ok {
			layoutName = layoutTok.Lexeme
			layoutSpan = layoutTok.Span
			endPos = layoutTok.Span.End
		}
	}
	p.expectNewline()

	span := ast.Span{Start: formatTok.Span.Start, End: endPos}
	stmt := ast.NewFormatStmt(span)
	stmt.Template = tmplTok.Lexeme
	stmt.TemplateSpan = tmplTok.Span
	stmt.Data = data
	stmt.Layout = layoutName
	stmt.LayoutSpan = layoutSpan
	return stmt
}

// parseRedirectStmt is `redirect <url-template>`. The URL is captured
// verbatim from the first token after `redirect` to the last token
// before NEWLINE.
func (p *parser) parseRedirectStmt(redirectTok Token) ast.Stmt {
	if p.peek().Kind == TokenNewline || p.peek().Kind == TokenEOF {
		p.errCurrent("redirect requires a URL")
		p.expectNewline()
		return nil
	}
	urlStartTok := p.peek()
	urlEndPos := urlStartTok.Span.End
	for p.peek().Kind != TokenNewline && p.peek().Kind != TokenEOF {
		urlEndPos = p.peek().Span.End
		p.advance()
	}
	p.expectNewline()
	urlSpan := ast.Span{Start: urlStartTok.Span.Start, End: urlEndPos}
	url := string(p.src.Bytes[urlSpan.Start.Offset:urlSpan.End.Offset])
	span := ast.Span{Start: redirectTok.Span.Start, End: urlEndPos}
	stmt := ast.NewRedirectStmt(span)
	stmt.URL = url
	stmt.URLSpan = urlSpan
	return stmt
}

// parseLayoutStmt is `layout <name>`.
func (p *parser) parseLayoutStmt(layoutTok Token) ast.Stmt {
	if none := p.tryParseNoneStmt("layout", layoutTok); none != nil {
		return none
	}
	nameTok, ok := p.expect(TokenIdent, "layout name")
	if !ok {
		p.abandonStatement()
		return nil
	}
	p.expectNewline()
	span := ast.Span{Start: layoutTok.Span.Start, End: nameTok.Span.End}
	stmt := ast.NewLayoutStmt(span)
	stmt.Name = nameTok.Lexeme
	stmt.NameSpan = nameTok.Span
	return stmt
}

// parseIncludeStmt resolves an `include <path>` directive: it reads
// the path tokens (one or more adjacent IDENT/INT/SLASH/MINUS pieces
// — paths may contain subdirectories), validates the .writ
// extension, resolves the path against the current source's
// directory within session.cfg.fsys, detects cycles, opens and
// recursively parses the included file, then inlines the included
// program's blocks into program. A `system` block in an included
// file is reported as an error and not merged.
func (p *parser) parseIncludeStmt(includeTok Token, program *ast.Program) {
	if p.peek().Kind == TokenNewline || p.peek().Kind == TokenEOF {
		p.errCurrent("include requires a path")
		p.expectNewline()
		return
	}

	pathStartTok := p.peek()
	pathEndPos := pathStartTok.Span.End
	for {
		k := p.peek().Kind
		if k == TokenNewline || k == TokenEOF {
			break
		}
		if k != TokenIdent && k != TokenInt && k != TokenSlash && k != TokenMinus {
			p.errAt(p.peek().Span, "unexpected %s in include path", describeToken(p.peek()))
			break
		}
		pathEndPos = p.peek().Span.End
		p.advance()
	}
	p.expectNewline()

	pathSpan := ast.Span{Start: pathStartTok.Span.Start, End: pathEndPos}
	includePath := string(p.src.Bytes[pathSpan.Start.Offset:pathSpan.End.Offset])
	if !strings.HasSuffix(includePath, ".writ") {
		p.errAt(pathSpan, `include path %q must end in ".writ"`, includePath)
		return
	}

	if p.session.cfg.fsys == nil {
		p.errAt(includeTok.Span, "include not supported: no filesystem configured")
		return
	}

	currentDir := path.Dir(p.relPath)
	if p.relPath == "" || currentDir == "." {
		currentDir = ""
	}
	resolved := path.Clean(path.Join(currentDir, includePath))

	if slices.Contains(p.session.cycleStack, resolved) {
		chain := append([]string{}, p.session.cycleStack...)
		chain = append(chain, resolved)
		p.errAt(pathSpan, "include cycle: %s", strings.Join(chain, " -> "))
		return
	}

	bytes, err := fs.ReadFile(p.session.cfg.fsys, resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			p.errAt(includeTok.Span, "include %q: file not found (resolved to %s)", includePath, resolved)
		} else {
			p.errAt(includeTok.Span, "cannot read include %q: %v", includePath, err)
		}
		return
	}

	src := &ast.Source{Path: resolved, Bytes: bytes}
	p.session.sources = append(p.session.sources, src)
	p.session.cycleStack = append(p.session.cycleStack, resolved)

	sub := newParser(src, resolved, p.session)
	subProgram := sub.parseProgram()

	p.session.cycleStack = p.session.cycleStack[:len(p.session.cycleStack)-1]

	if subProgram.System != nil {
		sub.errAt(subProgram.System.Span(), "system block is not allowed in an included file")
	}
	program.Groups = append(program.Groups, subProgram.Groups...)
	program.Errors = append(program.Errors, subProgram.Errors...)
	program.Handlers = append(program.Handlers, subProgram.Handlers...)
}

// parseRoutePattern is `/` followed by zero or more slash-separated
// segments. Empty segments and trailing slashes are errors. Only the
// final segment may be a wildcard (`*`).
func (p *parser) parseRoutePattern() *ast.RoutePattern {
	if p.peek().Kind != TokenSlash {
		p.errCurrent("expected route pattern starting with /")
		return nil
	}
	leadingSlash := p.advance()
	pat := ast.NewRoutePattern(leadingSlash.Span)

	if p.peek().Kind == TokenArrow || p.peek().Kind == TokenNewline || p.peek().Kind == TokenEOF {
		return pat
	}

	if p.peek().Kind == TokenSlash {
		p.errAt(p.peek().Span, "empty route segment")
		p.advance() // consume the second / and continue
	}

	var segments []ast.RouteSegment
	endPos := leadingSlash.Span.End
	first := p.parseRouteSegment()
	if first != nil {
		segments = append(segments, first)
		endPos = first.Span().End
	}

	for p.peek().Kind == TokenSlash {
		slashTok := p.advance()
		if p.peek().Kind == TokenArrow || p.peek().Kind == TokenNewline || p.peek().Kind == TokenEOF {
			p.errAt(slashTok.Span, "trailing slash in route pattern")
			break
		}
		if p.peek().Kind == TokenSlash {
			p.errAt(p.peek().Span, "empty route segment")
			continue
		}
		if last := segments; len(last) > 0 {
			if _, isWild := last[len(last)-1].(*ast.WildcardSegment); isWild {
				p.errAt(last[len(last)-1].Span(), "wildcard segment must be the final segment")
			}
		}
		seg := p.parseRouteSegment()
		if seg != nil {
			segments = append(segments, seg)
			endPos = seg.Span().End
		}
	}

	pat.Segments = segments
	span := ast.Span{Start: leadingSlash.Span.Start, End: endPos}
	out := ast.NewRoutePattern(span)
	out.Segments = segments
	return out
}

func (p *parser) parseRouteSegment() ast.RouteSegment {
	cur := p.peek()
	switch cur.Kind {
	case TokenStar:
		p.advance()
		return ast.NewWildcardSegment(cur.Span)
	case TokenColon:
		colonTok := p.advance()
		return p.parseParameterSegment(colonTok)
	case TokenIdent, TokenInt:
		return p.parseLiteralSegment()
	default:
		p.errAt(cur.Span, "expected route segment, got %s", describeToken(cur))
		return nil
	}
}

// parseLiteralSegment combines adjacent IDENT/INT tokens (with
// optional internal MINUS hyphens) into one route-segment literal.
// Tokens must be byte-adjacent — any whitespace between them ends
// the segment.
func (p *parser) parseLiteralSegment() *ast.LiteralSegment {
	first := p.advance()
	var sb strings.Builder
	sb.WriteString(first.Lexeme)
	endSpan := first.Span
	for {
		cur := p.peek()
		if cur.Kind == TokenMinus && cur.Span.Start.Offset == endSpan.End.Offset {
			next := p.peekAt(1)
			if (next.Kind != TokenIdent && next.Kind != TokenInt) ||
				next.Span.Start.Offset != cur.Span.End.Offset {
				break
			}
			p.advance() // -
			consumed := p.advance()
			sb.WriteByte('-')
			sb.WriteString(consumed.Lexeme)
			endSpan = consumed.Span
			continue
		}
		if (cur.Kind == TokenIdent || cur.Kind == TokenInt) &&
			cur.Span.Start.Offset == endSpan.End.Offset {
			sb.WriteString(cur.Lexeme)
			endSpan = cur.Span
			p.advance()
			continue
		}
		break
	}
	span := ast.Span{Start: first.Span.Start, End: endSpan.End}
	return ast.NewLiteralSegment(span, sb.String())
}

func (p *parser) parseParameterSegment(colonTok Token) *ast.ParameterSegment {
	if p.peek().Kind != TokenIdent && p.peek().Kind != TokenInt {
		p.errCurrent("expected parameter name after ':'")
		return nil
	}
	first := p.advance()
	if first.Span.Start.Offset != colonTok.Span.End.Offset {
		// Whitespace between : and name — still recover but flag.
		p.errAt(colonTok.Span, "no whitespace allowed between ':' and parameter name")
	}
	var sb strings.Builder
	sb.WriteString(first.Lexeme)
	endSpan := first.Span
	for {
		cur := p.peek()
		if cur.Kind == TokenMinus && cur.Span.Start.Offset == endSpan.End.Offset {
			next := p.peekAt(1)
			if (next.Kind != TokenIdent && next.Kind != TokenInt) ||
				next.Span.Start.Offset != cur.Span.End.Offset {
				break
			}
			p.advance()
			consumed := p.advance()
			sb.WriteByte('-')
			sb.WriteString(consumed.Lexeme)
			endSpan = consumed.Span
			continue
		}
		if (cur.Kind == TokenIdent || cur.Kind == TokenInt) &&
			cur.Span.Start.Offset == endSpan.End.Offset {
			sb.WriteString(cur.Lexeme)
			endSpan = cur.Span
			p.advance()
			continue
		}
		break
	}
	span := ast.Span{Start: colonTok.Span.Start, End: endSpan.End}
	return ast.NewParameterSegment(span, sb.String())
}

// parseCall is `<name>` or `<name>(<args>)`. Calls without parens
// are allowed; their Args slice is empty.
func (p *parser) parseCall() *ast.Call {
	nameTok, ok := p.expect(TokenIdent, "call name")
	if !ok {
		p.syncToNewline()
		return nil
	}
	call := ast.NewCall(nameTok.Span)
	call.Name = nameTok.Lexeme
	call.NameSpan = nameTok.Span
	if p.peek().Kind != TokenLParen {
		return call
	}
	p.advance() // '('
	if p.peek().Kind != TokenRParen {
		args := p.parseExprList()
		call.Args = args
	}
	rparen, ok := p.expect(TokenRParen, "')'")
	if !ok {
		return call
	}
	span := ast.Span{Start: nameTok.Span.Start, End: rparen.Span.End}
	out := ast.NewCall(span)
	out.Name = call.Name
	out.NameSpan = call.NameSpan
	out.Args = call.Args
	return out
}

// parseExprList reads a comma-separated list of expressions until
// it hits a non-expression token (NEWLINE, EOF, RPAREN, or an
// unexpected keyword position).
func (p *parser) parseExprList() []ast.Expr {
	var out []ast.Expr
	for startsExpr(p.peek()) {
		expr := p.parseExpr()
		if expr == nil {
			break
		}
		out = append(out, expr)
		if p.peek().Kind != TokenComma {
			break
		}
		p.advance() // ','
	}
	return out
}

// startsExpr reports whether the given token can begin an expression.
func startsExpr(tok Token) bool {
	switch tok.Kind {
	case TokenColon, TokenInt, TokenString, TokenRate, TokenMinus:
		return true
	case TokenIdent:
		return true
	default:
		return false
	}
}

// parseExpr parses one expression — a value reference, a literal, a
// named arg, a body/query reference, or a Call (the latter is used
// inside parseApproveExpr's primary branch via parseCall).
func (p *parser) parseExpr() ast.Expr {
	cur := p.peek()
	switch cur.Kind {
	case TokenColon:
		return p.parseColonRef()
	case TokenInt:
		intTok := p.advance()
		val, err := strconv.ParseInt(intTok.Lexeme, 10, 64)
		if err != nil {
			p.errAt(intTok.Span, "integer literal %q out of range", intTok.Lexeme)
			return nil
		}
		return ast.NewIntLit(intTok.Span, val)
	case TokenMinus:
		minusTok := p.advance()
		if p.peek().Kind != TokenInt {
			p.errCurrent("expected integer after '-'")
			return nil
		}
		intTok := p.advance()
		if intTok.Span.Start.Offset != minusTok.Span.End.Offset {
			p.errAt(minusTok.Span, "no whitespace allowed between '-' and integer")
		}
		val, err := strconv.ParseInt("-"+intTok.Lexeme, 10, 64)
		if err != nil {
			p.errAt(intTok.Span, "integer literal %q out of range", "-"+intTok.Lexeme)
			return nil
		}
		span := ast.Span{Start: minusTok.Span.Start, End: intTok.Span.End}
		return ast.NewIntLit(span, val)
	case TokenString:
		strTok := p.advance()
		return ast.NewStringLit(strTok.Span, strTok.Lexeme)
	case TokenRate:
		rateTok := p.advance()
		return ast.NewRateLit(rateTok.Span, rateTok.RateCount, rateTok.RateUnit)
	case TokenIdent:
		return p.parseIdentExpr()
	default:
		p.errCurrent("expected expression, got %s", describeToken(cur))
		return nil
	}
}

// parseColonRef handles `:name` (RouteParamRef) and `:name.field...`
// (FieldRef). The lexer emits the identifier after `:` as a single
// dotted IDENT, so we split on `.` to decide which kind to build.
func (p *parser) parseColonRef() ast.Expr {
	colonTok := p.advance()
	if p.peek().Kind != TokenIdent {
		p.errCurrent("expected identifier after ':'")
		return nil
	}
	identTok := p.advance()
	if identTok.Span.Start.Offset != colonTok.Span.End.Offset {
		p.errAt(colonTok.Span, "no whitespace allowed between ':' and identifier")
	}

	parts := strings.Split(identTok.Lexeme, ".")
	span := ast.Span{Start: colonTok.Span.Start, End: identTok.Span.End}
	if len(parts) == 1 {
		ref := ast.NewRouteParamRef(span)
		ref.Name = parts[0]
		ref.NameSpan = identTok.Span
		return ref
	}

	root, rootSpan, pathSpans := splitDottedSpans(identTok)
	ref := ast.NewFieldRef(span)
	ref.Root = root
	ref.RootSpan = rootSpan
	ref.Path = parts[1:]
	ref.PathSpans = pathSpans[1:]
	return ref
}

// parseIdentExpr handles the four IDENT-led expression forms:
// NamedArg (`name=literal`), BodyRef (`body Type`), QueryRef
// (`query Type`), and otherwise an error.
func (p *parser) parseIdentExpr() ast.Expr {
	identTok := p.peek()

	if identTok.Lexeme == "body" || identTok.Lexeme == "query" {
		// Type reference: keyword IDENT.
		nxt := p.peekAt(1)
		if nxt.Kind == TokenIdent {
			p.advance()
			typeTok := p.advance()
			span := ast.Span{Start: identTok.Span.Start, End: typeTok.Span.End}
			if identTok.Lexeme == "body" {
				ref := ast.NewBodyRef(span)
				ref.TypeName = typeTok.Lexeme
				ref.TypeSpan = typeTok.Span
				return ref
			}
			ref := ast.NewQueryRef(span)
			ref.TypeName = typeTok.Lexeme
			ref.TypeSpan = typeTok.Span
			return ref
		}
	}

	if p.peekAt(1).Kind == TokenEquals {
		nameTok := p.advance()
		p.advance() // '='
		value := p.parseLiteral()
		if value == nil {
			return nil
		}
		span := ast.Span{Start: nameTok.Span.Start, End: value.Span().End}
		arg := ast.NewNamedArg(span)
		arg.Name = nameTok.Lexeme
		arg.NameSpan = nameTok.Span
		arg.Value = value
		return arg
	}

	p.errAt(identTok.Span,
		"unexpected identifier %q in argument position; expected ':name', 'name=value', 'body Type', or 'query Type'",
		identTok.Lexeme)
	p.advance()
	return nil
}

// parseLiteral parses an int/string/rate literal.
func (p *parser) parseLiteral() ast.Literal {
	cur := p.peek()
	switch cur.Kind {
	case TokenInt:
		tok := p.advance()
		val, err := strconv.ParseInt(tok.Lexeme, 10, 64)
		if err != nil {
			p.errAt(tok.Span, "integer literal %q out of range", tok.Lexeme)
			return nil
		}
		return ast.NewIntLit(tok.Span, val)
	case TokenMinus:
		minusTok := p.advance()
		if p.peek().Kind != TokenInt {
			p.errCurrent("expected integer after '-'")
			return nil
		}
		intTok := p.advance()
		val, err := strconv.ParseInt("-"+intTok.Lexeme, 10, 64)
		if err != nil {
			p.errAt(intTok.Span, "integer literal %q out of range", "-"+intTok.Lexeme)
			return nil
		}
		span := ast.Span{Start: minusTok.Span.Start, End: intTok.Span.End}
		return ast.NewIntLit(span, val)
	case TokenString:
		tok := p.advance()
		return ast.NewStringLit(tok.Span, tok.Lexeme)
	case TokenRate:
		tok := p.advance()
		return ast.NewRateLit(tok.Span, tok.RateCount, tok.RateUnit)
	default:
		p.errCurrent("expected literal, got %s", describeToken(cur))
		return nil
	}
}

// parseNamedRef parses one entry of a `with` data list — either a
// bare identifier or a dotted path.
func (p *parser) parseNamedRef() (ast.NamedRef, bool) {
	if p.peek().Kind != TokenIdent {
		p.errCurrent("expected data reference name")
		return ast.NamedRef{}, false
	}
	tok := p.advance()
	parts := strings.Split(tok.Lexeme, ".")
	ref := ast.NewNamedRef(tok.Span)
	if len(parts) == 1 {
		ref.Name = parts[0]
	} else {
		_, _, pathSpans := splitDottedSpans(tok)
		ref.Path = parts
		ref.PathSpans = pathSpans
	}
	return ref, true
}

// parseApproveExpr is the entry point of the precedence-climbing
// tower for approve expressions: NOT (right-assoc) > AND (left) > OR (left).
func (p *parser) parseApproveExpr() ast.ApproveExpr {
	return p.parseOrExpr()
}

func (p *parser) parseOrExpr() ast.ApproveExpr {
	left := p.parseAndExpr()
	for left != nil && p.peek().Kind == TokenIdent && p.peek().Lexeme == "OR" {
		p.advance()
		right := p.parseAndExpr()
		if right == nil {
			return left
		}
		span := ast.Span{Start: left.Span().Start, End: right.Span().End}
		left = ast.NewApproveOr(span, left, right)
	}
	return left
}

func (p *parser) parseAndExpr() ast.ApproveExpr {
	left := p.parseNotExpr()
	for left != nil && p.peek().Kind == TokenIdent && p.peek().Lexeme == "AND" {
		p.advance()
		right := p.parseNotExpr()
		if right == nil {
			return left
		}
		span := ast.Span{Start: left.Span().Start, End: right.Span().End}
		left = ast.NewApproveAnd(span, left, right)
	}
	return left
}

func (p *parser) parseNotExpr() ast.ApproveExpr {
	if p.peek().Kind == TokenIdent && p.peek().Lexeme == "NOT" {
		notTok := p.advance()
		inner := p.parseNotExpr()
		if inner == nil {
			return nil
		}
		span := ast.Span{Start: notTok.Span.Start, End: inner.Span().End}
		return ast.NewApproveNot(span, inner)
	}
	return p.parsePrimaryExpr()
}

func (p *parser) parsePrimaryExpr() ast.ApproveExpr {
	if p.peek().Kind == TokenLParen {
		lparen := p.advance()
		inner := p.parseOrExpr()
		rparen, ok := p.expect(TokenRParen, "')'")
		if !ok {
			return inner
		}
		if inner == nil {
			return nil
		}
		// The tree shape records parenthesization implicitly; we
		// just widen the inner expression's span to include the
		// parens for diagnostics.
		_ = lparen
		_ = rparen
		return inner
	}
	if p.peek().Kind != TokenIdent {
		p.errCurrent("expected approver call or '(', got %s", describeToken(p.peek()))
		return nil
	}
	call := p.parseCall()
	if call == nil {
		return nil
	}
	return ast.NewApproveCall(call.Span(), call)
}

// consumeArrowAndNewline expects `-> NEWLINE` and recovers if either
// is missing. Returns false if the arrow was absent (caller should
// abandon the current block); a missing newline is recovered locally.
func (p *parser) consumeArrowAndNewline(blockKind string) bool {
	if p.peek().Kind != TokenArrow {
		p.errCurrent("expected '->' to open %s, got %s", blockKind, describeToken(p.peek()))
		return false
	}
	p.advance()
	p.expectNewline()
	return true
}

// blockEnd computes the end position for a block based on its
// header/pattern/statements, falling back through the chain when
// later artifacts are absent.
func blockEnd(headerTok Token, pattern *ast.RoutePattern, stmts []ast.Stmt) ast.Position {
	if len(stmts) > 0 {
		return stmts[len(stmts)-1].Span().End
	}
	if pattern != nil {
		return pattern.Span().End
	}
	return headerTok.Span.End
}

// isMethodCandidate reports whether tok could start a handler block
// — an IDENT whose lexeme matches `[A-Z][A-Z0-9]*` (the leading
// segment of `[A-Z][A-Z0-9-]*`; collectMethodName extends with
// hyphenated continuations).
func isMethodCandidate(tok Token) bool {
	return tok.Kind == TokenIdent && isMethodSegment(tok.Lexeme)
}

func isMethodPart(tok Token) bool {
	return tok.Kind == TokenIdent && isMethodSegment(tok.Lexeme)
}

func isMethodSegment(s string) bool {
	if s == "" {
		return false
	}
	if s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		return false
	}
	return true
}

// splitDottedSpans returns the root segment plus per-segment Spans
// for a dotted identifier token. The token's lexeme is split on '.'
// and each part's span is computed from the token's start position;
// dotted identifiers always live on a single line.
func splitDottedSpans(tok Token) (root string, rootSpan ast.Span, pathSpans []ast.Span) {
	parts := strings.Split(tok.Lexeme, ".")
	spans := make([]ast.Span, len(parts))
	off := 0
	line := tok.Span.Start.Line
	col := tok.Span.Start.Column
	src := tok.Span.Start.Source
	baseOff := tok.Span.Start.Offset
	for i, part := range parts {
		startCol := col + off
		startOff := baseOff + off
		endCol := startCol + len(part)
		endOff := startOff + len(part)
		spans[i] = ast.Span{
			Start: ast.Position{Source: src, Line: line, Column: startCol, Offset: startOff},
			End:   ast.Position{Source: src, Line: line, Column: endCol, Offset: endOff},
		}
		off += len(part) + 1 // trailing '.'
	}
	return parts[0], spans[0], spans
}
