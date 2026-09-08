package janet

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type TokenKind uint8

const (
	TokenSymbol TokenKind = iota
	TokenString
	TokenOpen
	TokenClose
	TokenReader
)

type Token struct {
	Kind       TokenKind
	Text       string
	Start, End int
}

func (t Token) Value() string {
	if t.Kind != TokenString {
		return t.Text
	}
	text := t.Text
	if strings.HasPrefix(text, "@\"") {
		text = text[1:]
	}
	if strings.HasPrefix(text, "\"") {
		if value, err := strconv.Unquote(text); err == nil {
			return value
		}
	}
	if strings.HasPrefix(text, "`") {
		n := 0
		for n < len(text) && text[n] == '`' {
			n++
		}
		if len(text) >= 2*n {
			return text[n : len(text)-n]
		}
	}
	return text
}

type NodeKind uint8

const (
	NodeRoot NodeKind = iota
	NodeList
	NodeVector
	NodeStruct
	NodeAtom
)

type Node struct {
	Kind       NodeKind
	Delimiter  string
	Start, End int
	Token      int
	Quoted     bool
	Children   []*Node
}

func (n *Node) Atom(tokens []Token) (Token, bool) {
	if n == nil || n.Kind != NodeAtom || n.Token < 0 || n.Token >= len(tokens) {
		return Token{}, false
	}
	return tokens[n.Token], true
}

func (n *Node) Symbol(tokens []Token) (string, bool) {
	token, ok := n.Atom(tokens)
	if !ok || token.Kind != TokenSymbol {
		return "", false
	}
	return token.Text, true
}

type Diagnostic struct {
	Start, End int
	Message    string
}

type Document struct {
	URI         string
	Path        string
	Source      string
	Tokens      []Token
	Root        *Node
	Diagnostics []Diagnostic
	LineStarts  []int
}

func Parse(uri, path, source string) *Document {
	tokens, diagnostics := lex(source)
	root := &Node{Kind: NodeRoot, Start: 0, End: len(source), Token: -1}
	stack := []*Node{root}
	pendingQuote := false

	for index := range tokens {
		token := tokens[index]
		switch token.Kind {
		case TokenReader:
			pendingQuote = token.Text == "'" || token.Text == "~" || token.Text == ";"
		case TokenOpen:
			node := &Node{
				Kind:      nodeKind(token.Text),
				Delimiter: token.Text,
				Start:     token.Start,
				End:       len(source),
				Token:     -1,
				Quoted:    pendingQuote,
			}
			pendingQuote = false
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
			stack = append(stack, node)
		case TokenClose:
			pendingQuote = false
			if len(stack) == 1 {
				diagnostics = append(diagnostics, Diagnostic{
					Start:   token.Start,
					End:     token.End,
					Message: fmt.Sprintf("unexpected closing delimiter %q", token.Text),
				})
				continue
			}
			match := len(stack) - 1
			for match > 0 && !matchingDelimiter(stack[match].Delimiter, token.Text) {
				match--
			}
			if match == 0 {
				current := stack[len(stack)-1]
				diagnostics = append(diagnostics, Diagnostic{
					Start:   token.Start,
					End:     token.End,
					Message: fmt.Sprintf("closing delimiter %q does not match %q", token.Text, current.Delimiter),
				})
				continue
			}
			for unclosed := len(stack) - 1; unclosed > match; unclosed-- {
				node := stack[unclosed]
				diagnostics = append(diagnostics, Diagnostic{
					Start:   node.Start,
					End:     min(node.Start+len(node.Delimiter), len(source)),
					Message: fmt.Sprintf("unclosed delimiter %q", node.Delimiter),
				})
			}
			current := stack[match]
			current.End = token.End
			stack = stack[:match]
		default:
			node := &Node{
				Kind:   NodeAtom,
				Start:  token.Start,
				End:    token.End,
				Token:  index,
				Quoted: pendingQuote,
			}
			pendingQuote = false
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, node)
		}
	}

	for index := len(stack) - 1; index > 0; index-- {
		node := stack[index]
		diagnostics = append(diagnostics, Diagnostic{
			Start:   node.Start,
			End:     min(node.Start+len(node.Delimiter), len(source)),
			Message: fmt.Sprintf("unclosed delimiter %q", node.Delimiter),
		})
	}

	return &Document{
		URI:         uri,
		Path:        path,
		Source:      source,
		Tokens:      tokens,
		Root:        root,
		Diagnostics: diagnostics,
		LineStarts:  lineStarts(source),
	}
}

