package markdown

import (
	"strings"
	"testing"
)

func TestTokenizer(t *testing.T) {
	const text ElemType = "token.text"
	cases := []struct {
		name    string
		matcher Tokenizer
		input   string
		matched bool
		want    []typedText
		rest    string
	}{
		{"any-text", Any(), "文", true, nil, "文"},
		{"any-nul", Any(), "\x00", true, nil, "\x00"},
		{"any-eof", Any(), "", false, nil, ""},
		{"any-newline", Any(), "\n", false, nil, "\n"},
		{"any-carriage-return", Any(), "\r", false, nil, "\r"},
		{"string-unicode", String(text, "你好"), "你好!", true, []typedText{{text, "你好"}}, "!"},
		{"string-partial-rollback", String(text, "abc"), "abx", false, nil, "abx"},
		{"string-empty", String(text, ""), "x", true, nil, "x"},
		{"string-literal-nul", String(text, "a\x00"), "a\x00!", true, []typedText{{text, "a\x00"}}, "!"},
		{"string-missing-nul", String(text, "a\x00"), "a", false, nil, "a"},
		{"runes-ordered", Runes(text, "你好"), "你好!", true, []typedText{{text, "你好"}}, "!"},
		{"runes-partial-rollback", Runes(text, "ab"), "ax", false, nil, "ax"},
		{"runes-missing-nul", Runes(text, "a\x00"), "a", false, nil, "a"},
		{"repeat-too-short", Repeat(text, '#', 2, 3), "#x", false, nil, "#x"},
		{"repeat-exact-minimum", Repeat(text, '#', 2, 3), "##x", true, []typedText{{text, "##"}}, "x"},
		{"repeat-exact-maximum", Repeat(text, '#', 2, 3), "###x", true, []typedText{{text, "###"}}, "x"},
		{"repeat-leaves-extra", Repeat(text, '#', 2, 3), "####x", true, []typedText{{text, "###"}}, "#x"},
		{"repeat-zero-bounds", Repeat(text, '#', 0, 0), "x", true, nil, "x"},
		{"repeat-optional-absent", Repeat(text, '#', 0, 3), "x", true, nil, "x"},
		{"repeat-impossible-bounds", Repeat(text, '#', 3, 2), "###x", false, nil, "###x"},
		{"repeat-nul", Repeat(text, 0, 1, 1), "\x00x", true, []typedText{{text, "\x00"}}, "x"},
		{"repeat-nul-at-eof", Repeat(text, 0, 1, 1), "", false, nil, ""},
		{"spaces", Spaces(text), " \tvalue", true, []typedText{{text, " \t"}}, "value"},
		{"spaces-optional", Spaces(text), "value", true, nil, "value"},
		{"until-escaped-delimiter", Until(text, ']'), "a\\]b]tail", true, []typedText{{text, "a\\]b"}}, "]tail"},
		{"until-newline-delimiter", Until(text, '\n'), "line\n", true, []typedText{{text, "line"}}, "\n"},
		{"until-backslash-delimiter", Until(text, '\\'), "a\\b", true, []typedText{{text, "a"}}, "\\b"},
		{"until-eof-rollback", Until(text, ']'), "abc", false, nil, "abc"},
		{"until-trailing-escape", Until(text, ']'), "abc\\", false, nil, "abc\\"},
		{"until-newline-rollback", Until(text, ']'), "a\nb]", false, nil, "a\nb]"},
		{"until-escaped-newline", Until(text, ']'), "a\\\nb]", false, nil, "a\\\nb]"},
		{"until-carriage-return", Until(text, ']'), "a\rb]", false, nil, "a\rb]"},
		{"until-nul-content", Until(text, ']'), "a\x00b]", true, []typedText{{text, "a\x00b"}}, "]"},
		{"until-runes-any-delimiter", UntilRunes(text, "]})"), "name}tail", true, []typedText{{text, "name"}}, "}tail"},
		{"until-runes-newline-delimiter", UntilRunes(text, "\r\n"), "line\r\n", true, []typedText{{text, "line"}}, "\r\n"},
		{"until-runes-escaped-newline", UntilRunes(text, "]}"), "a\\\nb]", false, nil, "a\\\nb]"},
		{"until-runes-backslash-delimiter", UntilRunes(text, "\\]"), "a\\b", true, []typedText{{text, "a"}}, "\\b"},
		{"until-runes-trailing-escape", UntilRunes(text, "]}"), "abc\\", false, nil, "abc\\"},
		{"until-runes-nul-content", UntilRunes(text, "]}"), "a\x00b]", true, []typedText{{text, "a\x00b"}}, "]"},
		{"or-failed-first-branch", Or(Sequence(String("first", "a"), String("first", "z")), String(text, "ab")), "ab!", true, []typedText{{text, "ab"}}, "!"},
		{"or-all-failed", Or(String(text, "ab"), String(text, "ac")), "ax", false, nil, "ax"},
		{"sequence-success", Sequence(String("prefix", "a"), String(text, "b")), "ab!", true, []typedText{{"prefix", "a"}, {text, "b"}}, "!"},
		{"sequence-rollback", Sequence(String("prefix", "a"), String(text, "b")), "ax", false, nil, "ax"},
		{"alignment-at-eof", MatchTableAlignRow(), "| --- |", true, []typedText{{"table.border", "|"}, {"whitespace", " "}, {"table.border", "---"}, {"whitespace", " "}, {"table.border", "|"}}, ""},
		{"alignment-keeps-newline", MatchTableAlignRow(), "| --- |\nnext", true, []typedText{{"table.border", "|"}, {"whitespace", " "}, {"table.border", "---"}, {"whitespace", " "}, {"table.border", "|"}}, "\nnext"},
		{"alignment-invalid-rollback", MatchTableAlignRow(), "| -x- |\n", false, nil, "| -x- |\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParser()
			p.Put(tc.input)
			p.Close()
			got, matched := tc.matcher(p)
			if matched != tc.matched {
				t.Fatalf("matched = %t, want %t; segments = %#v", matched, tc.matched, got)
			}
			assertSegments(t, got, tc.want...)
			var rest strings.Builder
			for range tc.rest {
				rest.WriteRune(p.Next())
			}
			if rest.String() != tc.rest {
				t.Fatalf("remaining input = %q, want %q", rest.String(), tc.rest)
			}
		})
	}
}
