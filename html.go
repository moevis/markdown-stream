package markdown

import (
	"errors"
	"io"
	"strconv"
	"strings"
)

// CompileContext is the state shared by HTML handlers. Raw writes content;
// Tag writes markup, which is omitted while an image's alternative text is
// captured. Errors are sticky, including errors that a handler fails to return.
// Buffer and Resume nest without changing the destination writer.
type CompileContext struct {
	Options             Options
	Data                map[string]any
	writer              io.Writer
	document            *Document
	token               *Token
	buffers             []*compileBuffer
	frames              []compileFrame
	enter               map[TokenType]HTMLHandler
	exit                map[TokenType]HTMLHandler
	err                 error
	lineEnding          string
	pendingEnding       string
	last                byte
	wrote               bool
	lastWasTag          bool
	imageDepth          int
	hidden              int
	tight               []bool
	gfm                 bool
	filterTags          bool
	ast                 bool
	tables              []*compileTable
	footnoteDefinitions map[string]string
	footnoteOrder       []string
	footnoteCounts      map[string]int
	footnoteConfig      GFMHTMLOptions
}

type compileBuffer struct {
	text          strings.Builder
	last          byte
	wrote         bool
	lastWasTag    bool
	pendingEnding string
}

type compileFrame struct {
	token          *Token
	atomic         bool
	hidden         bool
	paragraphTight bool
	endsInline     bool
	capture        bool
	captureDepth   int
	image          bool
	footnote       bool
	tight          bool
}

type compileTable struct {
	alignments  []Alignment
	rows        int
	column      int
	header      bool
	section     string
	ignoredCell bool
}

// Buffer starts a nested capture. Resume removes only the innermost capture.
func (c *CompileContext) Buffer() {
	index := len(c.buffers)
	var buffer *compileBuffer
	if index < cap(c.buffers) {
		buffer = c.buffers[:index+1][index]
	}
	if buffer == nil {
		buffer = &compileBuffer{}
	}
	buffer.text.Reset()
	buffer.last, buffer.wrote, buffer.lastWasTag, buffer.pendingEnding = c.last, c.wrote, c.lastWasTag, c.pendingEnding
	c.buffers = append(c.buffers, buffer)
	c.last, c.wrote, c.lastWasTag, c.pendingEnding = 0, false, false, ""
	c.Data["lastWasTag"] = false
}

func (c *CompileContext) Resume() (string, error) {
	if len(c.buffers) == 0 {
		return "", c.fail(errors.New("markdown: Resume without Buffer"))
	}
	if err := c.flushEnding(); err != nil {
		return "", err
	}
	index := len(c.buffers) - 1
	buffer := c.buffers[index]
	value := buffer.text.String()
	c.last, c.wrote, c.lastWasTag, c.pendingEnding = buffer.last, buffer.wrote, buffer.lastWasTag, buffer.pendingEnding
	c.Data["lastWasTag"] = c.lastWasTag
	c.buffers = c.buffers[:index]
	return value, c.err
}

func (c *CompileContext) fail(err error) error {
	if c.err == nil {
		c.err = err
	}
	return c.err
}

func (c *CompileContext) write(value string, tag bool) error {
	if c.err != nil {
		return c.err
	}
	if value == "" {
		return nil
	}
	if len(c.buffers) != 0 {
		_, c.err = c.buffers[len(c.buffers)-1].text.WriteString(value)
	} else {
		var count int
		count, c.err = io.WriteString(c.writer, value)
		if c.err == nil && count != len(value) {
			c.err = io.ErrShortWrite
		}
	}
	if c.err != nil {
		return c.err
	}
	c.last, c.wrote, c.lastWasTag = value[len(value)-1], true, tag
	c.Data["lastWasTag"] = tag
	return nil
}

func (c *CompileContext) Raw(value string) error { return c.write(value, false) }
func (c *CompileContext) Tag(value string) error {
	if c.imageDepth != 0 {
		return c.err
	}
	return c.write(value, true)
}

// Encode uses the four entities used by CommonMark's HTML compiler. Apostrophes
// are not escaped because generated attributes always use double quotes.
func (c *CompileContext) Encode(value string) string {
	if ignore, _ := c.Data["ignoreEncode"].(bool); ignore {
		return value
	}
	return htmlEncode(value)
}

func htmlEncode(value string) string {
	start := strings.IndexAny(value, "&<>\"")
	if start < 0 {
		return value
	}
	var result strings.Builder
	result.Grow(len(value) + 16)
	result.WriteString(value[:start])
	for i := start; i < len(value); i++ {
		switch value[i] {
		case '&':
			result.WriteString("&amp;")
		case '<':
			result.WriteString("&lt;")
		case '>':
			result.WriteString("&gt;")
		case '"':
			result.WriteString("&quot;")
		default:
			result.WriteByte(value[i])
		}
	}
	return result.String()
}

func (c *CompileContext) Slice(token *Token) string {
	if token == nil {
		return ""
	}
	return token.Source()
}
func (c *CompileContext) Document() *Document { return c.document }
func (c *CompileContext) Token() *Token       { return c.token }
func (c *CompileContext) Source() string {
	if c.document == nil || c.document.source == nil {
		return ""
	}
	return c.document.Source()
}
func (c *CompileContext) Position() Position {
	if c.token == nil {
		return Position{Line: 1, Column: 1}
	}
	return c.token.Range.Start
}
func (c *CompileContext) Range(token *Token) SourceRange {
	if token == nil {
		return SourceRange{}
	}
	return token.Range
}
func (c *CompileContext) SourcePosition(token *Token, index int) Position {
	if token == nil {
		return Position{Line: 1, Column: 1}
	}
	if index < 0 {
		index = 0
	}
	if index > len(token.view.text) {
		index = len(token.view.text)
	}
	return token.view.position(index, index == len(token.view.text))
}
func (c *CompileContext) LineEnding() string            { return c.lineEnding }
func (c *CompileContext) SlurpOneLineEnding()           { c.Data["slurpOneLineEnding"] = true }
func (c *CompileContext) GetData(key string) any        { return c.Data[key] }
func (c *CompileContext) SetData(key string, value any) { c.Data[key] = value }

