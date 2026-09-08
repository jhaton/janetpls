package janet

import "testing"

func TestFormatCanonicalLayout(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{
			name:     "control form indentation",
			source:   "(def     a\n 3 )",
			expected: "(def a\n  3)\n",
		},
		{
			name:     "reader macros and destructuring",
			source:   "(defmacro- letv [bindings & body]\n  ~(do ,;(seq [[k v] :in (partition 2 bindings)] ['var k v]) ,;body))\n",
			expected: "(defmacro- letv [bindings & body]\n  ~(do ,;(seq [[k v] :in (partition 2 bindings)] ['var k v]) ,;body))\n",
		},
		{
			name:     "comments and mutable containers",
			source:   "# values\n(def values   @{:items @[1   2]})\n",
			expected: "# values\n(def values @{:items @[1 2]})\n",
		},
		{
			name:     "long multiline string",
			source:   "(def text ``\nline one\nline two\n``)\n",
			expected: "(def text ``\nline one\nline two\n``)\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			formatted, err := Format(test.source)
			if err != nil {
				t.Fatal(err)
			}
			if formatted != test.expected {
				t.Fatalf("formatted source:\n%q\nwant:\n%q", formatted, test.expected)
			}
			second, err := Format(formatted)
			if err != nil {
				t.Fatal(err)
			}
			if second != formatted {
				t.Fatalf("formatter is not idempotent:\n%q", second)
			}
		})
	}
}

func TestFormatRejectsMalformedSource(t *testing.T) {
	if _, err := Format("(def value [1 2)"); err == nil {
		t.Fatal("malformed source formatted without an error")
	}
}
