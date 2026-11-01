package markdown

type Parser struct {
	in  chan string
	out chan MarkdownSegment

	content []rune
	index   int
}

func NewParser() *Parser {
	return &Parser{
		in:      make(chan string, 1024),
		out:     make(chan MarkdownSegment, 64),
		content: make([]rune, 0, 1024),
	}
}

type parserState struct {
	index int
}

func (p *Parser) PushState() parserState {
	return parserState{index: p.index}
}

func (p *Parser) ResumeState(state parserState) {
	p.index = state.index
}

func (p *Parser) Start(root MarkdownElem) {
	root.Process(p)
	close(p.out)
}

func (p *Parser) Next() rune {
	if p.index >= len(p.content) {
		if p.Peek() == 0 {
			// 已经没有内容了
			return 0
		}
		// 在执行过 p.Peek() 之后，p.content 已经被填充了新的内容
	}
	r := p.content[p.index]
	p.index++
	return r
}

func (p *Parser) Peek() rune {
	if p.index >= len(p.content) {
		for {
			newContent, ok := <-p.in
			if !ok {
				// 已关闭
				return 0
			}
			if len(newContent) == 0 {
				continue
			}
			p.content = append(p.content, []rune(newContent)...)
			break
		}
	}
	return p.content[p.index]
}

func (p *Parser) OutputRune(r rune, elemType ElemType) {
	p.out <- MarkdownSegment{Type: elemType, Rune: r}
}

func (p *Parser) Read() (MarkdownSegment, bool) {
	seg, ok := <-p.out
	return seg, ok
}

func (p *Parser) ReadNonBlocking() (*MarkdownSegment, bool) {
	select {
	case seg, ok := <-p.out:
		return &seg, ok
	default:
		return nil, true
	}
}

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

func (p *Parser) Put(delta string) {
	p.in <- delta
}

func (p *Parser) Close() {
	close(p.in)
}