// LineEndingIfNeeded writes a generated ending, preferring a pending physical
// ending. DefaultLineEnding never rewrites endings belonging to source tokens.
func (c *CompileContext) LineEndingIfNeeded() error {
	if err := c.flushEnding(); err != nil {
		return err
	}
	if !c.wrote || c.last == '\n' || c.last == '\r' {
		return c.err
	}
	return c.Raw(c.lineEnding)
}
func (c *CompileContext) flushEnding() error {
	ending := c.pendingEnding
	c.pendingEnding = ""
	if ending == "" {
		return c.err
	}
	if slurp, _ := c.Data["slurpOneLineEnding"].(bool); slurp {
		delete(c.Data, "slurpOneLineEnding")
		return c.err
	}
	if all, _ := c.Data["slurpAllLineEndings"].(bool); all {
		return c.err
	}
	return c.Raw(ending)
}
func (c *CompileContext) sourceEnding(value string) error {
	if all, _ := c.Data["slurpAllLineEndings"].(bool); all {
		return c.err
	}
	if one, _ := c.Data["slurpOneLineEnding"].(bool); one {
		delete(c.Data, "slurpOneLineEnding")
		return c.err
	}
	return c.Raw(value)
}

// RenderHTML compiles the document's recorded concrete event stream. It does
// not consult the AST child lists, which may have been edited after parsing.
func RenderHTML(writer io.Writer, document *Document, options ...Options) error {
	if writer == nil {
		return errors.New("markdown: nil HTML writer")
	}
	if document == nil {
		return errors.New("markdown: nil document")
	}
	settings := document.options
	if len(options) != 0 {
		settings = options[0]
	}
	context := newCompileContext(writer, document, settings, false)
	if err := context.begin(); err != nil {
		return err
	}
	for _, event := range document.Events {
		if err := context.event(event.Kind, event.Token); err != nil {
			return err
		}
	}
	return context.finish()
}

func HTML(source string, options ...Options) (string, error) {
	document, err := Parse(source, options...)
	if err != nil {
		return "", err
	}
	var result strings.Builder
	if err = RenderHTML(&result, document); err != nil {
		return "", err
	}
	return result.String(), nil
}

// RenderAST renders the current semantic tree, including added, removed,
// reordered, or modified nodes. Concrete syntax children are never traversed.
func RenderAST(writer io.Writer, root *Node, options ...Options) error {
	if writer == nil {
		return errors.New("markdown: nil HTML writer")
	}
	if root == nil {
		return errors.New("markdown: nil AST root")
	}
	var settings Options
	if len(options) != 0 {
		settings = options[0]
	}
	context := newCompileContext(writer, nil, settings, true)
	if root.input.doc != nil && settings.DefaultLineEnding == "" {
		context.lineEnding = firstLineEnding(root.input.doc.text)
	}
	if err := context.begin(); err != nil {
		return err
	}
	if err := context.renderNode(root); err != nil {
		return err
	}
	return context.finish()
}

func firstLineEnding(source string) string {
	for i := range len(source) {
		if source[i] == '\n' {
			return "\n"
		}
		if source[i] == '\r' {
			if i+1 < len(source) && source[i+1] == '\n' {
				return "\r\n"
			}
			return "\r"
		}
	}
	return "\n"
}

func newCompileContext(writer io.Writer, document *Document, settings Options, ast bool) *CompileContext {
	c := &CompileContext{Options: settings, Data: make(map[string]any), writer: writer, document: document, ast: ast,
		enter: make(map[TokenType]HTMLHandler), exit: make(map[TokenType]HTMLHandler),
		footnoteDefinitions: make(map[string]string), footnoteCounts: make(map[string]int)}
	c.lineEnding = settings.DefaultLineEnding
	if c.lineEnding == "" {
		if document != nil && document.source != nil {
			c.lineEnding = firstLineEnding(document.source.text)
		} else {
			c.lineEnding = "\n"
		}
	}
	for _, kind := range []TokenType{"document", "paragraph", "atxHeading", "setextHeading", "heading", "blockQuote", "listOrdered", "listUnordered", "listItem", "codeFenced", "codeIndented", "codeText", "htmlFlow", "htmlText", "thematicBreak", "definition", "data", "characterEscape", "characterReference", "emphasis", "strong", "link", "image", "autolink", "autolinkEmail", "autolinkProtocol", "lineEnding", "softBreak", "hardBreakEscape", "hardBreakTrailing"} {
		c.enter[kind] = compileCoreEnter
		c.exit[kind] = compileCoreExit
	}
	for _, kind := range []TokenType{"characterEscapeValue", "codeTextData", "codeFlowValue", "htmlFlowData", "htmlTextData"} {
		c.exit[kind] = compileLeaf
	}
	for _, kind := range []TokenType{"resource", "reference"} {
		c.enter[kind] = func(c *CompileContext, t *Token) error { c.atomic(); return nil }
	}
	for _, extension := range settings.HTMLExtensions {
		c.gfm = c.gfm || extension.gfm
		c.filterTags = c.filterTags || extension.filterTags
		if extension.EnterDocument != nil {
			handler := extension.EnterDocument
			c.enter[""] = func(c *CompileContext, _ *Token) error { return handler(c) }
		}
		if extension.ExitDocument != nil {
			handler := extension.ExitDocument
			c.exit[""] = func(c *CompileContext, _ *Token) error { return handler(c) }
		}
		for kind, handler := range extension.Enter {
			c.enter[kind] = handler
		}
		for kind, handler := range extension.Exit {
			c.exit[kind] = handler
		}
	}
	if document != nil {
		c.Data["definitions"] = document.Definitions
	}
	return c
}

