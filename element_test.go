package markdown

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestMarkdownSyntax(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []typedText
	}{
		{"empty", "", nil},
		{"paragraphs", "plain 文本\n\nnext", []typedText{{"paragraph.text", "plain 文本"}, {"whitespace", "\n\n"}, {"paragraph.text", "next"}}},
		{"leading-whitespace", "\t  text\n", []typedText{{"whitespace", "\t  "}, {"paragraph.text", "text"}, {"whitespace", "\n"}}},
		{"header", "# title\n", []typedText{{"header.prefix", "#"}, {"header.text", " title"}, {"whitespace", "\n"}}},
		{"header-limit", "####### title\n", []typedText{{"header.prefix", "#######"}, {"header.text", " title"}, {"whitespace", "\n"}}},
		{"header-over-limit", "######## title\n", []typedText{{"paragraph.text", "######## title"}, {"whitespace", "\n"}}},
		{"strong-stars", "**bold**", []typedText{{"strong.prefix", "**"}, {"strong.text", "bold"}, {"strong.suffix", "**"}}},
		{"strong-underscores", "__bold__", []typedText{{"strong.prefix", "__"}, {"strong.text", "bold"}, {"strong.suffix", "__"}}},
		{"italic-star", "*italic*", []typedText{{"italic.prefix", "*"}, {"italic.text", "italic"}, {"italic.suffix", "*"}}},
		{"italic-underscore", "_italic_", []typedText{{"italic.prefix", "_"}, {"italic.text", "italic"}, {"italic.suffix", "_"}}},
		{"intraword-underscores", "web_42 foo_bar_baz a__b__c", []typedText{{"paragraph.text", "web_42 foo_bar_baz a__b__c"}}},
		{"underscore-with-internal-underscore", "_foo_bar_", []typedText{{"italic.prefix", "_"}, {"italic.text", "foo_bar"}, {"italic.suffix", "_"}}},
		{"nested-italic-in-stars", "**bold _italic_**", []typedText{{"strong.prefix", "**"}, {"strong.text", "bold "}, {"italic.prefix", "_"}, {"italic.text", "italic"}, {"italic.suffix", "_"}, {"strong.suffix", "**"}}},
		{"nested-italic-in-underscores", "__bold *italic*__", []typedText{{"strong.prefix", "__"}, {"strong.text", "bold "}, {"italic.prefix", "*"}, {"italic.text", "italic"}, {"italic.suffix", "*"}, {"strong.suffix", "__"}}},
		{"escaped-openers", "\\*literal\\* \\[name](url) \\`code\\` \\$x\\$", []typedText{{"paragraph.text", "\\*literal\\* \\[name](url) \\`code\\` \\$x\\$"}}},
		{"escaped-strong-closer", "**a\\**b**", []typedText{{"strong.prefix", "**"}, {"strong.text", "a\\**b"}, {"strong.suffix", "**"}}},
		{"escaped-backslash", "\\\\*italic*", []typedText{{"paragraph.text", "\\\\"}, {"italic.prefix", "*"}, {"italic.text", "italic"}, {"italic.suffix", "*"}}},
		{"trailing-backslash", "text\\", []typedText{{"paragraph.text", "text\\"}}},
		{"link", "[name](https://example.test)", []typedText{{"link.[", "["}, {"link.name", "name"}, {"link.]", "]"}, {"link.(", "("}, {"link.url", "https://example.test"}, {"link.)", ")"}}},
		{"escaped-link-delimiters", "[a\\]b](u\\)v)", []typedText{{"link.[", "["}, {"link.name", "a\\]b"}, {"link.]", "]"}, {"link.(", "("}, {"link.url", "u\\)v"}, {"link.)", ")"}}},
		{"image", "![alt](image.png)", []typedText{{"image.![", "!["}, {"image.alt", "alt"}, {"image.]", "]"}, {"image.(", "("}, {"image.url", "image.png"}, {"image.)", ")"}}},
		{"nested-link", "**[name](url)**", []typedText{{"strong.prefix", "**"}, {"link.[", "["}, {"link.name", "name"}, {"link.]", "]"}, {"link.(", "("}, {"link.url", "url"}, {"link.)", ")"}, {"strong.suffix", "**"}}},
		{"nested-reference", "**Hello <web_1>**", []typedText{{"strong.prefix", "**"}, {"strong.text", "Hello "}, {"reference.prefix", "<"}, {"reference.web", "web_"}, {"reference.id", "1"}, {"reference.suffix", ">"}, {"strong.suffix", "**"}}},
		{"inline-code", "`a\\`b`", []typedText{{"inline-code.prefix", "`"}, {"inline-code.text", "a\\`b"}, {"inline-code.suffix", "`"}}},
		{"inline-equation", "$x\\$y$", []typedText{{"inline-equation.prefix", "$"}, {"inline-equation.text", "x\\$y"}, {"inline-equation.suffix", "$"}}},
		{"equation-block", "$$\nx=1\n$$\n", []typedText{{"equation.prefix", "$$"}, {"equation.text", "\nx=1\n"}, {"equation.suffix", "$$"}, {"whitespace", "\n"}}},
		{"code-block", "```go\nfmt.Println(1)\n```\n", []typedText{{"code.prefix", "```"}, {"code.syntax", "go"}, {"code.text", "\nfmt.Println(1)\n"}, {"code.suffix", "```"}, {"whitespace", "\n"}}},
		{"code-fence-inside-content", "```go\nx := \"```\"\n```\n", []typedText{{"code.prefix", "```"}, {"code.syntax", "go"}, {"code.text", "\nx := \"```\"\n"}, {"code.suffix", "```"}, {"whitespace", "\n"}}},
		{"code-fence-with-trailing-text", "```\n```text\n```\n", []typedText{{"code.prefix", "```"}, {"code.text", "\n```text\n"}, {"code.suffix", "```"}, {"whitespace", "\n"}}},
		{"code-opening-at-eof", "```", []typedText{{"code.prefix", "```"}}},
		{"code-info-at-eof", "```go", []typedText{{"code.prefix", "```"}, {"code.syntax", "go"}}},
		{"code-info-trailing-backslash", "```go\\\nbody\n```", []typedText{{"code.prefix", "```"}, {"code.syntax", "go\\"}, {"code.text", "\nbody\n"}, {"code.suffix", "```"}}},
		{"list-markers", "- one\n+ two\n* three\n", []typedText{{"list.prefix", "- "}, {"list.text", "one"}, {"whitespace", "\n"}, {"list.prefix", "+ "}, {"list.text", "two"}, {"whitespace", "\n"}, {"list.prefix", "* "}, {"list.text", "three"}, {"whitespace", "\n"}}},
		{"partial-list-prefix", "-x\n+ ok\n", []typedText{{"paragraph.text", "-x"}, {"whitespace", "\n"}, {"list.prefix", "+ "}, {"list.text", "ok"}, {"whitespace", "\n"}}},
		{"quote", "> one\n> two\n", []typedText{{"quote.prefix", "> "}, {"quote.text", "one"}, {"whitespace", "\n"}, {"quote.prefix", "> "}, {"quote.text", "two"}, {"whitespace", "\n"}}},
		{"table-body-and-next-block", "| h |\n| --- |\n| v |\n# after\n", []typedText{{"table.border", "|"}, {"whitespace", " "}, {"table.text", "h"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"table.border", "|"}, {"whitespace", " "}, {"table.border", "---"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"table.border", "|"}, {"whitespace", " "}, {"table.text", "v"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"header.prefix", "#"}, {"header.text", " after"}, {"whitespace", "\n"}}},
		{"table-trailing-whitespace-before-next-block", "| h |\n| --- |\n| v | \t\n# after\n", []typedText{{"table.border", "|"}, {"whitespace", " "}, {"table.text", "h"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"table.border", "|"}, {"whitespace", " "}, {"table.border", "---"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"table.border", "|"}, {"whitespace", " "}, {"table.text", "v"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", " \t\n"}, {"header.prefix", "#"}, {"header.text", " after"}, {"whitespace", "\n"}}},
		{"table-crlf-with-link", "| h |\r\n| --- |\r\n| [n](u) |\r\n# after\r\n", []typedText{{"table.border", "|"}, {"whitespace", " "}, {"table.text", "h"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\r\n"}, {"table.border", "|"}, {"whitespace", " "}, {"table.border", "---"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\r\n"}, {"table.border", "|"}, {"whitespace", " "}, {"link.[", "["}, {"link.name", "n"}, {"link.]", "]"}, {"link.(", "("}, {"link.url", "u"}, {"link.)", ")"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\r\n"}, {"header.prefix", "#"}, {"header.text", " after"}, {"whitespace", "\r\n"}}},
		{"two-line-table-boundary", "| h |\n| --- |\n# after\n", []typedText{{"table.border", "|"}, {"whitespace", " "}, {"table.text", "h"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"table.border", "|"}, {"whitespace", " "}, {"table.border", "---"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"header.prefix", "#"}, {"header.text", " after"}, {"whitespace", "\n"}}},
		{"table-at-eof", "| h |\n| --- |", []typedText{{"table.border", "|"}, {"whitespace", " "}, {"table.text", "h"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"table.border", "|"}, {"whitespace", " "}, {"table.border", "---"}, {"whitespace", " "}, {"table.border", "|"}}},
		{"escaped-table-pipe", "| a\\|b |\n| --- |\n", []typedText{{"table.border", "|"}, {"whitespace", " "}, {"table.text", "a\\|b"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}, {"table.border", "|"}, {"whitespace", " "}, {"table.border", "---"}, {"whitespace", " "}, {"table.border", "|"}, {"whitespace", "\n"}}},
		{"invalid-table-alignment", "| h |\n| -x- |\n| v |\n", []typedText{{"paragraph.text", "| h |"}, {"whitespace", "\n"}, {"paragraph.text", "| -x- |"}, {"whitespace", "\n"}, {"paragraph.text", "| v |"}, {"whitespace", "\n"}}},
		{"table-column-mismatch", "| a | b |\n| --- |\n", []typedText{{"paragraph.text", "| a | b |"}, {"whitespace", "\n"}, {"paragraph.text", "| --- |"}, {"whitespace", "\n"}}},
		{"unclosed-link", "[name](url", []typedText{{"paragraph.text", "[name](url"}}},
		{"unclosed-image", "![alt](url", []typedText{{"paragraph.text", "![alt](url"}}},
		{"unclosed-reference", "<web_42", []typedText{{"paragraph.text", "<web_42"}}},
		{"unclosed-inline-code", "`open", []typedText{{"paragraph.text", "`open"}}},
		{"unclosed-inline-equation", "$open", []typedText{{"paragraph.text", "$open"}}},
		{"unclosed-strong-at-newline", "**open\nnext", []typedText{{"strong.prefix", "**"}, {"strong.text", "open"}, {"whitespace", "\n"}, {"paragraph.text", "next"}}},
		{"unclosed-code-block", "```\nopen", []typedText{{"code.prefix", "```"}, {"code.text", "\nopen"}}},
		{"unclosed-equation-block", "$$open", []typedText{{"equation.prefix", "$$"}, {"equation.text", "open"}}},
		{"escaped-newline-in-code", "`a\\\nb`", []typedText{{"paragraph.text", "`a\\"}, {"whitespace", "\n"}, {"paragraph.text", "b`"}}},
		{"escaped-newline-in-equation", "$a\\\nb$", []typedText{{"paragraph.text", "$a\\"}, {"whitespace", "\n"}, {"paragraph.text", "b$"}}},
		{"crlf-boundary", "plain\r\n# title\r\n", []typedText{{"paragraph.text", "plain"}, {"whitespace", "\r\n"}, {"header.prefix", "#"}, {"header.text", " title"}, {"whitespace", "\r\n"}}},
		{"cr-boundary", "plain\rnext", []typedText{{"paragraph.text", "plain"}, {"whitespace", "\r"}, {"paragraph.text", "next"}}},
		{"nul-is-text", "before\x00after", []typedText{{"paragraph.text", "before\x00after"}}},
	}
	for _, kind := range []string{"doc", "web", "url"} {
		cases = append(cases, struct {
			name  string
			input string
			want  []typedText
		}{"reference-" + kind, "<" + kind + "_42>", []typedText{{"reference.prefix", "<"}, {ElemType("reference." + kind), kind + "_"}, {"reference.id", "42"}, {"reference.suffix", ">"}}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !t.Run("whole", func(t *testing.T) {
				assertSegments(t, parseChunks(t, MarkdownDoc, tc.input), tc.want...)
			}) {
				return
			}
			t.Run("rune-chunks", func(t *testing.T) {
				chunks := []string{""}
				for _, r := range tc.input {
					chunks = append(chunks, string(r))
				}
				chunks = append(chunks, "")
				assertSegments(t, parseChunks(t, MarkdownDoc, chunks...), tc.want...)
			})
			t.Run("byte-chunks", func(t *testing.T) {
				chunks := make([]string, len(tc.input))
				for i := range len(tc.input) {
					chunks[i] = tc.input[i : i+1]
				}
				assertSegments(t, parseChunks(t, MarkdownDoc, chunks...), tc.want...)
			})
		})
	}
}

func TestCustomElements(t *testing.T) {
	t.Run("leaf-whitespace-preserves-next-rune", func(t *testing.T) {
		block := MarkdownElem{
			Type: "custom", Tokenizer: String("custom.prefix", ">>>"),
			TrimWhites: []rune{' '}, TextElemType: "custom.text",
			SuffixPredictor: String("custom.suffix", "<<<"),
		}
		assertSegments(t, parseChunks(t, block, ">>>a b<<<"),
			typedText{"custom.prefix", ">>>"}, typedText{"custom.text", "a"},
			typedText{"whitespace", " "}, typedText{"custom.text", "b"}, typedText{"custom.suffix", "<<<"},
		)
	})
	t.Run("leaf-inline-stops-at-newline", func(t *testing.T) {
		header := HeaderElem
		header.SubElems = nil
		root := MarkdownDoc
		root.SubElems = []MarkdownElem{header, ParagraphElem}
		assertSegments(t, parseChunks(t, root, "# title\nbody"),
			typedText{"header.prefix", "#"}, typedText{"header.text", " title"},
			typedText{"whitespace", "\n"}, typedText{"paragraph.text", "body"},
		)
	})
	t.Run("inline-trimming-does-not-cross-newline", func(t *testing.T) {
		header := HeaderElem
		header.SubElems = nil
		header.TrimWhites = []rune{' ', '\n'}
		root := MarkdownDoc
		root.SubElems = []MarkdownElem{header, ParagraphElem}
		assertSegments(t, parseChunks(t, root, "#  \nbody"),
			typedText{"header.prefix", "#"}, typedText{"whitespace", "  \n"}, typedText{"paragraph.text", "body"},
		)
	})
	t.Run("zero-width-child-does-not-stall", func(t *testing.T) {
		root := MarkdownDoc
		root.SubElems = []MarkdownElem{{Tokenizer: Any()}, ParagraphElem}
		assertSegments(t, parseChunks(t, root, "plain"), typedText{"paragraph.text", "plain"})
	})
	t.Run("consumed-prefix-at-eof-is-not-retried", func(t *testing.T) {
		block := MarkdownElem{
			Tokenizer: String("custom.prefix", ">>>"), TrimWhites: []rune{' '}, TextElemType: "custom.text",
		}
		root := MarkdownDoc
		root.SubElems = []MarkdownElem{block, ParagraphElem}
		assertSegments(t, parseChunks(t, root, ">>> "), typedText{"custom.prefix", ">>>"}, typedText{"whitespace", " "})
	})
	t.Run("zero-repeat-in-sequence", func(t *testing.T) {
		root := MarkdownElem{Tokenizer: Sequence(Repeat("prefix", '#', 0, 0), String("hit", "x"))}
		assertSegments(t, parseChunks(t, root, "x"), typedText{"hit", "x"})
	})
	t.Run("literal-nul-is-not-eof", func(t *testing.T) {
		root := MarkdownDoc
		root.SubElems = []MarkdownElem{{Tokenizer: String("literal", "a\x00")}, ParagraphElem}
		assertSegments(t, parseChunks(t, root, "a"), typedText{"paragraph.text", "a"})
	})
}

func TestMarkdownDelimiterSplitPositions(t *testing.T) {
	inputs := []string{
		"**中文 _italic_**", "__bold *italic*__", "[a\\]b](u\\)v)", "![中文](image.png)",
		"```go\nx := \"```\"\n```\n", "| h |\n| --- |\n# after\n", "\\*literal\\*", "plain\r\n# h\r\n",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			whole := parseChunks(t, MarkdownDoc, input)
			for offset := range len(input) - 1 {
				cut := offset + 1
				t.Run(fmt.Sprintf("byte-%d", cut), func(t *testing.T) {
					got := parseChunks(t, MarkdownDoc, input[:cut], "", input[cut:])
					if segmentText(got) != input || !slices.Equal(got, whole) {
						t.Fatalf("classification changed at byte boundary %d: %#v", cut, got)
					}
				})
			}
		})
	}
}

func TestTableAlignmentForms(t *testing.T) {
	for _, alignment := range []string{"---", ":---", "---:", ":---:"} {
		t.Run(alignment, func(t *testing.T) {
			input := "| h |\n| " + alignment + " |\n"
			got := parseChunks(t, MarkdownDoc, input)
			assertSegments(t, got,
				typedText{"table.border", "|"}, typedText{"whitespace", " "}, typedText{"table.text", "h"},
				typedText{"whitespace", " "}, typedText{"table.border", "|"}, typedText{"whitespace", "\n"},
				typedText{"table.border", "|"}, typedText{"whitespace", " "}, typedText{"table.border", alignment},
				typedText{"whitespace", " "}, typedText{"table.border", "|"}, typedText{"whitespace", "\n"},
			)
		})
	}
	for _, alignment := range []string{"", "--", "x---", "---x", "---::", ": :"} {
		t.Run("invalid-"+alignment, func(t *testing.T) {
			input := "| h |\n| " + alignment + " |\n"
			got := parseChunks(t, MarkdownDoc, input)
			if segmentText(got) != input {
				t.Fatalf("invalid alignment discarded input: %q", segmentText(got))
			}
			for _, seg := range got {
				if strings.HasPrefix(string(seg.Type), "table.") {
					t.Fatalf("invalid alignment %q was accepted as a table", alignment)
				}
			}
		})
	}
}
