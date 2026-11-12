package parser

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SQLType represents the kind of SQL statement.
type SQLType string

const (
	SQLSelect        SQLType = "select"
	SQLInsert        SQLType = "insert"
	SQLUpdate        SQLType = "update"
	SQLDelete        SQLType = "delete"
	SQLDynamicSelect SQLType = "dynamicselect"
	SQLDynamicUpdate SQLType = "dynamicupdate"
	SQLDynamicInsert SQLType = "dynamicinsert"
)

// SQLStatement holds a parsed SQL statement with its metadata.
type SQLStatement struct {
	File    string
	Sys     string
	ModName string
	SQLID   string
	SQLType SQLType
	OldSQL  string
	TestSQL string
	Matched string
	ErrMsg  string
}

// ParseResult holds all parsed statements from a directory.
type ParseResult struct {
	Statements []*SQLStatement
	SQLErrors  []string
}

func isSQLTag(name string) bool {
	switch SQLType(strings.ToLower(name)) {
	case SQLSelect, SQLInsert, SQLUpdate, SQLDelete,
		SQLDynamicSelect, SQLDynamicUpdate, SQLDynamicInsert:
		return true
	}
	return false
}

// isIBatisCondTag reports whether name is one of the iBATIS 2.x conditional tags.
// These all share one code path (condition treated as true → body is included),
// so the family is handled together rather than per-tag — a tag added later would
// otherwise fall back into the "unknown tag" path and silently drop connectors.
// Tags marked TODO below have not yet been observed in real mappers but behave
// identically; they are accepted so they never degrade to connector-loss.
func isIBatisCondTag(name string) bool {
	switch strings.ToLower(name) {
	case "isnotempty", "isempty",
		"isnotnull", "isnull",
		"isequal", "isnotequal",
		// TODO: unobserved in real samples yet — same expansion path, kept supported.
		"isgreaterthan", "isgreaterequal",
		"islessthan", "islessequal",
		"ispropertyavailable", "isnotpropertyavailable",
		"isparameterpresent", "isnotparameterpresent":
		return true
	}
	return false
}

// normalizeConnector trims and re-spaces a prepend attribute value so it can be
// concatenated safely: "AND", " and ", "AND " → "AND "; "," → ", ".
func normalizeConnector(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return s + " "
}

// attrValue returns the value of the named attribute (case-insensitive), or "".
func attrValue(t xml.StartElement, want string) string {
	for _, a := range t.Attr {
		if strings.ToLower(a.Name.Local) == want {
			return a.Value
		}
	}
	return ""
}

// sqlFragmentMap holds collected <sql id="..."> fragments.
type sqlFragmentMap map[string]string

// xmlParser handles stateful XML parsing with MyBatis dynamic tag support.
type xmlParser struct {
	file       string
	statements []*SQLStatement
	fragments  sqlFragmentMap
	errors     []string

	// SQL statement state
	inSQL       bool
	currentType SQLType
	currentID   string
	currentText strings.Builder

	// Dynamic tag state
	inWhere     bool
	inSet       bool
	inAnd       bool
	andPending  bool // next charData inside <and> needs "\n and " prefix
	inChoose    bool
	inWhen      bool
	inOtherwise bool
	chooseDepth int

	// Choose branch buffers (separate from currentText to prevent duplication)
	whenBuf      strings.Builder
	otherwiseBuf strings.Builder
	whenSeen     int
	whenID       string

	// Trim accumulator
	trimPrefix          string
	trimSuffix          string
	trimPrefixOverrides string
	trimSuffixOverrides string

	// iBATIS 2.x dynamic / conditional / iterate state.
	// iBATIS maps a set of conditional tags (isNotEmpty, isEmpty, isNotNull,
	// isNull, isEqual, isNotEqual, isGreaterThan, ... see isIBatisCondTag) plus a
	// <dynamic prepend="..."> container and an <iterate open close conjunction>
	// list. Connectors (prepend) were previously dropped, producing broken SQL.
	inDynamic          bool   // inside <dynamic>
	dynamicPrepend     string // normalized connector from <dynamic prepend>
	dynamicUsed        bool   // the <dynamic> prepend has been consumed by a child
	inIBatisCond       bool   // inside an iBATIS conditional tag
	isBodyStarted      bool   // current conditional tag already emitted content
	pendingConn        string // connector to emit before the next body content
	pendingFromDynamic bool   // pending connector came from the enclosing <dynamic>
	inIterate          bool   // inside <iterate>: inner #param# is swallowed
}