func (c *CompileContext) begin() error {
	if handler, ok := c.enter[""]; ok && handler != nil {
		if err := handler(c, nil); err != nil {
			return c.fail(err)
		}
	}
	return c.err
}
func (c *CompileContext) finish() error {
	if err := c.flushEnding(); err != nil {
		return err
	}
	if len(c.frames) != 0 {
		return c.fail(errors.New("markdown: unclosed HTML token events"))
	}
	if handler, ok := c.exit[""]; ok && handler != nil {
		if err := handler(c, nil); err != nil {
			return c.fail(err)
		}
	}
	if len(c.buffers) != 0 {
		return c.fail(errors.New("markdown: unclosed HTML capture buffer"))
	}
	return c.err
}

func (c *CompileContext) event(kind EventKind, token *Token) error {
	if token == nil {
		return nil
	}
	c.token = token
	if kind == Enter {
		frame := compileFrame{token: token, hidden: c.hidden != 0}
		c.frames = append(c.frames, frame)
		if frame.hidden {
			return c.err
		}
		if handler := c.enter[token.Type]; handler != nil {
			if err := handler(c, token); err != nil {
				return c.fail(err)
			}
		}
	} else {
		if len(c.frames) == 0 || c.frames[len(c.frames)-1].token != token {
			return c.fail(errors.New("markdown: unbalanced HTML token events"))
		}
		frame := c.frames[len(c.frames)-1]
		if frame.atomic {
			c.hidden--
		}
		if !frame.hidden {
			if handler := c.exit[token.Type]; handler != nil {
				if err := handler(c, token); err != nil {
					return c.fail(err)
				}
			}
		}
		current := &c.frames[len(c.frames)-1]
		if current.capture {
			if len(c.buffers) >= current.captureDepth {
				if _, err := c.Resume(); err != nil {
					return err
				}
			}
			if current.image {
				c.imageDepth--
			}
			if current.footnote && len(c.tight) != 0 {
				c.tight = c.tight[:len(c.tight)-1]
				c.Data["tightStack"] = c.tight
			}
		}
		if current.tight && len(c.tight) != 0 {
			c.tight = c.tight[:len(c.tight)-1]
			c.Data["tightStack"] = c.tight
		}
		c.frames = c.frames[:len(c.frames)-1]
	}
	return c.err
}
func (c *CompileContext) atomic() {
	frame := &c.frames[len(c.frames)-1]
	if !frame.atomic {
		frame.atomic = true
		c.hidden++
	}
}
func (c *CompileContext) isTight() bool {
	if stack, ok := c.Data["tightStack"].([]bool); ok {
		return len(stack) != 0 && stack[len(stack)-1]
	}
	return len(c.tight) != 0 && c.tight[len(c.tight)-1]
}
func (c *CompileContext) queueEnding(node *Node) {
	if node != nil && node.ending.text != "" {
		c.pendingEnding = node.ending.text
	}
}

func semanticKind(token *Token) NodeKind {
	if token.node != nil {
		return token.node.Kind
	}
	switch token.Type {
	case "paragraph":
		return KindParagraph
	case "atxHeading", "setextHeading", "heading":
		return KindHeading
	case "blockQuote":
		return KindBlockQuote
	case "listOrdered", "listUnordered":
		return KindList
	case "listItem":
		return KindListItem
	case "codeFenced", "codeIndented":
		return KindCodeBlock
	case "codeText":
		return KindCodeSpan
	case "htmlFlow":
		return KindHTMLBlock
	case "htmlText":
		return KindHTMLInline
	case "thematicBreak":
		return KindThematicBreak
	case "definition":
		return KindDefinition
	case "data", "characterEscape", "characterReference":
		return KindText
	case "emphasis":
		return KindEmphasis
	case "strong":
		return KindStrong
	case "link", "autolinkEmail", "autolinkProtocol":
		return KindLink
	case "image":
		return KindImage
	case "lineEnding", "softBreak":
		return KindSoftBreak
	case "hardBreakEscape", "hardBreakTrailing":
		return KindHardBreak
	case "strikethrough":
		return KindStrikethrough
	case "table":
		return KindTable
	case "tableRow":
		return KindTableRow
	case "tableCell", "tableHeader", "tableData":
		return KindTableCell
	case "footnoteDefinition", "gfmFootnoteDefinition":
		return KindFootnoteDefinition
	case "gfmFootnoteCall", "gfmFootnoteCallString":
		return KindFootnoteReference
	case "mathFlow":
		return KindMathBlock
	case "mathText":
		return KindMathInline
	}
	return KindCustom
}

func tokenNode(token *Token) *Node {
	if token.node != nil {
		return token.node
	}
	return &Node{Kind: semanticKind(token), Literal: token.Source(), Level: 1, Start: 1, Ordered: token.Type == "listOrdered"}
}

