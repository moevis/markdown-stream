package core

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

//go:embed testdata/commonmark.json
var commonMarkFixtures []byte

func rendered(t *testing.T, document *Document) string {
	t.Helper()
	var output strings.Builder
	if err := RenderHTML(&output, document); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func assertHTML(t *testing.T, source, expected string, options ...Options) {
	t.Helper()
	actual, err := HTML(source, options...)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("source: %q\nactual: %q\nexpected: %q", source, actual, expected)
	}
}

func TestCommonMark(t *testing.T) {
	var fixtures []struct {
		Markdown string `json:"markdown"`
		HTML     string `json:"html"`
		Example  int    `json:"example"`
		Section  string `json:"section"`
	}
	if err := json.Unmarshal(commonMarkFixtures, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%03d/%s", fixture.Example, fixture.Section), func(t *testing.T) {
			assertHTML(t, fixture.Markdown, fixture.HTML, Options{AllowDangerousHTML: true, AllowDangerousProtocol: true})
		})
	}
}

func TestASTEditsDoNotRewriteConcreteSyntax(t *testing.T) {
	document, err := Parse("one *two*")
	if err != nil {
		t.Fatal(err)
	}
	paragraph := document.Root.Children[0]
	for _, child := range paragraph.Children {
		if child.Kind == KindEmphasis {
			child.Kind = KindStrong
			child.Children = nil
			child.AppendChild(&Node{Kind: KindText, Literal: "changed"})
		}
	}
	paragraph.AppendChild(&Node{Kind: KindText, Literal: "!"})
	var edited strings.Builder
	if err := RenderAST(&edited, document.Root); err != nil {
		t.Fatal(err)
	}
	if edited.String() != "<p>one <strong>changed</strong>!</p>" {
		t.Fatalf("edited AST: %q", edited.String())
	}
	if actual := rendered(t, document); actual != "<p>one <em>two</em></p>" {
		t.Fatalf("concrete syntax changed: %q", actual)
	}
}

func TestHTMLSafety(t *testing.T) {
	assertHTML(t, "<b>x</b> [x](javascript:alert%281%29)", "<p>&lt;b&gt;x&lt;/b&gt; <a href=\"\">x</a></p>")
	assertHTML(t, "<b>x</b> [x](javascript:alert%281%29)", "<p><b>x</b> <a href=\"javascript:alert%281%29\">x</a></p>", Options{AllowDangerousHTML: true, AllowDangerousProtocol: true})
	assertHTML(t, "![x](data:image/png;base64,YQ==)", "<p><img src=\"\" alt=\"x\" /></p>")
	assertHTML(t, "![x](data:text/html,YQ==)", "<p><img src=\"\" alt=\"x\" /></p>")
}

func TestReferencesRequireExplicitExtension(t *testing.T) {
	const input = "<doc_intro> <web_1> <url_1>"
	const expected = "<p>&lt;doc_intro&gt; &lt;web_1&gt; &lt;url_1&gt;</p>"
	assertHTML(t, input, expected)
	assertHTML(t, input, expected, Options{Extensions: []SyntaxExtension{GFM()}, HTMLExtensions: []HTMLExtension{GFMHTML()}})
}

func TestGFMAndMathAreExplicit(t *testing.T) {
	assertHTML(t, "~a~ ~~b~~", "<p>~a~ ~~b~~</p>")
	assertHTML(t, "~a~ ~~b~~", "<p><del>a</del> <del>b</del></p>", Options{Extensions: []SyntaxExtension{GFM()}, HTMLExtensions: []HTMLExtension{GFMHTML()}})
	assertHTML(t, "~a~ ~~b~~", "<p>~a~ <del>b</del></p>", Options{Extensions: []SyntaxExtension{GFM(GFMOptions{SingleTilde: false})}, HTMLExtensions: []HTMLExtension{GFMHTML()}})
	assertHTML(t, "$x < y$", "<p><span class=\"math math-inline\">x &lt; y</span></p>", Options{Extensions: []SyntaxExtension{Math()}, HTMLExtensions: []HTMLExtension{MathHTML()}})
}

func TestReferenceLabelUTF16Limit(t *testing.T) {
	for _, alphabet := range []struct {
		name, symbol string
		width        int
	}{{"ascii", "a", 1}, {"astral", "😀", 2}} {
		for _, units := range []int{999, 1000} {
			t.Run(fmt.Sprintf("%s/%d", alphabet.name, units), func(t *testing.T) {
				label := alphabet.symbol + strings.Repeat(" ", units-alphabet.width-1) + "b"
				definition := alphabet.symbol + " b"
				expected := "<p><a href=\"/target\">x</a></p>\n"
				if units == 1000 {
					expected = "<p>[x]<a href=\"/target\">" + label + "</a></p>\n"
				}
				assertHTML(t, "[x]["+label+"]\n\n["+definition+"]: /target", expected)
			})
		}
	}
}

func TestReferenceLinkTextHasNoRawLabelLimit(t *testing.T) {
	for _, alphabet := range []struct {
		name, symbol string
		width        int
	}{{"ascii", "a", 1}, {"astral", "😀", 2}} {
		label := alphabet.symbol + strings.Repeat(" ", 1000-alphabet.width-1) + "b"
		for _, test := range []struct{ name, suffix string }{
			{"shortcut", ""}, {"collapsed", "[]"},
		} {
			t.Run(alphabet.name+"/"+test.name, func(t *testing.T) {
				assertHTML(t, "["+label+"]"+test.suffix+"\n\n["+alphabet.symbol+" b]: /target", "<p><a href=\"/target\">"+label+"</a></p>\n")
			})
		}
	}
}

func TestDefinitionLabelUTF16Limit(t *testing.T) {
	for _, alphabet := range []struct {
		name, symbol string
		width        int
	}{{"ascii", "a", 1}, {"astral", "😀", 2}} {
		for _, units := range []int{999, 1000} {
			t.Run(fmt.Sprintf("%s/%d", alphabet.name, units), func(t *testing.T) {
				label := strings.Repeat(alphabet.symbol, units/alphabet.width) + strings.Repeat("a", units%alphabet.width)
				expected := "<p><a href=\"/target\">x</a></p>"
				if units == 1000 {
					expected = "<p>[" + label + "]: /target</p>\n<p>[x][" + label + "]</p>"
				}
				assertHTML(t, "["+label+"]: /target\n\n[x]["+label+"]", expected)
			})
		}
	}
}

func TestURIPercentNormalization(t *testing.T) {
	for _, test := range []struct{ destination, normalized string }{
		{"a%2F", "a%2F"},
		{"a%2f", "a%2f"},
		{"a%zz", "a%zz"},
		{"a%ag", "a%ag"},
		{"a%0G", "a%0G"},
		{"a%", "a%25"},
		{"a%a", "a%25a"},
		{"a%?b", "a%25?b"},
	} {
		t.Run(test.destination, func(t *testing.T) {
			expected := "<p><a href=\"" + test.normalized + "\">x</a></p>"
			assertHTML(t, "[x]("+test.destination+")", expected)
			assertHTML(t, "[x]\n\n[x]: "+test.destination, expected+"\n")
		})
	}
}
