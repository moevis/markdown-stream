package core

import (
	"errors"
	"io"
	"strings"
)

// EOF is distinct from every Unicode scalar and preprocessing sentinel.
const EOF = -1

// ErrClosed reports a write or close after the stream has been closed.
var ErrClosed = errors.New("markdown: input is closed")

// Parse confirms a complete document using the native grammar.
func Parse(source string, options ...Options) (*Document, error) {
	configuration := Options{}
	if len(options) != 0 {
		configuration = options[0]
	}
	decoded, err := decodeSource(source, configuration.Encoding)
	if err != nil {
		return nil, err
	}
	root := makeNode(KindDocument, "document", decoded.view())
	document := &Document{Root: root, Definitions: make(map[string]Definition), source: decoded, options: configuration, footnotes: make(map[string]*Node), footnoteCalls: make(map[string]int)}
	state := newParseState(document, configuration)
	if err := state.parseBlocks(sourceLines(decoded.view()), root); err != nil {
		return nil, err
	}
	if err := state.completeDocument(); err != nil {
		return nil, err
	}
	document.Tokens = root.token.Children
	document.Events = appendEvents(document.Tokens, nil)
	document.Root = cloneDocumentAST(document)
	return document, nil
}

// ParseBytes preserves original bytes, including encoded or invalid input.
func ParseBytes(source []byte, options ...Options) (*Document, error) {
	return Parse(string(source), options...)
}

// ParseReader propagates reader errors rather than treating truncated input as
// a successful complete document.
func ParseReader(reader io.Reader, options ...Options) (*Document, error) {
	var source strings.Builder
	if _, err := io.Copy(&source, reader); err != nil {
		return nil, err
	}
	return Parse(source.String(), options...)
}