func (c *CompileContext) listItemEnding(inline bool) {
	for i := len(c.frames) - 2; i >= 0; i-- {
		if semanticKind(c.frames[i].token) == KindListItem {
			c.frames[i].endsInline = inline
			return
		}
	}
}

func compileCoreEnter(c *CompileContext, token *Token) error {
	n := tokenNode(token)
	if n.Kind == KindParagraph || n.Kind == KindHeading || n.Kind == KindBlockQuote || n.Kind == KindList || n.Kind == KindCodeBlock || n.Kind == KindHTMLBlock || n.Kind == KindThematicBreak {
		c.listItemEnding(n.Kind == KindParagraph && c.isTight())
	}
	switch n.Kind {
	case KindParagraph:
		tight := c.isTight()
		c.frames[len(c.frames)-1].paragraphTight = tight
		delete(c.Data, "slurpAllLineEndings")
		if !tight {
			if err := c.LineEndingIfNeeded(); err != nil {
				return err
			}
			if err := c.Tag("<p>"); err != nil {
				return err
			}
		}
		if tight {
			if err := c.flushEnding(); err != nil {
				return err
			}
		}
		if c.ast && c.gfm {
			task, checked := n.Task, n.Checked
			for i := len(c.frames) - 2; i >= 0; i-- {
				if item := c.frames[i].token.node; item != nil && item.Kind == KindListItem {
					task = item.Task && len(item.Children) != 0 && item.Children[0] == n
					checked = item.Checked
					break
				}
			}
			if task {
				return c.checkbox(checked)
			}
		}
	case KindHeading:
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		return c.Tag("<h" + strconv.Itoa(n.Level) + ">")
	case KindBlockQuote:
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		c.tight = append(c.tight, false)
		c.Data["tightStack"] = c.tight
		c.frames[len(c.frames)-1].tight = true
		return c.Tag("<blockquote>")
	case KindList:
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		c.tight = append(c.tight, n.Tight)
		c.Data["tightStack"] = c.tight
		c.frames[len(c.frames)-1].tight = true
		if n.Ordered {
			if n.Start != 1 {
				return c.Tag("<ol start=\"" + strconv.Itoa(n.Start) + "\">")
			}
			return c.Tag("<ol>")
		}
		return c.Tag("<ul>")
	case KindListItem:
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		if err := c.Tag("<li>"); err != nil {
			return err
		}
		c.lastWasTag = false
		c.Data["lastWasTag"] = false
	case KindCodeBlock, KindHTMLBlock, KindThematicBreak:
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		c.atomic()
	case KindDefinition:
		if err := c.flushEnding(); err != nil {
			return err
		}
		c.atomic()
	case KindCodeSpan, KindHTMLInline, KindText, KindSoftBreak, KindHardBreak:
		c.atomic()
	case KindEmphasis:
		return c.Tag("<em>")
	case KindStrong:
		return c.Tag("<strong>")
	case KindLink:
		if token.Type == "autolinkEmail" || token.Type == "autolinkProtocol" {
			c.atomic()
			return nil
		}
		return c.Tag(c.linkOpening(n))
	case KindImage:
		c.Buffer()
		c.imageDepth++
		frame := &c.frames[len(c.frames)-1]
		frame.capture = true
		frame.captureDepth = len(c.buffers)
		frame.image = true
	}
	return c.err
}