func newXMLParser(file string) *xmlParser {
	return &xmlParser{
		file:      file,
		fragments: make(sqlFragmentMap),
	}
}

func parseFile(xmlFile string) (*ParseResult, error) {
	data, err := os.ReadFile(xmlFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", xmlFile, err)
	}

	p := newXMLParser(xmlFile)

	// Pass 1: collect <sql> fragments
	p.collectFragments(data)

	// Pass 2: extract SQL statements
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose

	for {
		tok, err := decoder.RawToken()
		if err != nil {
			break
		}
		p.processToken(tok)
	}

	modName := moduleName(xmlFile)
	for _, stmt := range p.statements {
		stmt.ModName = modName
	}

	return &ParseResult{
		Statements: p.statements,
		SQLErrors:  p.errors,
	}, nil
}

func moduleName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// collectFragments — Pass 1: find all <sql id="X"> elements.
func (p *xmlParser) collectFragments(data []byte) {
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose

	var inSQL bool
	var sqlID string
	var content strings.Builder
	depth := 0

	for {
		tok, err := decoder.RawToken()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if strings.ToLower(t.Name.Local) == "sql" {
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Name.Local) == "id" {
						sqlID = attr.Value
						break
					}
				}
				inSQL = true
				content.Reset()
			}
			if inSQL {
				depth++
			}
		case xml.EndElement:
			if inSQL {
				depth--
				if strings.ToLower(t.Name.Local) == "sql" && depth == 0 {
					p.fragments[sqlID] = content.String()
					inSQL = false
				}
			}
		case xml.CharData:
			if inSQL {
				content.Write(t)
			}
		}
	}
}

func (p *xmlParser) processToken(tok xml.Token) {
	switch t := tok.(type) {
	case xml.StartElement:
		p.handleStartElement(t)
	case xml.EndElement:
		p.handleEndElement(t)
	case xml.CharData:
		p.handleCharData(t)
	}
}

func (p *xmlParser) handleStartElement(t xml.StartElement) {
	name := strings.ToLower(t.Name.Local)

	if isSQLTag(name) {
		p.inSQL = true
		p.currentType = SQLType(name)
		p.currentText.Reset()
		p.currentID = ""
		p.inWhere = false
		p.inSet = false
		p.inAnd = false
		p.andPending = false
		p.inChoose = false
		p.inWhen = false
		p.inOtherwise = false
		p.chooseDepth = 0
		p.whenSeen = 0
		p.whenID = ""
		p.whenBuf.Reset()
		p.otherwiseBuf.Reset()
		p.trimPrefix = ""
		p.trimSuffix = ""
		p.trimPrefixOverrides = ""
		p.trimSuffixOverrides = ""
		p.inDynamic = false
		p.dynamicPrepend = ""
		p.dynamicUsed = false
		p.inIBatisCond = false
		p.isBodyStarted = false
		p.pendingConn = ""
		p.pendingFromDynamic = false
		p.inIterate = false

		for _, attr := range t.Attr {
			if strings.ToLower(attr.Name.Local) == "id" {
				p.currentID = attr.Value
			}
		}
		return
	}

	if !p.inSQL {
		return
	}

	// iBATIS 2.x constructs (handled before the MyBatis switch).
	if name == "dynamic" {
		p.handleDynamicStart(t)
		return
	}
	if name == "iterate" {
		p.handleIterateStart(t)
		return
	}
	if isIBatisCondTag(name) {
		p.handleCondStart(t)
		return
	}

	switch name {
	case "where":
		if !strings.Contains(p.currentText.String(), "\nwhere ") {
			p.currentText.WriteString(" where ")
		}
		p.inWhere = true
	case "set":
		p.currentText.WriteString(" set ")
		p.inSet = true
	case "and":
		p.inAnd = true
		p.andPending = true
	case "choose":
		p.inChoose = true
		p.chooseDepth++
		p.whenSeen = 0
		p.whenBuf.Reset()
		p.otherwiseBuf.Reset()
	case "when":
		if p.inChoose {
			p.inWhen = true
			p.whenSeen++
			p.whenBuf.Reset()
			for _, attr := range t.Attr {
				if strings.ToLower(attr.Name.Local) == "id" {
					p.whenID = attr.Value
				}
			}
		}
	case "otherwise":
		if p.inChoose {
			p.inOtherwise = true
			p.otherwiseBuf.Reset()
		}
	case "foreach":
		p.handleForeach(t)
	case "trim":
		p.handleTrim(t)
	case "include":
		p.handleInclude(t)
	case "if":
		// Reset andPending for each child inside <and>
		if p.inAnd {
			p.andPending = true
		}
	case "str":
		if p.inAnd {
			p.andPending = true
		}
	case "bind":
		// OGNL ignored
	}
}

