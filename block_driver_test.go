package markdown

import "testing"

func incrementalBlockDocument(t *testing.T, source string) *Document {
	t.Helper()
	decoded, err := decodeSource(source, UTF8)
	if err != nil {
		t.Fatal(err)
	}
	root := makeNode(KindDocument, "document", decoded.view())
	document := &Document{
		Root:          root,
		Definitions:   make(map[string]Definition),
		source:        decoded,
		footnotes:     make(map[string]*Node),
		footnoteCalls: make(map[string]int),
	}
	state := newParseState(document, Options{})
	lines := sourceLines(decoded.view())
	driver := &blockDriver{s: state, parent: root, stack: []*blockFrame{{node: root, kind: root.Kind}}}
	for cut := range len(lines) {
		if err := driver.feed(lines[:cut+1], false); err != nil {
			t.Fatal(err)
		}
	}
	if err := driver.feed(lines, true); err != nil {
		t.Fatal(err)
	}
	if err := state.completeDocument(); err != nil {
		t.Fatal(err)
	}
	document.Tokens = root.token.Children
	document.Events = appendEvents(document.Tokens, nil)
	document.Root = cloneDocumentAST(document)
	return document
}

func TestBlockDriverLineFeedMatchesFullParse(t *testing.T) {
	sources := []string{
		"# h\n\npara\n",
		"[foo]\n\n[foo]: /url\n",
		"- a\n- b\n\npara\n",
		"> quote\n\npara\n",
		"```go\ncode\n```\n\ntail\n",
		"| a | b |\n| - | - |\n| c | d |\n\ntail\n",
		"para\n---\n",
		"<div>\nx\n</div>\n\ntail\n",
		"    code\n\npara\n",
		"one\n\ntwo\n\nthree\n",
	}
	for index, source := range sources {
		full, err := Parse(source)
		if err != nil {
			t.Fatal(err)
		}
		streamed := incrementalBlockDocument(t, source)
		if len(streamed.Events) != len(full.Events) {
			t.Fatalf("source %d: events %d, want %d", index, len(streamed.Events), len(full.Events))
		}
		for event := range streamed.Events {
			if streamEventSig(streamed.Events[event]) != streamEventSig(full.Events[event]) {
				t.Fatalf("source %d event %d: got %#v, want %#v", index, event, streamEventSig(streamed.Events[event]), streamEventSig(full.Events[event]))
			}
		}
		if got, want := rendered(t, streamed), rendered(t, full); got != want {
			t.Fatalf("source %d HTML: %q, want %q", index, got, want)
		}
	}
}