func compileCoreExit(c *CompileContext, token *Token) error {
	n := tokenNode(token)
	switch n.Kind {
	case KindParagraph:
		if !c.frames[len(c.frames)-1].paragraphTight {
			if err := c.Tag("</p>"); err != nil {
				return err
			}
			c.queueEnding(n)
		}
	case KindHeading:
		if err := c.Tag("</h" + strconv.Itoa(n.Level) + ">"); err != nil {
			return err
		}
		c.queueEnding(n)
	case KindBlockQuote:
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		if err := c.Tag("</blockquote>"); err != nil {
			return err
		}
		if c.frames[len(c.frames)-1].tight {
			c.tight = c.tight[:len(c.tight)-1]
			c.frames[len(c.frames)-1].tight = false
			c.Data["tightStack"] = c.tight
		}
		c.queueEnding(n)
	case KindList:
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		tag := "</ul>"
		if n.Ordered {
			tag = "</ol>"
		}
		if err := c.Tag(tag); err != nil {
			return err
		}
		if c.frames[len(c.frames)-1].tight {
			c.tight = c.tight[:len(c.tight)-1]
			c.frames[len(c.frames)-1].tight = false
			c.Data["tightStack"] = c.tight
		}
		c.queueEnding(n)
	case KindListItem:
		if !c.frames[len(c.frames)-1].endsInline && (c.lastWasTag || c.pendingEnding != "") {
			if err := c.LineEndingIfNeeded(); err != nil {
				return err
			}
		}
		if err := c.Tag("</li>"); err != nil {
			return err
		}
		c.queueEnding(n)
	case KindCodeBlock:
		opening := "<pre><code"
		language := n.Info
		if word, ok := n.Data["language"].(string); ok && !c.ast {
			language = word
		} else if end := strings.IndexAny(language, " \t\r\n"); end >= 0 {
			language = language[:end]
		}
		if language != "" {
			opening += " class=\"language-" + c.Encode(language) + "\""
		}
		if err := c.Tag(opening + ">"); err != nil {
			return err
		}
		if err := c.Raw(c.Encode(n.Literal)); err != nil {
			return err
		}
		if n.Literal != "" {
			if err := c.LineEndingIfNeeded(); err != nil {
				return err
			}
		}
		if err := c.Tag("</code></pre>"); err != nil {
			return err
		}
		c.queueEnding(n)
		if closed, ok := n.Data["closed"].(bool); ok && !closed && token.Type == "codeFenced" && c.pendingEnding == "" {
			c.pendingEnding = c.lineEnding
		}
	case KindCodeSpan:
		if err := c.Tag("<code>"); err != nil {
			return err
		}
		if err := c.Raw(c.Encode(n.Literal)); err != nil {
			return err
		}
		return c.Tag("</code>")
	case KindHTMLBlock, KindHTMLInline:
		value := n.Literal
		if c.Options.AllowDangerousHTML {
			if c.filterTags {
				value = filterHTML(value, n.Kind == KindHTMLBlock)
			}
		} else {
			value = htmlEncode(value)
		}
		if err := c.Raw(value); err != nil {
			return err
		}
		if n.Kind == KindHTMLBlock && !strings.HasSuffix(value, n.ending.text) {
			c.queueEnding(n)
		}
	case KindThematicBreak:
		if err := c.Tag("<hr />"); err != nil {
			return err
		}
		c.queueEnding(n)
	case KindDefinition:
		c.pendingEnding = ""
	case KindText:
		value := n.Literal
		if token.node == nil {
			value = token.Source()
			if token.Type == "characterEscape" || token.Type == "characterReference" {
				value = decodeMarkdown(value)
			}
		}
		return c.Raw(c.Encode(value))
	case KindSoftBreak:
		value := n.Literal
		if value == "" {
			value = token.Source()
		}
		if !c.ast && (token.context == nil || token.context.Phase < PhaseText) {
			if len(c.frames) > 1 {
				parent := c.frames[len(c.frames)-2]
				kind := semanticKind(parent.token)
				if parent.endsInline || kind == KindParagraph || kind == KindHeading || kind == KindListItem {
					return nil
				}
			}
			c.pendingEnding = ""
		}
		return c.sourceEnding(value)
	case KindHardBreak:
		if err := c.Tag("<br />"); err != nil {
			return err
		}
		if n.Literal != "" {
			return c.sourceEnding(n.Literal)
		}
	case KindEmphasis:
		return c.Tag("</em>")
	case KindStrong:
		return c.Tag("</strong>")
	case KindLink:
		if token.Type == "autolinkEmail" || token.Type == "autolinkProtocol" {
			if token.node == nil {
				n.Literal = token.Source()
				n.Destination = n.Literal
				if token.Type == "autolinkEmail" {
					n.Destination = "mailto:" + n.Literal
				}
			}
			if err := c.Tag(c.linkOpening(n)); err != nil {
				return err
			}
			if err := c.Raw(c.Encode(n.Literal)); err != nil {
				return err
			}
		}
		return c.Tag("</a>")
	case KindImage:
		frame := &c.frames[len(c.frames)-1]
		if !frame.capture {
			return nil
		}
		alt, err := c.Resume()
		if err != nil {
			return err
		}
		c.imageDepth--
		frame.capture = false
		if err = c.Tag("<img src=\"" + c.uri(n.Destination, true) + "\" alt=\""); err != nil {
			return err
		}
		if err = c.Raw(alt); err != nil {
			return err
		}
		if err = c.Tag("\""); err != nil {
			return err
		}
		if n.Title != "" {
			if err = c.Tag(" title=\"" + c.Encode(n.Title) + "\""); err != nil {
				return err
			}
		}
		return c.Tag(" />")
	}
	return c.err
}

func compileLeaf(c *CompileContext, token *Token) error {
	value := token.Source()
	if token.Type == "htmlFlowData" || token.Type == "htmlTextData" {
		if c.Options.AllowDangerousHTML {
			if c.filterTags {
				value = filterHTML(value, token.Type == "htmlFlowData")
			}
			return c.Raw(value)
		}
	}
	return c.Raw(c.Encode(value))
}
func (c *CompileContext) linkOpening(n *Node) string {
	result := "<a href=\"" + c.uri(n.Destination, false) + "\""
	if n.Title != "" {
		result += " title=\"" + c.Encode(n.Title) + "\""
	}
	return result + ">"
}
func (c *CompileContext) uri(value string, image bool) string {
	value = normalizeURI(value)
	if !c.Options.AllowDangerousProtocol {
		colon := strings.IndexByte(value, ':')
		boundary := strings.IndexAny(value, "/?#")
		if colon >= 0 && (boundary < 0 || colon < boundary) {
			protocol := strings.ToLower(value[:colon])
			allowed := protocol == "http" || protocol == "https"
			if !image {
				allowed = allowed || protocol == "irc" || protocol == "ircs" || protocol == "mailto" || protocol == "xmpp"
			}
			if !allowed {
				return ""
			}
		}
	}
	return htmlEncode(value)
}

// GFMHTMLOptions configures GFM footnote HTML. Nil attribute and prefix pointers
// select GitHub's defaults; pointers to empty strings explicitly disable them.
// BackLabelText takes precedence over BackLabel when it is not empty.
type GFMHTMLOptions struct {
	Label           string
	LabelTagName    string
	LabelAttributes *string
	ClobberPrefix   *string
	BackLabelText   string
	BackLabel       func(referenceIndex, rereferenceIndex int) string
}