// handleDynamicStart opens an iBATIS <dynamic prepend="..."> container. The
// container emits nothing itself: the first child that produces content (a
// conditional tag, <iterate>, or raw text) consumes the container's prepend as
// its connector (iBATIS applies <dynamic> prepend to the first child and ignores
// that child's own prepend), e.g.
//
//	where 1=1 <dynamic prepend="AND"><isNotEmpty prepend="AND" p="a">a=#a#</isNotEmpty>...
//	→ where 1=1 AND a='x' ...
//
//	<dynamic prepend="WHERE"><isNotEmpty p="a">and a=#a#</isNotEmpty>...</dynamic>
//	→ ... WHERE a='x' and ...
func (p *xmlParser) handleDynamicStart(t xml.StartElement) {
	p.inDynamic = true
	p.dynamicUsed = false
	p.dynamicPrepend = normalizeConnector(attrValue(t, "prepend"))
}

// handleCondStart opens an iBATIS conditional tag (isNotEmpty/isNull/isEqual/...).
// The condition is treated as true — the body is always included — matching the
// existing <if> expansion policy. Exactly one connector is emitted per block (R2):
// the prepend (or the enclosing <dynamic>'s prepend for the first child) wins, and
// a redundant literal AND/OR/, carried by the body is stripped when the connector
// is emitted, so "AND AND"/"WHERE AND" can never appear. This also applies to bare
// is* tags not wrapped in <dynamic> (body usually self-carries "and ...").
func (p *xmlParser) handleCondStart(t xml.StartElement) {
	p.inIBatisCond = true
	p.isBodyStarted = false
	p.pendingConn = normalizeConnector(attrValue(t, "prepend"))
	if p.inDynamic && !p.dynamicUsed {
		p.dynamicUsed = true
		if p.dynamicPrepend != "" {
			p.pendingConn = p.dynamicPrepend
		}
	}
}

// handleIterateStart expands an iBATIS <iterate> tag. iBATIS uses it in two very
// different ways, distinguished by whether a parenthesised value list is formed:
//
//  1. Value-list iterate (open AND close present): body is a parameter list meant
//     to become values, e.g. `in <iterate open="(" close=")" conjunction=",">
//     #ids[]#</iterate>` → `in ('20230302')`. We swallow the inner parameter text
//     and emit a single dummy value between open/close. EXPLAIN validity holds in
//     any surrounding context (not only `in`).
//
//  2. Statement-level / batch iterate (no open/close pair, e.g.
//     conjunction=";"): the body IS the SQL (a full statement or a set fragment),
//     repeated once per list element at runtime. For a static single-statement
//     EXPLAIN we expand it once by letting the body flow through unchanged — the
//     tag acts as a transparent container (condition treated as true). Swallowing
//     it here is what previously collapsed batch UPDATEs to a bare dummy value
//     ("只剩个参数"). Only a single representative statement is produced; we never
//     simulate the N-way "; "-joined batch.
func (p *xmlParser) handleIterateStart(t xml.StartElement) {
	open := attrValue(t, "open")
	close := attrValue(t, "close")
	conjunction := attrValue(t, "conjunction")
	_ = conjunction // semantics documented above; single-element expansion needs no separator

	valueList := open != "" && close != ""
	if !valueList {
		// Statement-level / batch iterate: transparent container. Body charData and
		// nested tags flow through the normal handler (which applies cond/dynamic
		// connectors and leading-connector stripping as usual).
		return
	}

	// Value-list iterate.
	p.inIterate = true
	// Inside a conditional tag this <iterate> may be the block's first content.
	if p.inIBatisCond && !p.isBodyStarted {
		p.beginIBatisContent()
	}
	// Otherwise it may be the first content of a <dynamic>.
	if p.inDynamic && !p.dynamicUsed {
		p.dynamicUsed = true
		if p.dynamicPrepend != "" {
			p.emitConnector(p.dynamicPrepend)
			p.pendingConn = ""
			p.pendingFromDynamic = false
		}
	}
	// Clean any trailing whitespace so the list glues with a single space.
	p.trimTrailingWhitespace()
	if p.currentText.Len() > 0 && p.currentText.String()[p.currentText.Len()-1] != '(' {
		p.currentText.WriteByte(' ')
	}
	if open != "" {
		p.currentText.WriteString(open)
	}
	p.currentText.WriteString("'20230302'")
	if close != "" {
		p.currentText.WriteString(close)
	}
}

