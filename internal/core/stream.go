package core

import (
	"strings"
	"sync"
)

// TokenStream emits confirmed concrete tokens while input is still arriving.
// Blocks are parsed incrementally by blockDriver; a block is emitted only after
// it can no longer change. Blocks with possible forward references (a "[" or
// "[^" candidate), open containers, and footnote definitions are held until
// Close. Emitted events are always a prefix of the events Parse would produce
// for the complete input; no token is withdrawn. Options with resolvers or
// custom tokenizers fall back to a full parse at Close.
//
// TokenStream does not replace Parse or Parser.Read. It is an opt-in view for
// consumers that need blocks earlier, not an incremental Document.
type TokenStream struct {
	mu      sync.Mutex
	options Options
	early   bool

	buffer strings.Builder
	closed bool
	dirty  bool
	done   bool
	err    error

	queue []Event

	started       bool
	finished      bool
	decoder       *sourceDecoder
	decoded       int
	decoderClosed bool
	lines         []sourceLine
	lineStart     int
	source        *sourceDocument
	document      *Document
	state         *parseState
	driver        *blockDriver
}

func streamEventSig(event Event) streamSig {
	return streamSig{
		kind:   event.Kind,
		typ:    event.Token.Type,
		start:  event.Token.Range.Start.ByteOffset,
		end:    event.Token.Range.End.ByteOffset,
		source: event.Token.Source(),
	}
}

type streamSig struct {
	kind   EventKind
	typ    TokenType
	start  int
	end    int
	source string
}

// NewTokenStream starts a streaming token reader. It accepts the same Options
// as Parse, and extension values must stay unchanged while parsing.
func NewTokenStream(options ...Options) *TokenStream {
	var configuration Options
	if len(options) != 0 {
		configuration = options[0]
	}
	return &TokenStream{options: configuration, early: streamSyntaxStable(configuration)}
}

// Put appends one input chunk. It does not parse until Next needs output.
func (s *TokenStream) Put(chunk string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if chunk != "" {
		s.buffer.WriteString(chunk)
		s.dirty = true
	}
	return nil
}

// Write implements io.Writer.
func (s *TokenStream) Write(chunk []byte) (int, error) {
	if err := s.Put(string(chunk)); err != nil {
		return 0, err
	}
	return len(chunk), nil
}

// Close ends input. Remaining blocks are emitted by Next.
func (s *TokenStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.closed, s.dirty = true, true
	return nil
}

// Next returns the next confirmed event. ReadPending means no further token can
// be confirmed from the current input; ReadDone means the stream is exhausted.
func (s *TokenStream) Next() (Event, ReadStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return Event{}, ReadDone, s.err
	}
	for {
		if len(s.queue) != 0 {
			event := s.queue[0]
			s.queue = s.queue[1:]
			return event, ReadEvent, nil
		}
		if s.dirty {
			s.refresh()
			if s.err != nil {
				return Event{}, ReadDone, s.err
			}
			continue
		}
		if s.closed {
			return Event{}, ReadDone, nil
		}
		return Event{}, ReadPending, nil
	}
}

func (s *TokenStream) start() error {
	if s.started {
		return nil
	}
	decoder, err := newSourceDecoder(s.options.Encoding)
	if err != nil {
		return err
	}
	s.decoder = decoder
	s.source = decoder.Document()
	root := makeNode(KindDocument, "document", s.source.view())
	s.document = &Document{
		Root:          root,
		Definitions:   make(map[string]Definition),
		source:        s.source,
		options:       s.options,
		footnotes:     make(map[string]*Node),
		footnoteCalls: make(map[string]int),
	}
	s.state = newParseState(s.document, s.options)
	s.driver = &blockDriver{s: s.state, parent: root, stack: []*blockFrame{{node: root, kind: root.Kind}}}
	s.started = true
	return nil
}

func (s *TokenStream) refresh() {
	s.dirty = false
	if !s.early {
		if !s.closed {
			return
		}
		document, err := Parse(s.buffer.String(), s.options)
		if err != nil {
			s.err = err
			return
		}
		s.queue = append(s.queue, document.Events...)
		return
	}
	if err := s.start(); err != nil {
		s.err = err
		return
	}
	raw := s.buffer.String()
	if len(raw) > s.decoded {
		s.decoder.WriteString(raw[s.decoded:])
		s.decoded = len(raw)
	}
	if s.closed && !s.decoderClosed {
		s.decoder.Close()
		s.decoderClosed = true
	}
	s.appendLines(s.closed)
	if s.closed {
		if err := s.driver.feed(s.lines, true); err != nil {
			s.err = err
			return
		}
		if err := s.finish(); err != nil {
			s.err = err
		}
		return
	}
	if err := s.driver.feed(s.lines, false); err != nil {
		s.err = err
		return
	}
	if err := s.drain(); err != nil {
		s.err = err
	}
}

func (s *TokenStream) appendLines(closed bool) {
	text := s.source.text
	view := s.source.view()
	start := s.lineStart
	for index := start; index < len(text); {
		if text[index] != '\n' && text[index] != '\r' {
			index++
			continue
		}
		end := index + 1
		if text[index] == '\r' && end < len(text) && text[end] == '\n' {
			end++
		}
		if text[index] == '\r' && end == len(text) && !closed {
			break
		}
		s.lines = append(s.lines, sourceLine{body: view.slice(start, index), ending: view.slice(index, end)})
		start, index = end, end
	}
	if closed && start < len(text) {
		s.lines = append(s.lines, sourceLine{body: view.slice(start, len(text)), ending: view.slice(len(text), len(text))})
		start = len(text)
	}
	s.lineStart = start
}

