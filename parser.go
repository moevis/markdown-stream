package markdown

import (
	"errors"
	"io"
	"strings"
	"sync"
)

// EOF is distinct from every Unicode scalar and preprocessing sentinel.
const EOF = -1

// ErrClosed reports a write or close after input has been closed.
var ErrClosed = errors.New("markdown: input is closed")

// Parse confirms a complete document using the same grammar as Parser.
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

// Parser accepts bounded chunks and confirms its document only after Close.
// Read pulls confirmed events; it never blocks publication of Document.
// Writes and Close are serialized. Multiple readers share one event cursor.
type Parser struct {
	input    chan string
	done     chan struct{}
	options  Options
	writeMu  sync.Mutex
	readMu   sync.Mutex
	closed   bool
	inputErr error
	document *Document
	err      error
	cursor   int
}

// NewParser starts input collection. No separate Start or custom default root
// is needed: syntax is selected explicitly through Options.Extensions.
func NewParser(options ...Options) *Parser {
	configuration := Options{}
	if len(options) != 0 {
		configuration = options[0]
	}
	parser := &Parser{input: make(chan string, 16), done: make(chan struct{}), options: configuration}
	go parser.collect()
	return parser
}

func (p *Parser) collect() {
	var source strings.Builder
	for chunk := range p.input {
		source.WriteString(chunk)
	}
	if p.inputErr != nil {
		p.err = p.inputErr
	} else {
		p.document, p.err = Parse(source.String(), p.options)
	}
	close(p.done)
}

// Put enqueues a string without interpreting chunk boundaries as syntax.
// It blocks while the bounded input queue is full.
func (p *Parser) Put(chunk string) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if p.closed {
		return ErrClosed
	}
	if chunk != "" {
		p.input <- chunk
	}
	return nil
}

// Write implements io.Writer. Input bytes remain owned by the caller after the
// call, so the queued string retains its own immutable copy.
func (p *Parser) Write(chunk []byte) (int, error) {
	if err := p.Put(string(chunk)); err != nil {
		return 0, err
	}
	return len(chunk), nil
}

// Close marks the end of input. Document and Read wait for confirmation.
func (p *Parser) Close() error { return p.CloseWithError(nil) }

// CloseWithError aborts confirmation and propagates the producer's error.
func (p *Parser) CloseWithError(err error) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if p.closed {
		return ErrClosed
	}
	p.closed, p.inputErr = true, err
	close(p.input)
	return nil
}

// Document waits for a confirmed document without consuming its event cursor.
func (p *Parser) Document() (*Document, error) {
	<-p.done
	return p.document, p.err
}

// Read blocks until confirmation and returns the next event. A false boolean
// means exhaustion; an error means input or syntax confirmation failed.
func (p *Parser) Read() (Event, bool, error) {
	<-p.done
	p.readMu.Lock()
	defer p.readMu.Unlock()
	return p.readConfirmed()
}

func (p *Parser) readConfirmed() (Event, bool, error) {
	if p.err != nil {
		return Event{}, false, p.err
	}
	if p.cursor == len(p.document.Events) {
		return Event{}, false, nil
	}
	event := p.document.Events[p.cursor]
	p.cursor++
	return event, true, nil
}

// ReadNonBlocking distinguishes pending confirmation from exhausted events.
func (p *Parser) ReadNonBlocking() (Event, ReadStatus, error) {
	select {
	case <-p.done:
		p.readMu.Lock()
		defer p.readMu.Unlock()
		event, available, err := p.readConfirmed()
		if available {
			return event, ReadEvent, nil
		}
		return event, ReadDone, err
	default:
		return Event{}, ReadPending, nil
	}
}
