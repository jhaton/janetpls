package janet

// The formatter algorithm is adapted from janet-lang/spork's fmt.janet,
// Copyright (c) 2022 Calvin Rose and contributors, under the MIT License.

import (
	"fmt"
	"strings"
)

type formatKind uint8

const (
	formatTop formatKind = iota
	formatNewline
	formatComment
	formatSpan
	formatString
	formatBuffer
	formatContainer
	formatReader
)

type formatNode struct {
	kind     formatKind
	text     string
	opening  string
	closing  string
	children []*formatNode
}

type formatParser struct {
	source string
	offset int
}

// Format rewrites Janet source using the canonical two-space layout used by
// spork/fmt. It rejects malformed source rather than risking a destructive edit.
func Format(source string) (string, error) {
	parser := formatParser{source: source}
	children, err := parser.sequence(0)
	if err != nil {
		return "", err
	}
	emitter := formatEmitter{}
	emitter.node(&formatNode{kind: formatTop, children: children})
	emitter.newline()
	emitter.flushWhitespace()
	return emitter.output.String(), nil
}

func (parser *formatParser) sequence(closing byte) ([]*formatNode, error) {
	var children []*formatNode
	for parser.offset < len(parser.source) {
		current := parser.source[parser.offset]
		if closing != 0 && current == closing {
			parser.offset++
			return children, nil
		}
		if current == ')' || current == ']' || current == '}' {
			return nil, fmt.Errorf("unexpected closing delimiter %q at byte %d", current, parser.offset)
		}
		if isDiscardedFormatWhitespace(current) {
			parser.offset++
			continue
		}
		if current == '\n' {
			parser.offset++
			children = append(children, &formatNode{kind: formatNewline})
			continue
		}
		if current == '#' {
			children = append(children, parser.comment())
			continue
		}
		node, err := parser.form()
		if err != nil {
			return nil, err
		}
		children = append(children, node)
	}
	if closing != 0 {
		return nil, fmt.Errorf("missing closing delimiter %q", closing)
	}
	return children, nil
}

func (parser *formatParser) form() (*formatNode, error) {
	if parser.offset >= len(parser.source) {
		return nil, fmt.Errorf("expected form at end of input")
	}
	start := parser.offset
	current := parser.source[parser.offset]
	if isReaderMacro(current) {
		parser.offset++
		reader := &formatNode{kind: formatReader, text: parser.source[start:parser.offset]}
		for parser.offset < len(parser.source) {
			current = parser.source[parser.offset]
			if isDiscardedFormatWhitespace(current) {
				parser.offset++
				continue
			}
			if current == '\n' {
				parser.offset++
				reader.children = append(reader.children, &formatNode{kind: formatNewline})
				continue
			}
			if current == '#' {
				reader.children = append(reader.children, parser.comment())
				continue
			}
			break
		}
		child, err := parser.form()
		if err != nil {
			return nil, err
		}
		reader.children = append(reader.children, child)
		return reader, nil
	}

	if opening, closing, ok := parser.container(); ok {
		parser.offset += len(opening)
		children, err := parser.sequence(closing)
		if err != nil {
			return nil, err
		}
		return &formatNode{
			kind: formatContainer, opening: opening, closing: string(closing), children: children,
		}, nil
	}

	buffer := current == '@' && parser.offset+1 < len(parser.source) &&
		(parser.source[parser.offset+1] == '"' || parser.source[parser.offset+1] == '`')
	if current == '"' || current == '`' || buffer {
		return parser.string(buffer)
	}

	for parser.offset < len(parser.source) && !isFormatTerminator(parser.source, parser.offset) {
		parser.offset++
	}
	if parser.offset == start {
		return nil, fmt.Errorf("cannot parse byte %q at byte %d", current, start)
	}
	return &formatNode{kind: formatSpan, text: parser.source[start:parser.offset]}, nil
}

func (parser *formatParser) container() (string, byte, bool) {
	remaining := parser.source[parser.offset:]
	if len(remaining) >= 2 && remaining[0] == '@' {
		switch remaining[1] {
		case '(':
			return "@(", ')', true
		case '[':
			return "@[", ']', true
		case '{':
			return "@{", '}', true
		}
	}
	switch remaining[0] {
	case '(':
		return "(", ')', true
	case '[':
		return "[", ']', true
	case '{':
		return "{", '}', true
	default:
		return "", 0, false
	}
}