// GFMHTML registers tables, strike, task lists, literal links, footnotes, and
// the GFM forbidden-tag filter independently from GFM syntax recognition.
func GFMHTML(options ...GFMHTMLOptions) HTMLExtension {
	var config GFMHTMLOptions
	if len(options) != 0 {
		config = options[0]
	}
	extension := HTMLExtension{gfm: true, filterTags: true, Enter: make(map[TokenType]HTMLHandler), Exit: make(map[TokenType]HTMLHandler)}
	for _, kind := range []TokenType{"table", "tableRow", "tableCell", "tableHeader", "tableData", "strikethrough", "taskListCheck", "literalAutolink", "literalAutolinkEmail", "literalAutolinkHttp", "literalAutolinkWww", "gfmFootnoteCall", "footnoteDefinition", "gfmFootnoteDefinition", "footnoteDefinitionPrefix"} {
		extension.Enter[kind] = compileGFMEnter
		extension.Exit[kind] = compileGFMExit
	}
	extension.Enter["gfmFootnoteCall"] = func(c *CompileContext, t *Token) error { c.footnoteConfig = config; return compileGFMEnter(c, t) }
	extension.Exit["taskListCheckValue"] = func(c *CompileContext, t *Token) error {
		if t.Source() == "x" || t.Source() == "X" {
			return c.Tag("checked=\"\" ")
		}
		return nil
	}
	extension.Exit["taskListCheckValueChecked"] = func(c *CompileContext, t *Token) error { return c.Tag("checked=\"\" ") }
	for _, kind := range []TokenType{"htmlFlowData", "htmlTextData"} {
		extension.Exit[kind] = compileLeaf
	}
	extension.ExitDocument = func(c *CompileContext) error { c.footnoteConfig = config; return c.renderFootnotes() }
	return extension
}

func compileGFMEnter(c *CompileContext, t *Token) error {
	n := tokenNode(t)
	switch t.Type {
	case "taskListCheck":
		return c.Tag("<input type=\"checkbox\" disabled=\"\" ")
	case "footnoteDefinitionPrefix":
		c.atomic()
		return nil
	case "literalAutolinkEmail", "literalAutolinkHttp", "literalAutolinkWww":
		c.atomic()
		return nil
	case "literalAutolink":
		return compileCoreEnter(c, t)
	}
	switch n.Kind {
	case KindStrikethrough:
		return c.Tag("<del>")
	case KindTable:
		c.listItemEnding(false)
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		c.tables = append(c.tables, &compileTable{alignments: n.Alignments})
		return c.Tag("<table>")
	case KindTableRow:
		if len(c.tables) == 0 {
			return nil
		}
		table := c.tables[len(c.tables)-1]
		table.header = table.rows == 0
		if header, ok := n.Data["header"].(bool); ok {
			table.header = header
		}
		section := "tbody"
		if table.header {
			section = "thead"
		}
		if table.section != section {
			if table.section != "" {
				if err := c.LineEndingIfNeeded(); err != nil {
					return err
				}
				if err := c.Tag("</" + table.section + ">"); err != nil {
					return err
				}
			}
			if err := c.LineEndingIfNeeded(); err != nil {
				return err
			}
			if err := c.Tag("<" + section + ">"); err != nil {
				return err
			}
			table.section = section
		}
		table.column = 0
		table.rows++
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		return c.Tag("<tr>")
	case KindTableCell:
		if len(c.tables) == 0 {
			return nil
		}
		table := c.tables[len(c.tables)-1]
		if table.column >= len(table.alignments) {
			c.atomic()
			table.ignoredCell = true
			return nil
		}
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		alignment := table.alignments[table.column]
		if c.ast {
			alignment = n.Alignment
		}
		tag := "td"
		if table.header {
			tag = "th"
		}
		return c.Tag("<" + tag + alignmentAttribute(alignment) + ">")
	case KindFootnoteDefinition:
		c.Buffer()
		c.tight = append(c.tight, false)
		c.Data["tightStack"] = c.tight
		frame := &c.frames[len(c.frames)-1]
		frame.capture = true
		frame.captureDepth = len(c.buffers)
		frame.footnote = true
	case KindFootnoteReference:
		c.atomic()
	}
	return c.err
}

func compileGFMExit(c *CompileContext, t *Token) error {
	n := tokenNode(t)
	switch t.Type {
	case "taskListCheck":
		return c.Tag("/>")
	case "literalAutolink":
		return compileCoreExit(c, t)
	case "literalAutolinkEmail", "literalAutolinkHttp", "literalAutolinkWww":
		if n.Kind == KindText {
			return c.Raw(c.Encode(n.Literal))
		}
		if t.node == nil {
			n.Literal = t.Source()
			n.Destination = n.Literal
			if t.Type == "literalAutolinkEmail" {
				n.Destination = "mailto:" + n.Literal
			}
			if t.Type == "literalAutolinkWww" {
				n.Destination = "http://" + n.Literal
			}
		}
		if err := c.Tag(c.linkOpening(n)); err != nil {
			return err
		}
		if err := c.Raw(c.Encode(n.Literal)); err != nil {
			return err
		}
		return c.Tag("</a>")
	}
	switch n.Kind {
	case KindStrikethrough:
		return c.Tag("</del>")
	case KindTableCell:
		if len(c.tables) == 0 {
			return nil
		}
		table := c.tables[len(c.tables)-1]
		if table.ignoredCell {
			table.ignoredCell = false
			table.column++
			return nil
		}
		tag := "td"
		if table.header {
			tag = "th"
		}
		table.column++
		return c.Tag("</" + tag + ">")
	case KindTableRow:
		if len(c.tables) == 0 {
			return nil
		}
		table := c.tables[len(c.tables)-1]
		for table.column < len(table.alignments) {
			if err := c.LineEndingIfNeeded(); err != nil {
				return err
			}
			tag := "td"
			if table.header {
				tag = "th"
			}
			if err := c.Tag("<" + tag + alignmentAttribute(table.alignments[table.column]) + "></" + tag + ">"); err != nil {
				return err
			}
			table.column++
		}
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		return c.Tag("</tr>")
	case KindTable:
		if len(c.tables) == 0 {
			return nil
		}
		table := c.tables[len(c.tables)-1]
		if table.section != "" {
			if err := c.LineEndingIfNeeded(); err != nil {
				return err
			}
			if err := c.Tag("</" + table.section + ">"); err != nil {
				return err
			}
		}
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		if err := c.Tag("</table>"); err != nil {
			return err
		}
		c.tables = c.tables[:len(c.tables)-1]
		delete(c.Data, "slurpAllLineEndings")
		c.queueEnding(n)
	case KindFootnoteDefinition:
		frame := &c.frames[len(c.frames)-1]
		if !frame.capture {
			return nil
		}
		value, err := c.Resume()
		if err != nil {
			return err
		}
		frame.capture = false
		id := n.Identifier
		if _, exists := c.footnoteDefinitions[id]; !exists {
			c.footnoteDefinitions[id] = value
		}
		c.tight = c.tight[:len(c.tight)-1]
		c.Data["tightStack"] = c.tight
		c.lastWasTag = false
		c.Data["lastWasTag"] = false
	case KindFootnoteReference:
		return c.footnoteCall(n.Identifier)
	}
	return c.err
}

