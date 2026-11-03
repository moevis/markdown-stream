package markdown

import (
	"fmt"
	"strings"
	"sync"
)

// TokenStream emits confirmed concrete tokens while input is still arriving.
// A block is emitted only after it can no longer change: a closed fenced code
// block, a completed ATX/Setext heading, a closed list or table, and so on.
// Blocks with possible forward references (a "[" or "[^" candidate) and open
// containers are held until Close. Emitted events are always a prefix of the
// events Parse would produce for the complete input; no token is withdrawn.
//
// TokenStream does not replace Parse or Parser.Read. It is an opt-in view for
// consumers that need blocks earlier, not an incremental Document.
type TokenStream struct {
	mu      sync.Mutex
	options Options
	buffer  strings.Builder
	early   bool

	closed bool
	dirty  bool
	err    error

	queue   []Event
	emitted int
	sigs    []streamSig
}

type streamSig struct {
	kind   EventKind
	typ    TokenType
	start  int
	end    int
	source string
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

func (s *TokenStream) refresh() {
	s.dirty = false
	document, err := Parse(s.buffer.String(), s.options)
	if err != nil {
		s.err = err
		return
	}
	events := document.Events
	if len(events) < s.emitted {
		s.err = fmt.Errorf("markdown: token stream prefix shrank")
		return
	}
	for index := range s.emitted {
		if streamEventSig(events[index]) != s.sigs[index] {
			s.err = fmt.Errorf("markdown: token stream prefix changed at event %d", index)
			return
		}
	}
	safe := 0
	if s.closed {
		safe = len(events)
	} else if s.early {
		safe = streamSafeEventCount(document, s.buffer.String())
	}
	if safe < s.emitted {
		s.err = fmt.Errorf("markdown: token stream prefix regressed")
		return
	}
	if safe > s.emitted {
		s.queue = append(s.queue, events[s.emitted:safe]...)
		for _, event := range events[s.emitted:safe] {
			s.sigs = append(s.sigs, streamEventSig(event))
		}
		s.emitted = safe
	}
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

// streamSafeEventCount returns how many leading events are final under any
// future input. Only complete top-level blocks and trivia before them count.
func streamSafeEventCount(document *Document, buffer string) int {
	end := streamSafeEnd(document, buffer)
	if end <= 0 {
		return 0
	}
	count := 0
	for _, event := range document.Events {
		if event.Kind == Enter {
			if event.Token.Range.Start.ByteOffset >= end {
				break
			}
		} else if event.Token.Range.End.ByteOffset > end {
			break
		}
		count++
	}
	return count
}

func streamSafeEnd(document *Document, buffer string) int {
	end := 0
	tokens := document.Tokens
	for index, token := range tokens {
		if token.node == nil {
			end = token.Range.End.ByteOffset
			continue
		}
		hasFollowing, blankAfter := streamNeighbours(tokens[index+1:])
		if !streamBlockSafe(token, hasFollowing, blankAfter, buffer) {
			break
		}
		end = token.Range.End.ByteOffset
	}
	return end
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
		return hasFollowing
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