func (parser *formatParser) comment() *formatNode {
	start := parser.offset + 1
	parser.offset = start
	for parser.offset < len(parser.source) && parser.source[parser.offset] != '\n' {
		parser.offset++
	}
	text := parser.source[start:parser.offset]
	if parser.offset < len(parser.source) {
		parser.offset++
	}
	return &formatNode{kind: formatComment, text: text}
}

func (parser *formatParser) string(buffer bool) (*formatNode, error) {
	kind := formatString
	if buffer {
		kind = formatBuffer
		parser.offset++
	}
	start := parser.offset
	if parser.source[parser.offset] == '"' {
		parser.offset++
		for parser.offset < len(parser.source) {
			switch parser.source[parser.offset] {
			case '\\':
				parser.offset++
				if parser.offset < len(parser.source) {
					parser.offset++
				}
			case '"':
				parser.offset++
				return &formatNode{kind: kind, text: parser.source[start:parser.offset]}, nil
			default:
				parser.offset++
			}
		}
		return nil, fmt.Errorf("unterminated string at byte %d", start)
	}

	delimiterStart := parser.offset
	for parser.offset < len(parser.source) && parser.source[parser.offset] == '`' {
		parser.offset++
	}
	delimiter := parser.source[delimiterStart:parser.offset]
	closing := strings.Index(parser.source[parser.offset:], delimiter)
	if closing < 0 {
		return nil, fmt.Errorf("unterminated long string at byte %d", start)
	}
	parser.offset += closing + len(delimiter)
	return &formatNode{kind: kind, text: parser.source[start:parser.offset]}, nil
}

func isDiscardedFormatWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\f', 0, '\v':
		return true
	default:
		return false
	}
}

func isReaderMacro(value byte) bool {
	switch value {
	case '\'', ';', '~', ',', '|':
		return true
	default:
		return false
	}
}

func isFormatTerminator(source string, offset int) bool {
	value := source[offset]
	if isDiscardedFormatWhitespace(value) || value == '\n' || value == '#' || value == '"' ||
		value == '`' || value == '(' || value == ')' || value == '[' || value == ']' ||
		value == '{' || value == '}' || isReaderMacro(value) {
		return true
	}
	return value == '@' && offset+1 < len(source) && strings.ContainsRune("\"`([{", rune(source[offset+1]))
}

type formatEmitter struct {
	output      strings.Builder
	column      int
	indent      string
	indentStack []string
	whitespace  string
}

func (emitter *formatEmitter) node(node *formatNode) {
	if node.kind == formatTop || node.kind == formatContainer {
		node.children = trimFormatNewlines(node.children, node.kind == formatTop)
	}
	if node.kind != formatNewline {
		emitter.flushWhitespace()
	}

	switch node.kind {
	case formatTop:
		emitter.body("", node.children, "", 0)
	case formatNewline:
		emitter.newline()
	case formatComment:
		emitter.emit("#", node.text)
		emitter.newline()
	case formatSpan:
		emitter.emit(node.text)
		emitter.addWhitespace()
	case formatString:
		emitter.multilineString(node.text)
		emitter.addWhitespace()
	case formatBuffer:
		emitter.emit("@")
		emitter.multilineString(node.text)
		emitter.addWhitespace()
	case formatContainer:
		if node.opening == "(" && indentTwo(node.children) {
			emitter.body(node.opening, node.children, node.closing, 1)
		} else if node.opening == "(" {
			emitter.functionCall(node.children)
		} else {
			emitter.body(node.opening, node.children, node.closing, 0)
		}
	case formatReader:
		emitter.emit(node.text)
		for _, child := range node.children {
			emitter.node(child)
		}
	}
}

func (emitter *formatEmitter) body(opening string, children []*formatNode, closing string, delta int) {
	emitter.emit(opening)
	emitter.pushIndent(delta)
	for _, child := range children {
		emitter.node(child)
	}
	emitter.dropWhitespace()
	emitter.popIndent()
	emitter.emit(closing)
	emitter.addWhitespace()
}