// beginIBatisContent marks the first content fragment of a conditional block and,
// if a connector was decided, emits it. Returns true when a connector was emitted
// (caller should then strip a redundant leading AND/OR/, from the body text).
func (p *xmlParser) beginIBatisContent() bool {
	if !p.inIBatisCond || p.isBodyStarted {
		return false
	}
	p.isBodyStarted = true
	if p.pendingConn != "" {
		p.emitPendingConnector()
		return true
	}
	return false
}

// emitPendingConnector writes the stored connector, if any.
func (p *xmlParser) emitPendingConnector() {
	if p.pendingConn == "" {
		return
	}
	p.emitConnector(p.pendingConn)
	p.pendingConn = ""
	p.pendingFromDynamic = false
}

// emitConnector concatenates connector c onto currentText separated by a single
// space (comma connectors join without a leading space: "A=1, B=2").
func (p *xmlParser) emitConnector(c string) {
	cur := strings.TrimRight(p.currentText.String(), " \t\n")
	p.currentText.Reset()
	p.currentText.WriteString(cur)
	c = strings.TrimLeft(c, " \t\n")
	if strings.HasPrefix(c, ",") {
		p.currentText.WriteString(c) // c == ", " or similar
	} else {
		p.currentText.WriteString(" ")
		p.currentText.WriteString(c)
	}
}

// stripLeadingConnector removes a single leading connector (and/or/,) from body
// text so a block emits exactly one connector (R2). Connector keywords are only
// stripped when they are actually connectors (a column literally named "and" is
// guarded by requiring a following space / end).
func stripLeadingConnector(s string) string {
	t := strings.TrimLeft(s, " \t\n")
	rest := t
	lower := strings.ToLower(rest)
	switch {
	case strings.HasPrefix(lower, "and "), lower == "and",
		strings.HasPrefix(lower, "and("):
		rest = rest[3:]
	case strings.HasPrefix(lower, "or "), lower == "or",
		strings.HasPrefix(lower, "or("):
		rest = rest[2:]
	case strings.HasPrefix(rest, ","):
		rest = rest[1:]
	default:
		return s
	}
	return rest
}

// trimTrailingWhitespace removes trailing spaces/newlines from currentText so a
// connector concatenates cleanly (prevents "... \nAND ...").
func (p *xmlParser) trimTrailingWhitespace() {
	cur := p.currentText.String()
	p.currentText.Reset()
	p.currentText.WriteString(strings.TrimRight(cur, " \t\n"))
}

func (p *xmlParser) handleEndElement(t xml.EndElement) {
	name := strings.ToLower(t.Name.Local)

	if isSQLTag(name) {
		if p.inChoose {
			// Flush choose result into currentText
			p.flushChoose()
			p.inChoose = false
		}
		if p.inSet {
			p.appendStrippedComma()
			p.inSet = false
		}
		sql := cleanSQL(p.currentText.String())
		p.statements = append(p.statements, &SQLStatement{
			File:    p.file,
			SQLID:   p.currentID,
			SQLType: p.currentType,
			OldSQL:  sql,
		})
		p.inSQL = false
		return
	}

	if !p.inSQL {
		return
	}

	switch name {
	case "where":
		p.appendStrippedPrefix()
		p.inWhere = false
	case "set":
		p.appendStrippedComma()
		p.inSet = false
	case "and":
		p.inAnd = false
		p.andPending = false
	case "choose":
		if p.inChoose {
			p.flushChoose()
			p.inChoose = false
		}
	case "when":
		if p.inChoose {
			// Save the text accumulated in whenBuf
			// (charData went to whenBuf while inWhen)
			p.inWhen = false
		}
	case "otherwise":
		if p.inChoose {
			// Text accumulated in otherwiseBuf
			p.inOtherwise = false
		}
	case "foreach":
		// nothing extra needed
	case "trim":
		// Apply suffix after content
		if p.trimSuffix != "" {
			p.currentText.WriteString(p.trimSuffix)
		}
		// Apply suffixOverrides — strip matching suffix from accumulated text
		// (simplified: P1 feature, marked for future)
	case "if", "str", "bind":
		// Reset andPending for next sibling inside <and>
		if p.inAnd {
			p.andPending = true
		}
	case "dynamic":
		p.inDynamic = false
		p.dynamicUsed = false
		p.dynamicPrepend = ""
	case "iterate":
		p.inIterate = false
	}
	if isIBatisCondTag(name) {
		// End of a conditional block: content (if any) already emitted with its
		// single connector. Reset so a following sibling re-decides its connector.
		p.inIBatisCond = false
		p.isBodyStarted = false
		p.pendingConn = ""
		p.pendingFromDynamic = false
	}
}

