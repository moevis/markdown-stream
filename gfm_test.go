package markdown

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

//go:embed testdata/gfm.json
var gfmFixtures []byte

type gfmOracleComponent struct {
	Component  string                     `json:"component"`
	Side       string                     `json:"side"`
	Options    map[string]json.RawMessage `json:"options"`
	Disable    map[string][]string        `json:"disable"`
	SyntaxKeys []string                   `json:"syntaxKeys"`
}

type gfmOracleFixture struct {
	ID             string                     `json:"id"`
	Suite          string                     `json:"suite"`
	Origins        []string                   `json:"origins"`
	Source         string                     `json:"source"`
	Bytes          *string                    `json:"bytes"`
	Syntax         []gfmOracleComponent       `json:"syntax"`
	HTMLComponents []gfmOracleComponent       `json:"htmlExtensions"`
	Options        map[string]json.RawMessage `json:"options"`
	Callbacks      []json.RawMessage          `json:"callbacks"`
	HTML           string                     `json:"html"`
}

func gfmOracleDecode(t *testing.T, data []byte, value any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		t.Fatal(err)
	}
}

func gfmOracleSyntax(t *testing.T, component gfmOracleComponent) SyntaxExtension {
	t.Helper()
	if component.Component == "custom" {
		if component.Side != "" || len(component.Options) != 0 || len(component.SyntaxKeys) != 1 || component.SyntaxKeys[0] != "disable" {
			t.Fatalf("unsupported custom syntax: %+v", component)
		}
		var extension SyntaxExtension
		for key, names := range component.Disable {
			if key != "null" {
				t.Fatalf("unsupported disable key %q", key)
			}
			for _, name := range names {
				switch name {
				case "table", "tasklistCheck", "strikethrough", "emailAutolink", "wwwAutolink", "protocolAutolink", "gfmFootnoteDefinition", "gfmFootnoteCall", "gfmPotentialFootnoteCall", "codeIndented":
					extension.Disable = append(extension.Disable, name)
				default:
					t.Fatalf("unknown disabled construct %q", name)
				}
			}
		}
		return extension
	}
	if component.Side != "syntax" || len(component.Disable) != 0 || len(component.SyntaxKeys) != 0 {
		t.Fatalf("unsupported syntax descriptor: %+v", component)
	}
	extension := GFM()
	for key, value := range component.Options {
		if component.Component != "strikethrough" || key != "singleTilde" {
			t.Fatalf("unknown %s syntax option %q", component.Component, key)
		}
		var single bool
		gfmOracleDecode(t, value, &single)
		extension = GFM(GFMOptions{SingleTilde: single})
	}
	allowed := map[string]bool{}
	switch component.Component {
	case "table":
		allowed["table"] = true
	case "task-list-item":
		allowed["tasklistCheck"] = true
	case "strikethrough":
		allowed["strikethrough"] = true
	case "autolink-literal":
		for _, name := range []string{"emailAutolink", "wwwAutolink", "protocolAutolink"} {
			allowed[name] = true
		}
	case "footnote":
		for _, name := range []string{"gfmFootnoteDefinition", "gfmFootnoteCall", "gfmPotentialFootnoteCall"} {
			allowed[name] = true
		}
	default:
		t.Fatalf("unknown syntax component %q", component.Component)
	}
	filter := func(table map[Code][]Construct) map[Code][]Construct {
		result := make(map[Code][]Construct)
		for code, constructs := range table {
			for _, construct := range constructs {
				if allowed[construct.Name] {
					result[code] = append(result[code], construct)
				}
			}
		}
		return result
	}
	extension.Document = filter(extension.Document)
	extension.FlowInitial = filter(extension.FlowInitial)
	extension.Flow = filter(extension.Flow)
	extension.ContentInitial = filter(extension.ContentInitial)
	extension.Text = filter(extension.Text)
	extension.String = filter(extension.String)
	if component.Component != "strikethrough" {
		extension.AttentionMarkers = nil
		extension.InsideSpan = nil
	}
	return extension
}