// drain emits safe leading top-level blocks. Each block is finalized on its own;
// blocks without forward-reference candidates need no later definitions.
func (s *TokenStream) drain() error {
	root := s.document.Root.token
	for len(root.Children) != 0 {
		token := root.Children[0]
		if token.node == nil {
			s.queue = append(s.queue, appendEvents([]*Token{token}, nil)...)
			root.Children = root.Children[1:]
			continue
		}
		hasFollowing, blankAfter := streamNeighbours(root.Children[1:])
		if !streamBlockSafe(token, hasFollowing, blankAfter, s.source.raw) {
			break
		}
		if err := s.finalizeEarly(token); err != nil {
			return err
		}
		s.queue = append(s.queue, appendEvents([]*Token{token}, nil)...)
		root.Children = root.Children[1:]
	}
	return nil
}

func (s *TokenStream) finalizeEarly(token *Token) error {
	if err := s.state.expandBlockContent(token); err != nil {
		return err
	}
	if err := s.state.expandStringContent(token); err != nil {
		return err
	}
	s.state.finalizeBlockValues(token)
	if token.node != nil {
		if err := s.state.parseDeferredInlines(token.node); err != nil {
			return err
		}
	}
	if err := s.state.expandInlineContent(token); err != nil {
		return err
	}
	mergeRecordedPrefixes(token)
	return nil
}

// finish resolves all remaining blocks in two passes: collect definitions and
// footnotes first, then parse inlines and emit in document order.
func (s *TokenStream) finish() error {
	if s.finished {
		return nil
	}
	root := s.document.Root.token
	tokens := root.Children
	for _, token := range tokens {
		if token.node == nil {
			continue
		}
		if err := s.state.expandBlockContent(token); err != nil {
			return err
		}
		if err := s.state.expandStringContent(token); err != nil {
			return err
		}
		s.state.finalizeBlockValues(token)
	}
	for _, token := range tokens {
		if token.node != nil {
			if err := s.state.parseDeferredInlines(token.node); err != nil {
				return err
			}
			if err := s.state.expandInlineContent(token); err != nil {
				return err
			}
			mergeRecordedPrefixes(token)
		}
		s.queue = append(s.queue, appendEvents([]*Token{token}, nil)...)
	}
	root.Children = nil
	s.finished = true
	return nil
}

// streamSyntaxStable reports whether early emission is safe for these options.
// Resolvers and custom tokenizers may rewrite already parsed tokens, so they
// conservatively disable early emission.
func streamSyntaxStable(options Options) bool {
	for _, extension := range options.Extensions {
		if len(extension.InsideSpan) != 0 {
			return false
		}
		for _, table := range syntaxTables(extension) {
			for _, constructs := range table {
				for index := range constructs {
					construct := &constructs[index]
					if construct.Tokenize != nil || construct.Continuation != nil || construct.Exit != nil {
						return false
					}
					if construct.Resolve != nil || construct.ResolveTo != nil || construct.ResolveAll != nil {
						return false
					}
				}
			}
		}
	}
	return true
}

func streamNeighbours(tokens []*Token) (hasFollowing, blankAfter bool) {
	for _, token := range tokens {
		if token.node != nil {
			return true, blankAfter
		}
		if token.Type == "lineEndingBlank" {
			blankAfter = true
		}
	}
	return false, blankAfter
}

func streamLineDone(buffer string, end int) bool {
	if end < 0 || end > len(buffer) {
		return false
	}
	return strings.IndexAny(buffer[end:], "\r\n") >= 0
}

func streamSourceEndsLine(source string) bool {
	return strings.HasSuffix(source, "\n") || strings.HasSuffix(source, "\r")
}

func streamBlockSafe(token *Token, hasFollowing, blankAfter bool, buffer string) bool {
	node := token.node
	if node == nil {
		return false
	}
	hasRef := strings.Contains(token.Source(), "[")
	switch node.Kind {
	case KindHeading:
		if hasRef {
			return false
		}
		if token.Type == "atxHeading" || token.Type == "setextHeading" {
			return hasFollowing || streamLineDone(buffer, token.Range.End.ByteOffset)
		}
		return false
	case KindThematicBreak:
		return hasFollowing || streamLineDone(buffer, token.Range.End.ByteOffset)
	case KindCodeBlock:
		switch token.Type {
		case "codeFenced":
			return streamClosed(token) && (hasFollowing || streamSourceEndsLine(token.Source()))
		case "codeIndented":
			return hasFollowing
		}
		return false
	case KindMathBlock:
		return streamClosed(token) && (hasFollowing || streamSourceEndsLine(token.Source()))
	case KindHTMLBlock:
		if hasFollowing {
			return true
		}
		data := blockCaptured(token, "htmlFlowData")
		if data == nil || data.Data == nil {
			return false
		}
		class, _ := data.Data["class"].(int)
		end, _ := data.Data["end"].(string)
		if class >= 6 {
			return blankAfter
		}
		if end == "" {
			return false
		}
		if class == 1 {
			return strings.Contains(strings.ToLower(token.Source()), strings.ToLower(end))
		}
		return strings.Contains(token.Source(), end)
	case KindParagraph:
		return (hasFollowing || blankAfter) && !hasRef
	case KindList, KindBlockQuote:
		return hasFollowing && !hasRef
	case KindTable:
		return (hasFollowing || blankAfter) && !hasRef
	case KindDefinition:
		return hasFollowing || blankAfter
	case KindFootnoteDefinition:
		return false
	}
	return false
}

func streamClosed(token *Token) bool {
	if token.node == nil || token.node.Data == nil {
		return false
	}
	closed, _ := token.node.Data["closed"].(bool)
	return closed
}
