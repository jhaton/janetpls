package janet

import (
	"strings"
	"testing"
)

func TestParseToleratesIncompleteSource(t *testing.T) {
	document := Parse("file:///test.janet", "/test.janet", `(defn greet [name]
  (print "hello`)
	if len(document.Diagnostics) != 3 {
		t.Fatalf("diagnostics = %#v, want unterminated string and two unclosed lists", document.Diagnostics)
	}
	if got := document.Root.Children[0].Kind; got != NodeList {
		t.Fatalf("first form kind = %v, want list", got)
	}
}

func TestParseIgnoresCommentsAndStringContents(t *testing.T) {
	source := "# (ignored symbol)\n(def value \"(not-a-form)\")\n(def long ``[also ignored]``)"
	document := Parse("file:///test.janet", "/test.janet", source)
	if len(document.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", document.Diagnostics)
	}
	if got := len(document.Root.Children); got != 2 {
		t.Fatalf("top-level forms = %d, want 2", got)
	}
	for _, token := range document.Tokens {
		if token.Text == "ignored" || token.Text == "not-a-form" {
			t.Fatalf("string or comment contents emitted as symbol: %#v", token)
		}
	}
}

func TestMutableDelimiters(t *testing.T) {
	document := Parse("file:///test.janet", "/test.janet", "@[(one) @{two three}]")
	if len(document.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", document.Diagnostics)
	}
	if got := document.Root.Children[0].Delimiter; got != "@[" {
		t.Fatalf("delimiter = %q, want %q", got, "@[")
	}
}

func TestUTF16PositionRoundTrip(t *testing.T) {
	source := "a😀b\nsecond"
	document := Parse("file:///test.janet", "/test.janet", source)
	offset := strings.Index(source, "b")
	position := document.Position(offset)
	if position != (Position{Line: 0, Character: 3}) {
		t.Fatalf("position = %#v, want line 0 character 3", position)
	}
	if got := document.Offset(position); got != offset {
		t.Fatalf("round-trip offset = %d, want %d", got, offset)
	}
}