func gfmOracleHTML(t *testing.T, component gfmOracleComponent) HTMLExtension {
	t.Helper()
	if component.Side != "html" || len(component.Disable) != 0 || len(component.SyntaxKeys) != 0 {
		t.Fatalf("unsupported HTML descriptor: %+v", component)
	}
	var config GFMHTMLOptions
	for key, raw := range component.Options {
		if component.Component != "footnote" {
			t.Fatalf("unknown %s HTML option %q", component.Component, key)
		}
		var value string
		gfmOracleDecode(t, raw, &value)
		switch key {
		case "label":
			config.Label = value
		case "labelTagName":
			config.LabelTagName = value
		case "labelAttributes":
			config.LabelAttributes = &value
		case "clobberPrefix":
			config.ClobberPrefix = &value
		case "backLabel":
			config.BackLabelText = value
		default:
			t.Fatalf("unknown footnote HTML option %q", key)
		}
	}
	extension := GFMHTML(config)
	allowed := map[TokenType]bool{}
	switch component.Component {
	case "table":
		for _, name := range []TokenType{"table", "tableRow", "tableCell", "tableHeader", "tableData"} {
			allowed[name] = true
		}
	case "task-list-item":
		for _, name := range []TokenType{"taskListCheck", "taskListCheckValue", "taskListCheckValueChecked"} {
			allowed[name] = true
		}
	case "strikethrough":
		allowed["strikethrough"] = true
	case "autolink-literal":
		for _, name := range []TokenType{"literalAutolink", "literalAutolinkEmail", "literalAutolinkHttp", "literalAutolinkWww"} {
			allowed[name] = true
		}
	case "footnote":
		for _, name := range []TokenType{"gfmFootnoteCall", "footnoteDefinition", "gfmFootnoteDefinition", "footnoteDefinitionPrefix"} {
			allowed[name] = true
		}
	case "tagfilter":
		allowed["htmlFlowData"], allowed["htmlTextData"] = true, true
	default:
		t.Fatalf("unknown HTML component %q", component.Component)
	}
	for name := range extension.Enter {
		if !allowed[name] {
			delete(extension.Enter, name)
		}
	}
	for name := range extension.Exit {
		if !allowed[name] {
			delete(extension.Exit, name)
		}
	}
	extension.filterTags = component.Component == "tagfilter"
	extension.gfm = component.Component != "tagfilter"
	if component.Component != "footnote" {
		extension.EnterDocument = nil
		extension.ExitDocument = nil
	}
	return extension
}

func TestGFM(t *testing.T) {
	var captured struct {
		Provenance struct {
			Consumer   string `json:"consumer"`
			Capture    string `json:"capture"`
			OriginRoot string `json:"originRoot"`
		} `json:"provenance"`
		Fixtures []gfmOracleFixture `json:"fixtures"`
	}
	gfmOracleDecode(t, gfmFixtures, &captured)
	for _, fixture := range captured.Fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			if len(fixture.Callbacks) != 0 {
				t.Fatal("unsupported oracle callbacks")
			}
			var options Options
			for key, value := range fixture.Options {
				switch key {
				case "allowDangerousHtml":
					gfmOracleDecode(t, value, &options.AllowDangerousHTML)
				case "allowDangerousProtocol":
					gfmOracleDecode(t, value, &options.AllowDangerousProtocol)
				case "encoding":
					gfmOracleDecode(t, value, &options.Encoding)
				case "defaultLineEnding":
					gfmOracleDecode(t, value, &options.DefaultLineEnding)
				default:
					t.Fatalf("unknown consumer option %q", key)
				}
			}
			for _, component := range fixture.Syntax {
				options.Extensions = append(options.Extensions, gfmOracleSyntax(t, component))
			}
			for _, component := range fixture.HTMLComponents {
				options.HTMLExtensions = append(options.HTMLExtensions, gfmOracleHTML(t, component))
			}
			if fixture.Bytes == nil {
				assertHTML(t, fixture.Source, fixture.HTML, options)
				return
			}
			input, err := base64.StdEncoding.DecodeString(*fixture.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			document, err := ParseBytes(input, options)
			if err != nil {
				t.Fatal(err)
			}
			if actual := rendered(t, document); actual != fixture.HTML {
				t.Fatal(fmt.Sprintf("origins: %v\nactual: %q\nexpected: %q", fixture.Origins, actual, fixture.HTML))
			}
		})
	}
}