func (p *xmlParser) handleCharData(t xml.CharData) {
	text := string(t)
	isWhitespace := strings.TrimSpace(text) == ""

	if p.inChoose && (p.inWhen || p.inOtherwise) {
		// Route charData into branch-specific buffer to prevent duplication
		if isWhitespace {
			if p.inWhen {
				p.whenBuf.WriteString(" ")
			} else {
				p.otherwiseBuf.WriteString(" ")
			}
			return
		}
		// Prepend "\n and " connector for and context
		if p.andPending {
			p.currentText.WriteString("\n and ")
			p.andPending = false
		}
		if p.inWhen {
			p.whenBuf.WriteString(text)
		} else {
			p.otherwiseBuf.WriteString(text)
		}
		return
	}

	if !p.inSQL {
		return
	}

	// iBATIS <iterate>: its inner text is a parameter list that the start tag has
	// already expanded to a single dummy value — swallow the raw text.
	if p.inIterate {
		return
	}

	if isWhitespace {
		// Ignore whitespace before a conditional block has produced content: the
		// connector emission trims and re-spaces, so stray spaces only hurt.
		if p.inIBatisCond && !p.isBodyStarted {
			return
		}
		last := p.currentText.String()
		if len(last) > 0 && last[len(last)-1] != ' ' && last[len(last)-1] != '\n' {
			p.currentText.WriteByte(' ')
		}
		return
	}

	// Prepend "\n and " connector for each child's first charData inside <and>
	if p.andPending {
		p.currentText.WriteString("\n and ")
		p.andPending = false
	}

	// iBATIS conditional block's first content: emit its single connector (R2) and
	// drop a duplicate literal AND/OR/, that the body may carry.
	if p.inIBatisCond {
		if p.beginIBatisContent() {
			text = strings.TrimLeft(stripLeadingConnector(text), " \t\n")
		}
	} else if p.inDynamic && !p.dynamicUsed && p.dynamicPrepend != "" {
		// Raw content directly inside <dynamic> consumes the container's prepend.
		p.dynamicUsed = true
		p.emitConnector(p.dynamicPrepend)
		text = strings.TrimLeft(stripLeadingConnector(text), " \t\n")
	}
	p.currentText.WriteString(text)
}

// flushChoose writes the selected <choose> branch into currentText.
func (p *xmlParser) flushChoose() {
	var selected string
	if p.otherwiseSeen() {
		selected = p.otherwiseBuf.String()
	} else if p.whenSeen > 0 {
		selected = p.whenBuf.String() + " /* [expanded: first-when] */"
	}
	if selected != "" {
		// A <choose> may be the first content of an iBATIS <dynamic>/is* block; emit
		// that block's single connector and drop a duplicate leading AND/OR/, so the
		// branch joins the statement correctly (R2 / R5 nesting).
		if p.inIBatisCond && !p.isBodyStarted {
			if p.beginIBatisContent() {
				selected = strings.TrimLeft(stripLeadingConnector(selected), " \t\n")
			}
		} else if p.inDynamic && !p.dynamicUsed && p.dynamicPrepend != "" {
			p.dynamicUsed = true
			p.emitConnector(p.dynamicPrepend)
			selected = strings.TrimLeft(stripLeadingConnector(selected), " \t\n")
		}
		p.currentText.WriteString(selected)
	}
	p.whenBuf.Reset()
	p.otherwiseBuf.Reset()
	p.whenSeen = 0
	p.inWhen = false
	p.inOtherwise = false
}

