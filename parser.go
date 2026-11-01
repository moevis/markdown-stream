package markdown

import "unicode/utf8"

// EOF marks the end of closed input. U+0000 remains an ordinary input rune.
const EOF rune = -1

// Parser is a single-use streaming parser. Run Start, one input producer, and
// one output consumer concurrently; cursor methods belong to the parsing goroutine.
type Parser struct {
	in  chan string
	out chan MarkdownSegment

	content    []rune
	index      int
	previous   rune
	pending    [utf8.UTFMax]byte
	pendingLen int
}

// NewParser creates a parser with bounded input and output buffers.
func NewParser() *Parser {
	return &Parser{
		in:       make(chan string, 1024),
		out:      make(chan MarkdownSegment, 64),
		content:  make([]rune, 0, 1024),
		previous: EOF,
	}
}

type parserState struct {
	index    int
	previous rune
}

// PushState saves an opaque cursor position for rollback within the current parse.
func (p *Parser) PushState() parserState {
	return parserState{index: p.index, previous: p.previous}
}

// ResumeState restores a cursor position saved by PushState.
func (p *Parser) ResumeState(state parserState) {
	p.index = state.index
	p.previous = state.previous
}

// Start processes root and closes the output when root ends. Call it only once.
// A root other than MarkdownDoc may finish before the input is closed.
func (p *Parser) Start(root MarkdownElem) {
	root.Process(p)
	p.content = nil
	p.index = 0
	p.previous = EOF
	p.pendingLen = 0
	close(p.out)
}

// Next consumes the next decoded rune, waiting for input if needed.
// It returns EOF only when closed input is exhausted; U+0000 is an ordinary rune.
func (p *Parser) Next() rune {
	if p.index >= len(p.content) && p.Peek() == EOF {
		return EOF
	}
	r := p.content[p.index]
	p.index++
	p.previous = r
	return r
}

// Peek returns the next decoded rune without advancing the cursor.
// It waits for a complete rune or closed input and returns EOF at the end.
func (p *Parser) Peek() rune {
	for p.index >= len(p.content) {
		delta, ok := <-p.in
		if !ok {
			// An incomplete UTF-8 sequence decodes to one RuneError per byte at EOF.
			for range p.pendingLen {
				p.content = append(p.content, utf8.RuneError)
			}
			p.pendingLen = 0
			if p.index >= len(p.content) {
				return EOF
			}
			break
		}
		p.appendContent(delta)
	}
	return p.content[p.index]
}

func (p *Parser) appendContent(delta string) {
	for p.pendingLen > 0 {
		if !utf8.FullRune(p.pending[:p.pendingLen]) {
			if len(delta) == 0 {
				return
			}
			p.pending[p.pendingLen] = delta[0]
			p.pendingLen++
			delta = delta[1:]
			continue
		}
		r, size := utf8.DecodeRune(p.pending[:p.pendingLen])
		p.content = append(p.content, r)
		p.pendingLen -= size
		if p.pendingLen > 0 {
			copy(p.pending[:], p.pending[size:size+p.pendingLen])
		}
	}
	for len(delta) > 0 {
		r, size := utf8.DecodeRuneInString(delta)
		if r == utf8.RuneError && size == 1 && !utf8.FullRuneInString(delta) {
			p.pendingLen = copy(p.pending[:], delta)
			return
		}
		p.content = append(p.content, r)
		delta = delta[size:]
	}
}

// OutputRune sends a segment during grammar processing and may block on the consumer.
func (p *Parser) OutputRune(r rune, elemType ElemType) {
	p.out <- MarkdownSegment{Type: elemType, Rune: r}
}

// Read waits for the next segment. It returns ok=false after the output is exhausted.
func (p *Parser) Read() (MarkdownSegment, bool) {
	seg, ok := <-p.out
	return seg, ok
}

// ReadNonBlocking returns a segment if available, or (nil, true) while pending.
// Once output is exhausted, it returns ok=false; callers must ignore the segment.
func (p *Parser) ReadNonBlocking() (*MarkdownSegment, bool) {
	select {
	case seg, ok := <-p.out:
		return &seg, ok
	default:
		return nil, true
	}
}

// MarkdownSegment labels one decoded input rune, including markup and escapes.
type MarkdownSegment struct {
	Type ElemType
	Rune rune
}

type markdownCtx interface {
	OutputRune(r rune, elemType ElemType)
	PushState() parserState
	ResumeState(state parserState)
	Next() rune
	Peek() rune
}

// Put appends an input block and blocks when the input buffer is full.
// One producer must call Put in order and must not call it after Close.
func (p *Parser) Put(delta string) {
	p.in <- delta
}

// Close marks the end of input. Call it once after the last Put.
// It does not cancel Start or wait for the output to be consumed.
func (p *Parser) Close() {
	close(p.in)
}