func TestGFMSyntaxOnlyKeepsTaskSeparator(t *testing.T) {
	assertHTML(t, "- [x] done", "<ul>\n<li> done</li>\n</ul>", Options{Extensions: []SyntaxExtension{GFM()}})
}

func TestGFMFootnoteLabelUTF16Limit(t *testing.T) {
	options := Options{Extensions: []SyntaxExtension{GFM()}, HTMLExtensions: []HTMLExtension{GFMHTML()}}
	for _, alphabet := range []struct {
		name, symbol string
		width        int
	}{{"ascii", "a", 1}, {"astral", "😀", 2}} {
		for _, units := range []int{999, 1000} {
			t.Run(fmt.Sprintf("%s/%d", alphabet.name, units), func(t *testing.T) {
				label := strings.Repeat(alphabet.symbol, units/alphabet.width) + strings.Repeat("a", units%alphabet.width)
				document, err := Parse("[^"+label+"]: note\n\n[^"+label+"]", options)
				if err != nil {
					t.Fatal(err)
				}
				definitions, calls := 0, 0
				document.Root.Walk(func(node *Node) bool {
					if node.Kind == KindFootnoteDefinition {
						definitions++
					} else if node.Kind == KindFootnoteReference {
						calls++
					}
					return true
				})
				if units == 999 {
					if definitions != 1 || calls != 1 {
						t.Fatalf("valid footnote: definitions=%d calls=%d", definitions, calls)
					}
					if html := rendered(t, document); !strings.Contains(html, "data-footnote-ref") || !strings.Contains(html, "data-footnotes") {
						t.Fatalf("valid footnote was not rendered: %q", html)
					}
					return
				}
				if definitions != 0 || calls != 0 {
					t.Fatalf("oversized footnote: definitions=%d calls=%d", definitions, calls)
				}
				if html, expected := rendered(t, document), "<p>[^"+label+"]: note</p>\n<p>[^"+label+"]</p>"; html != expected {
					t.Fatalf("oversized label did not remain literal: %q", html)
				}
			})
		}
	}
}

func TestGFMFootnoteIdentifierPercentEscapes(t *testing.T) {
	prefix := "test-"
	options := Options{Extensions: []SyntaxExtension{GFM()}, HTMLExtensions: []HTMLExtension{GFMHTML(GFMHTMLOptions{ClobberPrefix: &prefix})}}
	for _, test := range []struct{ label, identifier string }{
		{"a%zz", "a%zz"},
		{"a%ag", "a%ag"},
		{"a%0G", "a%0g"},
		{"a%2F", "a%2f"},
		{"a%", "a%25"},
	} {
		t.Run(test.label, func(t *testing.T) {
			html, err := HTML("[^"+test.label+"]: note\n\n[^"+test.label+"]", options)
			if err != nil {
				t.Fatal(err)
			}
			for _, attribute := range []string{
				`href="#test-fn-` + test.identifier + `"`,
				`id="test-fnref-` + test.identifier + `"`,
				`id="test-fn-` + test.identifier + `"`,
				`href="#test-fnref-` + test.identifier + `"`,
			} {
				if !strings.Contains(html, attribute) {
					t.Fatalf("footnote target %q missing from %q", attribute, html)
				}
			}
		})
	}
}