func (c *CompileContext) checkbox(checked bool) error {
	value := "<input type=\"checkbox\" disabled=\"\" "
	if checked {
		value += "checked=\"\" "
	}
	return c.Tag(value + "/>")
}
func alignmentAttribute(alignment Alignment) string {
	switch alignment {
	case AlignLeft:
		return " align=\"left\""
	case AlignCenter:
		return " align=\"center\""
	case AlignRight:
		return " align=\"right\""
	}
	return ""
}

var forbiddenHTMLTagRule = Sequence(String("<"), Optional(String("/")),
	Or(FoldedString("iframe"), FoldedString("noembed"), FoldedString("noframes"), FoldedString("plaintext"), FoldedString("script"), FoldedString("style"), FoldedString("title"), FoldedString("textarea"), FoldedString("xmp")),
	Lookahead(Satisfy(func(code Code) bool {
		return IsMarkdownSpace(code) || IsLineEnding(code) || code == '\f' || code == '/' || code == '>'
	})))

func filterHTML(value string, flow bool) string {
	context := TokenizeContext{view: sourceView{text: value}, Previous: EOF}
	var result strings.Builder
	start := 0
	for index := 0; index < len(value); {
		next := strings.IndexByte(value[index:], '<')
		if next < 0 {
			break
		}
		index += next
		if !flow && index != 0 {
			break
		}
		context.reset(index, false)
		if forbiddenHTMLTagRule(&context, &Effects{context: &context}) {
			result.WriteString(value[start:index])
			result.WriteString("&lt;")
			start = index + 1
		}
		index++
	}
	if start == 0 {
		return value
	}
	result.WriteString(value[start:])
	return result.String()
}

func (c *CompileContext) footnotePrefix() string {
	if c.footnoteConfig.ClobberPrefix != nil {
		return *c.footnoteConfig.ClobberPrefix
	}
	return "user-content-"
}
func (c *CompileContext) footnoteCall(id string) error {
	index := 0
	if c.footnoteCounts[id] == 0 {
		c.footnoteOrder = append(c.footnoteOrder, id)
	}
	c.footnoteCounts[id]++
	for i, existing := range c.footnoteOrder {
		if existing == id {
			index = i + 1
			break
		}
	}
	count := c.footnoteCounts[id]
	safe := htmlEncode(normalizeURI(strings.ToLower(id)))
	prefix := htmlEncode(c.footnotePrefix())
	suffix := ""
	if count > 1 {
		suffix = "-" + strconv.Itoa(count)
	}
	return c.Tag("<sup><a href=\"#" + prefix + "fn-" + safe + "\" id=\"" + prefix + "fnref-" + safe + suffix + "\" data-footnote-ref=\"\" aria-describedby=\"footnote-label\">" + strconv.Itoa(index) + "</a></sup>")
}

