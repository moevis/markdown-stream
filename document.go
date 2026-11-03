package markdown

// Position identifies both the original byte offset and the preprocessed
// UTF-16 coordinate. Line and Column are one-based; offsets are zero-based.
type Position struct {
	ByteOffset int
	Offset     int
	Line       int
	Column     int
}

// SourceRange is an end-exclusive enclosure in the original input.
type SourceRange struct {
	Start Position
	End   Position
}

// TokenType remains open to syntax extensions.
type TokenType string

// EventKind describes a concrete token boundary.
type EventKind uint8

const (
	Enter EventKind = iota
	Exit
)

// Event boundaries retain the same token identity on enter and exit.
type Event struct {
	Kind    EventKind
	Token   *Token
	Context *TokenizeContext
}

// ContentType selects the grammar used for a token's logical content.
type ContentType uint8

const (
	ContentNone ContentType = iota
	ContentDocument
	ContentFlow
	ContentText
	ContentString
)

// Token retains concrete syntax independently of semantic AST nodes.
// Source is normalized logical content; RawSource includes physical margins.
type Token struct {
	Type        TokenType
	Range       SourceRange
	Children    []*Token
	ContentType ContentType
	Data        map[string]any

	view           sourceView
	node           *Node
	context        *TokenizeContext
	recordedMargin bool
}

func (t *Token) Source() string { return t.view.text }

func (t *Token) RawSource() string {
	if t.view.doc == nil {
		return ""
	}
	return t.view.doc.raw[t.Range.Start.ByteOffset:t.Range.End.ByteOffset]
}

// SourceRanges returns the original pieces making up logical content. Unlike
// Range, these pieces exclude margins omitted by a nested tokenizer.
func (t *Token) SourceRanges() []SourceRange { return t.view.ranges() }

func (t *Token) append(children ...*Token) {
	t.Children = append(t.Children, children...)
}

// NodeKind describes semantic structure rather than individual markers.
type NodeKind string

const (
	KindDocument           NodeKind = "document"
	KindParagraph          NodeKind = "paragraph"
	KindHeading            NodeKind = "heading"
	KindBlockQuote         NodeKind = "blockquote"
	KindList               NodeKind = "list"
	KindListItem           NodeKind = "listItem"
	KindCodeBlock          NodeKind = "code"
	KindHTMLBlock          NodeKind = "html"
	KindThematicBreak      NodeKind = "thematicBreak"
	KindDefinition         NodeKind = "definition"
	KindText               NodeKind = "text"
	KindEmphasis           NodeKind = "emphasis"
	KindStrong             NodeKind = "strong"
	KindCodeSpan           NodeKind = "inlineCode"
	KindLink               NodeKind = "link"
	KindImage              NodeKind = "image"
	KindHTMLInline         NodeKind = "inlineHTML"
	KindSoftBreak          NodeKind = "softBreak"
	KindHardBreak          NodeKind = "hardBreak"
	KindStrikethrough      NodeKind = "strikethrough"
	KindTable              NodeKind = "table"
	KindTableRow           NodeKind = "tableRow"
	KindTableCell          NodeKind = "tableCell"
	KindFootnoteDefinition NodeKind = "footnoteDefinition"
	KindFootnoteReference  NodeKind = "footnoteReference"
	KindMathBlock          NodeKind = "math"
	KindMathInline         NodeKind = "inlineMath"
	KindCustom             NodeKind = "custom"
)

// Alignment describes GFM table cell alignment.
type Alignment uint8

const (
	AlignNone Alignment = iota
	AlignLeft
	AlignCenter
	AlignRight
)

// Node is a semantic AST node. Literal is decoded/normalized content where
// applicable; Range and its concrete Token preserve original provenance.
type Node struct {
	Kind        NodeKind
	Range       SourceRange
	Children    []*Node
	Literal     string
	Destination string
	Title       string
	Identifier  string
	Label       string
	Info        string
	Meta        string
	Level       int
	Start       int
	Ordered     bool
	Tight       bool
	Task        bool
	Checked     bool
	Alignment   Alignment
	Alignments  []Alignment
	Data        map[string]any

	input  sourceView
	ending sourceView
	token  *Token
	parent *Node
}

func (n *Node) Parent() *Node { return n.parent }
func (n *Node) Token() *Token { return n.token }

// AppendChild moves a node from its previous parent when necessary.
func (n *Node) AppendChild(child *Node) {
	if child.parent != nil {
		child.parent.RemoveChild(child)
	}
	child.parent = n
	n.Children = append(n.Children, child)
}

func (n *Node) RemoveChild(child *Node) bool {
	for i, candidate := range n.Children {
		if candidate == child {
			copy(n.Children[i:], n.Children[i+1:])
			n.Children[len(n.Children)-1] = nil
			n.Children = n.Children[:len(n.Children)-1]
			child.parent = nil
			return true
		}
	}
	return false
}

// Walk visits nodes depth-first; false skips the visited node's descendants.
func (n *Node) Walk(visit func(*Node) bool) {
	if !visit(n) {
		return
	}
	for _, child := range n.Children {
		child.Walk(visit)
	}
}

// Definition retains the first recognized definition for a normalized label.
type Definition struct {
	Destination string
	Title       string
	Node        *Node
}

// Document contains original input, concrete syntax, and a genuine semantic AST.
// The parsed concrete representation is separate from subsequent AST edits.
type Document struct {
	Root        *Node
	Tokens      []*Token
	Events      []Event
	Definitions map[string]Definition

	source        *sourceDocument
	options       Options
	footnotes     map[string]*Node
	footnoteOrder []*Node
	footnoteCalls map[string]int
}

func (d *Document) Source() string           { return d.source.raw }
func (d *Document) NormalizedSource() string { return d.source.text }

func makeToken(kind TokenType, view sourceView) *Token {
	return &Token{Type: kind, Range: view.span(), view: view}
}

func makeNode(kind NodeKind, tokenType TokenType, view sourceView) *Node {
	n := &Node{Kind: kind, Range: view.span(), input: view}
	if tokenType != "" {
		n.token = makeToken(tokenType, view)
		n.token.node = n
	}
	return n
}

func appendNode(parent, child *Node) {
	parent.AppendChild(child)
	if parent.token != nil && child.token != nil {
		parent.token.append(child.token)
	}
}

func appendEvents(tokens []*Token, output []Event) []Event {
	for _, token := range tokens {
		output = append(output, Event{Kind: Enter, Token: token, Context: token.context})
		output = appendEvents(token.Children, output)
		output = append(output, Event{Kind: Exit, Token: token, Context: token.context})
	}
	return output
}

// ReadStatus distinguishes pending finalization from exhausted events.
type ReadStatus uint8

const (
	ReadPending ReadStatus = iota
	ReadEvent
	ReadDone
)