func (p *xmlParser) otherwiseSeen() bool {
	return p.otherwiseBuf.Len() > 0
}

func (p *xmlParser) handleForeach(t xml.StartElement) {
	var open, close, separator string
	for _, attr := range t.Attr {
		switch strings.ToLower(attr.Name.Local) {
		case "open":
			open = attr.Value
		case "close":
			close = attr.Value
		case "separator":
			separator = attr.Value
		}
	}
	if open != "" {
		p.currentText.WriteString(open)
	}
	p.currentText.WriteString("1")
	if separator != "" {
		p.currentText.WriteString(separator)
		p.currentText.WriteString("2")
	}
	if close != "" {
		p.currentText.WriteString(close)
	}
}

func (p *xmlParser) handleTrim(t xml.StartElement) {
	for _, attr := range t.Attr {
		switch strings.ToLower(attr.Name.Local) {
		case "prefix":
			p.trimPrefix = attr.Value
		case "suffix":
			p.trimSuffix = attr.Value
		case "prefixoverrides":
			p.trimPrefixOverrides = attr.Value
		case "suffixoverrides":
			p.trimSuffixOverrides = attr.Value
		}
	}

	// Apply prefix immediately
	if p.trimPrefix != "" {
		p.currentText.WriteString(p.trimPrefix)
		p.currentText.WriteString(" ")
	}

	// If prefixOverrides is set, strip matching prefix from accumulated text
	if p.trimPrefixOverrides != "" {
		p.applyPrefixOverrides()
	}
}

// applyPrefixOverrides strips the override prefixes from the accumulated text.
func (p *xmlParser) applyPrefixOverrides() {
	text := p.currentText.String()
	// prefixOverrides is a pipe-separated list like "AND |OR "
	parts := strings.Split(p.trimPrefixOverrides, "|")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pattern := regexp.MustCompile(`(?i)^\s*` + regexp.QuoteMeta(part) + `\s*`)
		text = pattern.ReplaceAllString(text, "")
	}
	text = strings.TrimSpace(text)
	p.currentText.Reset()
	p.currentText.WriteString(text)
}

func (p *xmlParser) handleInclude(t xml.StartElement) {
	for _, attr := range t.Attr {
		if strings.ToLower(attr.Name.Local) == "refid" {
			if fragment, ok := p.fragments[attr.Value]; ok {
				p.currentText.WriteString(fragment)
			} else {
				p.errors = append(p.errors,
					fmt.Sprintf("%s: <include refid=\"%s\"> not found in file", p.file, attr.Value))
			}
		}
	}
}

func (p *xmlParser) appendStrippedPrefix() {
	text := strings.TrimRight(p.currentText.String(), " \t\n")
	upper := strings.ToUpper(text)
	for _, prefix := range []string{" AND", " OR"} {
		if strings.HasSuffix(upper, prefix) {
			text = text[:len(text)-len(prefix)]
			p.currentText.Reset()
			p.currentText.WriteString(strings.TrimRight(text, " \t\n"))
			return
		}
	}
}

func (p *xmlParser) appendStrippedComma() {
	text := strings.TrimRight(p.currentText.String(), " \t\n")
	if len(text) > 0 && text[len(text)-1] == ',' {
		text = text[:len(text)-1]
	}
	p.currentText.Reset()
	p.currentText.WriteString(text)
}

func cleanSQL(sql string) string {
	sql = strings.ReplaceAll(sql, "\t", " ")
	for strings.Contains(sql, "  ") {
		sql = strings.ReplaceAll(sql, "  ", " ")
	}
	for strings.Contains(sql, "\n\n\n") {
		sql = strings.ReplaceAll(sql, "\n\n\n", "\n")
	}
	return strings.TrimSpace(sql)
}

// ParseDirectory parses all .xml files in a directory.
func ParseDirectory(xmlDir string) (*ParseResult, error) {
	entries, err := os.ReadDir(xmlDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", xmlDir, err)
	}

	result := &ParseResult{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".xml") {
			continue
		}
		fullPath := filepath.Join(xmlDir, entry.Name())
		pr, err := parseFile(fullPath)
		if err != nil {
			result.SQLErrors = append(result.SQLErrors, err.Error())
			continue
		}
		result.Statements = append(result.Statements, pr.Statements...)
		result.SQLErrors = append(result.SQLErrors, pr.SQLErrors...)
	}
	return result, nil
}