func (c *CompileContext) renderFootnotes() error {
	if len(c.footnoteOrder) == 0 {
		return c.err
	}
	config := c.footnoteConfig
	label := config.Label
	if label == "" {
		label = "Footnotes"
	}
	tag := config.LabelTagName
	if tag == "" {
		tag = "h2"
	}
	attributes := "class=\"sr-only\""
	if config.LabelAttributes != nil {
		attributes = *config.LabelAttributes
	}
	if attributes != "" {
		attributes = " " + attributes
	}
	if err := c.LineEndingIfNeeded(); err != nil {
		return err
	}
	if err := c.Tag("<section data-footnotes=\"\" class=\"footnotes\"><" + tag + " id=\"footnote-label\"" + attributes + ">"); err != nil {
		return err
	}
	if err := c.Raw(c.Encode(label)); err != nil {
		return err
	}
	if err := c.Tag("</" + tag + ">"); err != nil {
		return err
	}
	if err := c.LineEndingIfNeeded(); err != nil {
		return err
	}
	if err := c.Tag("<ol>"); err != nil {
		return err
	}
	for index := range len(c.footnoteOrder) {
		id := c.footnoteOrder[index]
		safe := htmlEncode(normalizeURI(strings.ToLower(id)))
		prefix := htmlEncode(c.footnotePrefix())
		var references strings.Builder
		for count := 1; count <= c.footnoteCounts[id]; count++ {
			if count > 1 {
				references.WriteByte(' ')
			}
			suffix := ""
			if count > 1 {
				suffix = "-" + strconv.Itoa(count)
			}
			backLabel := config.BackLabelText
			if backLabel == "" {
				if config.BackLabel != nil {
					backLabel = config.BackLabel(index, count)
				} else {
					backLabel = "Back to reference " + strconv.Itoa(index+1) + suffix
				}
			}
			references.WriteString("<a href=\"#" + prefix + "fnref-" + safe + suffix + "\" data-footnote-backref=\"\" aria-label=\"" + c.Encode(backLabel) + "\" class=\"data-footnote-backref\">↩")
			if count > 1 {
				references.WriteString("<sup>" + strconv.Itoa(count) + "</sup>")
			}
			references.WriteString("</a>")
		}
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		if err := c.Tag("<li id=\"" + prefix + "fn-" + safe + "\">"); err != nil {
			return err
		}
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		value := c.footnoteDefinitions[id]
		endingStart := len(value)
		if endingStart > 0 && value[endingStart-1] == '\n' {
			endingStart--
			if endingStart > 0 && value[endingStart-1] == '\r' {
				endingStart--
			}
		} else if endingStart > 0 && value[endingStart-1] == '\r' {
			endingStart--
		}
		injected := endingStart >= 4 && value[endingStart-4:endingStart] == "</p>"
		if injected {
			value = value[:endingStart-4] + " " + references.String() + value[endingStart-4:]
		}
		if err := c.Tag(value); err != nil {
			return err
		}
		if !injected {
			if err := c.LineEndingIfNeeded(); err != nil {
				return err
			}
			if err := c.Tag(references.String()); err != nil {
				return err
			}
		}
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
		if err := c.Tag("</li>"); err != nil {
			return err
		}
	}
	if err := c.LineEndingIfNeeded(); err != nil {
		return err
	}
	if err := c.Tag("</ol>"); err != nil {
		return err
	}
	if err := c.LineEndingIfNeeded(); err != nil {
		return err
	}
	return c.Tag("</section>")
}

// MathHTML preserves TeX as escaped text with conventional math classes. It
// does not evaluate TeX or require a client-side typesetting implementation.
func MathHTML() HTMLExtension {
	return HTMLExtension{Enter: map[TokenType]HTMLHandler{"mathFlow": compileMathEnter, "mathText": compileMathEnter}, Exit: map[TokenType]HTMLHandler{"mathFlow": compileMathExit, "mathText": compileMathExit}}
}
func compileMathEnter(c *CompileContext, t *Token) error {
	if semanticKind(t) == KindMathBlock {
		if err := c.LineEndingIfNeeded(); err != nil {
			return err
		}
	}
	c.atomic()
	return nil
}
func compileMathExit(c *CompileContext, t *Token) error {
	n := tokenNode(t)
	opening, closing := "<span class=\"math math-inline\">", "</span>"
	if n.Kind == KindMathBlock {
		opening, closing = "<div class=\"math math-display\">", "</div>"
	}
	if err := c.Tag(opening); err != nil {
		return err
	}
	if err := c.Raw(c.Encode(n.Literal)); err != nil {
		return err
	}
	if err := c.Tag(closing); err != nil {
		return err
	}
	if n.Kind == KindMathBlock {
		c.queueEnding(n)
	}
	return c.err
}

func astTokenType(n *Node) TokenType {
	switch n.Kind {
	case KindDocument:
		return "document"
	case KindParagraph:
		return "paragraph"
	case KindHeading:
		return "atxHeading"
	case KindBlockQuote:
		return "blockQuote"
	case KindList:
		if n.Ordered {
			return "listOrdered"
		}
		return "listUnordered"
	case KindListItem:
		return "listItem"
	case KindCodeBlock:
		return "codeFenced"
	case KindHTMLBlock:
		return "htmlFlow"
	case KindThematicBreak:
		return "thematicBreak"
	case KindDefinition:
		return "definition"
	case KindText:
		return "data"
	case KindEmphasis:
		return "emphasis"
	case KindStrong:
		return "strong"
	case KindCodeSpan:
		return "codeText"
	case KindLink:
		return "link"
	case KindImage:
		return "image"
	case KindHTMLInline:
		return "htmlText"
	case KindSoftBreak:
		return "lineEnding"
	case KindHardBreak:
		return "hardBreakEscape"
	case KindStrikethrough:
		return "strikethrough"
	case KindTable:
		return "table"
	case KindTableRow:
		return "tableRow"
	case KindTableCell:
		return "tableCell"
	case KindFootnoteDefinition:
		return "footnoteDefinition"
	case KindFootnoteReference:
		return "gfmFootnoteCall"
	case KindMathBlock:
		return "mathFlow"
	case KindMathInline:
		return "mathText"
	}
	if n.token != nil {
		return n.token.Type
	}
	return TokenType(n.Kind)
}
func (c *CompileContext) renderNode(n *Node) error {
	if n == nil {
		return nil
	}
	token := Token{Type: astTokenType(n), Range: n.Range, node: n, view: n.input}
	if n.token != nil {
		token.Data = n.token.Data
		token.context = n.token.context
		token.view = n.token.view
	}
	if err := c.event(Enter, &token); err != nil {
		return err
	}
	if c.hidden == 0 {
		for _, child := range n.Children {
			if err := c.renderNode(child); err != nil {
				return err
			}
		}
	}
	return c.event(Exit, &token)
}