func (emitter *formatEmitter) functionCall(children []*formatNode) {
	emitter.emit("(")
	if len(children) > 0 {
		emitter.node(children[0])
		emitter.pushIndent(1)
		for _, child := range children[1:] {
			emitter.node(child)
		}
		emitter.dropWhitespace()
		emitter.popIndent()
	}
	emitter.emit(")")
	emitter.addWhitespace()
}

func (emitter *formatEmitter) multilineString(value string) {
	parts := strings.Split(value, "\n")
	for index, part := range parts {
		if index > 0 {
			emitter.newline()
			emitter.dropWhitespace()
		}
		emitter.emit(part)
	}
}

func (emitter *formatEmitter) emit(values ...string) {
	for _, value := range values {
		emitter.output.WriteString(value)
		emitter.column += len(value)
	}
}

func (emitter *formatEmitter) pushIndent(delta int) {
	emitter.indentStack = append(emitter.indentStack, emitter.indent)
	emitter.indent = strings.Repeat(" ", emitter.column+delta)
}

func (emitter *formatEmitter) popIndent() {
	last := len(emitter.indentStack) - 1
	emitter.indent = emitter.indentStack[last]
	emitter.indentStack = emitter.indentStack[:last]
}

func (emitter *formatEmitter) flushWhitespace() {
	emitter.emit(emitter.whitespace)
	emitter.whitespace = ""
}

func (emitter *formatEmitter) dropWhitespace() {
	emitter.whitespace = ""
}

func (emitter *formatEmitter) addWhitespace() {
	emitter.whitespace += " "
}

func (emitter *formatEmitter) newline() {
	emitter.dropWhitespace()
	emitter.output.WriteByte('\n')
	emitter.whitespace = emitter.indent
	emitter.column = 0
}

func trimFormatNewlines(children []*formatNode, top bool) []*formatNode {
	start := 0
	for start < len(children) && children[start].kind == formatNewline {
		start++
	}
	end := len(children)
	for end > start && children[end-1].kind == formatNewline {
		end--
	}
	children = children[start:end]
	maximum := 2
	if top {
		maximum = 3
	}
	result := make([]*formatNode, 0, len(children))
	consecutive := 0
	for _, child := range children {
		if child.kind == formatNewline {
			if consecutive == maximum {
				continue
			}
			consecutive++
		} else {
			consecutive = 0
		}
		result = append(result, child)
	}
	return result
}

func indentTwo(children []*formatNode) bool {
	if len(children) == 0 {
		return false
	}
	if len(children) > 1 && children[1].kind == formatNewline {
		return true
	}
	head := children[0]
	if head.kind != formatSpan {
		return false
	}
	if indentTwoForms[head.text] {
		return true
	}
	return strings.HasPrefix(head.text, "with-") || strings.HasPrefix(head.text, "def") ||
		strings.HasPrefix(head.text, "if-") || strings.HasPrefix(head.text, "when-")
}

var indentTwoForms = map[string]bool{
	"fn": true, "match": true, "with": true, "with-dyns": true, "def": true,
	"def-": true, "var": true, "var-": true, "defn": true, "defn-": true,
	"varfn": true, "defmacro": true, "defmacro-": true, "defer": true,
	"edefer": true, "loop": true, "seq": true, "tabseq": true, "generate": true,
	"coro": true, "for": true, "each": true, "eachp": true, "eachk": true,
	"case": true, "cond": true, "do": true, "defglobal": true, "varglobal": true,
	"if": true, "when": true, "when-let": true, "when-with": true, "while": true,
	"with-syms": true, "with-vars": true, "if-let": true, "if-not": true,
	"if-with": true, "let": true, "short-fn": true, "try": true, "unless": true,
	"default": true, "forever": true, "upscope": true, "repeat": true,
	"eachy": true, "forv": true, "compwhen": true, "compif": true,
	"ev/spawn": true, "ev/do-thread": true, "ev/with-deadline": true,
	"label": true, "prompt": true,
}