func lex(source string) ([]Token, []Diagnostic) {
	tokens := make([]Token, 0, len(source)/5)
	diagnostics := make([]Diagnostic, 0)

	for index := 0; index < len(source); {
		r, size := utf8.DecodeRuneInString(source[index:])
		if unicode.IsSpace(r) || r == 0 {
			index += size
			continue
		}
		if source[index] == '#' {
			for index < len(source) && source[index] != '\n' {
				index++
			}
			continue
		}
		if source[index] == '@' && index+1 < len(source) {
			next := source[index+1]
			if next == '"' {
				end, closed := scanQuotedString(source, index+1)
				tokens = append(tokens, Token{Kind: TokenString, Text: source[index:end], Start: index, End: end})
				if !closed {
					diagnostics = append(diagnostics, Diagnostic{Start: index, End: end, Message: "unterminated buffer string"})
				}
				index = end
				continue
			}
			if next == '(' || next == '[' || next == '{' {
				tokens = append(tokens, Token{Kind: TokenOpen, Text: source[index : index+2], Start: index, End: index + 2})
				index += 2
				continue
			}
		}
		if source[index] == '"' {
			end, closed := scanQuotedString(source, index)
			tokens = append(tokens, Token{Kind: TokenString, Text: source[index:end], Start: index, End: end})
			if !closed {
				diagnostics = append(diagnostics, Diagnostic{Start: index, End: end, Message: "unterminated string"})
			}
			index = end
			continue
		}
		if source[index] == '`' || (source[index] == '@' && index+1 < len(source) && source[index+1] == '`') {
			start := index
			if source[index] == '@' {
				index++
			}
			delimiterStart := index
			for index < len(source) && source[index] == '`' {
				index++
			}
			delimiter := source[delimiterStart:index]
			closing := strings.Index(source[index:], delimiter)
			closed := closing >= 0
			if closed {
				index += closing + len(delimiter)
			} else {
				index = len(source)
			}
			tokens = append(tokens, Token{Kind: TokenString, Text: source[start:index], Start: start, End: index})
			if !closed {
				diagnostics = append(diagnostics, Diagnostic{Start: start, End: index, Message: "unterminated long string"})
			}
			continue
		}
		if kind, width := punctuation(source[index:]); width != 0 {
			tokens = append(tokens, Token{Kind: kind, Text: source[index : index+width], Start: index, End: index + width})
			index += width
			continue
		}

		start := index
		for index < len(source) {
			r, size = utf8.DecodeRuneInString(source[index:])
			if unicode.IsSpace(r) || r == 0 || isTerminator(source[index]) {
				break
			}
			index += size
		}
		if index == start {
			index++
			continue
		}
		tokens = append(tokens, Token{Kind: TokenSymbol, Text: source[start:index], Start: start, End: index})
	}
	return tokens, diagnostics
}

func scanQuotedString(source string, start int) (int, bool) {
	for index := start + 1; index < len(source); index++ {
		switch source[index] {
		case '\\':
			index++
		case '"':
			return index + 1, true
		case '\n':
			return index, false
		}
	}
	return len(source), false
}

func punctuation(source string) (TokenKind, int) {
	switch source[0] {
	case '(', '[', '{':
		return TokenOpen, 1
	case ')', ']', '}':
		return TokenClose, 1
	case '\'', '~', ',', ';', '|':
		return TokenReader, 1
	default:
		return 0, 0
	}
}

func isTerminator(value byte) bool {
	switch value {
	case '#', '"', '`', '(', ')', '[', ']', '{', '}', '\'', '~', ',', ';', '|':
		return true
	default:
		return false
	}
}

func nodeKind(delimiter string) NodeKind {
	switch delimiter[len(delimiter)-1] {
	case '(':
		return NodeList
	case '[':
		return NodeVector
	default:
		return NodeStruct
	}
}

func matchingDelimiter(opening, closing string) bool {
	switch opening[len(opening)-1] {
	case '(':
		return closing == ")"
	case '[':
		return closing == "]"
	case '{':
		return closing == "}"
	default:
		return false
	}
}

func lineStarts(source string) []int {
	starts := []int{0}
	for index := range len(source) {
		if source[index] == '\n' {
			starts = append(starts, index+1)
		}
	}
	return starts
}
