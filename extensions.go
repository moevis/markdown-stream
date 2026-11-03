package markdown

import (
	"fmt"
	"unicode/utf8"
)

type constructBucket struct {
	before []*Construct
	core   []*Construct
	after  []*Construct
	final  []*Construct
}

func (b constructBucket) group(priority int) []*Construct {
	switch priority {
	case 0:
		return b.before
	case 1:
		return b.core
	case 2:
		return b.after
	default:
		return b.final
	}
}

type syntaxKey struct {
	phase  Phase
	owner  *Token
	parts  *sourcePart
	base   int
	length int
	anchor int
}

type openToken struct {
	token   *Token
	start   int
	virtual int
	point   Position
}

// TokenizeContext exposes the cursor and event tape of a syntax tokenizer.
// Effects owns cursor movement and transactional token boundaries.
type TokenizeContext struct {
	Phase            Phase
	Document         *Document
	Previous         Code
	Interrupt        bool
	CurrentConstruct *Construct
	Events           []Event

	view          sourceView
	index         int
	virtual       int
	visual        int
	roots         []*Token
	stack         []openToken
	err           error
	parser        *parseState
	inline        *inlineParser
	parent        *Token
	prefix        func() []*Token
	resolveAll    []*Resolver
	registered    map[*Resolver]bool
	resolved      bool
	lastResolveTo bool
	prefixed      bool
	running       int
	undo          []resolverUndo
	matched       *Construct
}

func (c *TokenizeContext) Point() Position {
	point := c.view.position(c.index, false)
	point.Column -= c.virtual
	return point
}
func (c *TokenizeContext) Slice(token *Token) string { return token.Source() }
func (c *TokenizeContext) Error() error              { return c.err }

// Fail stops the current attempt and propagates an extension error to Parse.
func (c *TokenizeContext) Fail(err error) {
	if c.err == nil {
		c.err = err
	}
}

// Node returns the semantic node attached to a token. Custom tokenizers may
// populate this node; concrete ranges continue to be owned by Effects.
func (c *TokenizeContext) Node(token *Token) *Node {
	if token.node == nil {
		token.node = &Node{Kind: KindCustom, Range: token.Range, token: token}
	}
	return token.node
}

func (c *TokenizeContext) Peek() Code {
	if c.virtual != 0 {
		return VirtualSpace
	}
	code := sourceCode(c.view.text, c.index)
	if code == ' ' && c.view.paddingAt(c.index) {
		return VirtualSpace
	}
	return code
}

func sourceCode(text string, index int) Code {
	if index >= len(text) {
		return EOF
	}
	switch text[index] {
	case '\t':
		return HorizontalTab
	case '\n':
		return LineFeed
	case '\r':
		if index+1 < len(text) && text[index+1] == '\n' {
			return CarriageReturnLineFeed
		}
		return CarriageReturn
	}
	code, _ := utf8.DecodeRuneInString(text[index:])
	return Code(code)
}

func previousSourceCode(view sourceView, index int) Code {
	if index == 0 {
		return EOF
	}
	if view.text[index-1] == '\n' {
		if index > 1 && view.text[index-2] == '\r' {
			return CarriageReturnLineFeed
		}
		return LineFeed
	}
	if view.text[index-1] == '\r' {
		return CarriageReturn
	}
	if view.text[index-1] == '\t' {
		return VirtualSpace
	}
	code, _ := utf8.DecodeLastRuneInString(view.text[:index])
	return Code(code)
}

func (c *TokenizeContext) reset(index int, interrupt bool) {
	if c.index != index || c.visual == 0 {
		start := index
		for start > 0 && c.view.text[start-1] != '\n' && c.view.text[start-1] != '\r' {
			start--
		}
		c.visual = sourceVisualColumn(c.view, index) - sourceVisualColumn(c.view, start) + 1
		c.index, c.virtual = index, 0
		c.Previous = previousSourceCode(c.view, index)
	}
	c.Interrupt = interrupt
	c.Events, c.roots, c.stack = c.Events[:0], c.roots[:0], c.stack[:0]
	c.lastResolveTo = false
	c.prefixed = false
}

// Effects is the only cursor-mutating interface exposed to a tokenizer.
type Effects struct{ context *TokenizeContext }

func (e *Effects) Enter(kind TokenType) *Token {
	c := e.context
	token := makeToken(kind, c.view.slice(c.index, c.index))
	token.context = c
	if len(c.stack) == 0 {
		c.roots = append(c.roots, token)
	} else {
		c.stack[len(c.stack)-1].token.append(token)
	}
	c.stack = append(c.stack, openToken{token: token, start: c.index, virtual: c.virtual, point: c.Point()})
	c.Events = append(c.Events, Event{Kind: Enter, Token: token, Context: c})
	return token
}

func (e *Effects) Exit(kind TokenType) *Token {
	c := e.context
	if len(c.stack) == 0 || c.stack[len(c.stack)-1].token.Type != kind {
		c.Fail(fmt.Errorf("markdown: exiting unopened token %q", kind))
		return nil
	}
	frame := c.stack[len(c.stack)-1]
	c.stack = c.stack[:len(c.stack)-1]
	token := frame.token
	token.view = c.view.slice(frame.start, c.index)
	token.Range = SourceRange{Start: frame.point, End: c.Point()}
	if frame.virtual != 0 && frame.start > 0 {
		padding := frame.virtual
		if c.index == frame.start {
			padding -= c.virtual
		}
		if padding > 0 {
			token.view = joinViews([]sourceView{paddingView(padding, c.view.slice(frame.start-1, frame.start)), token.view})
		}
	}
	if token.node != nil {
		token.node.Range = token.Range
	}
	c.Events = append(c.Events, Event{Kind: Exit, Token: token, Context: c})
	return token
}

func (e *Effects) Consume(code Code) {
	c := e.context
	if code == EOF || c.Peek() != code {
		c.Fail(fmt.Errorf("markdown: consuming %d at %d:%d, found %d", code, c.Point().Line, c.Point().Column, c.Peek()))
		return
	}
	if code == VirtualSpace {
		if c.virtual > 0 {
			c.virtual--
		} else {
			c.index++
		}
		c.visual++
	} else if code == HorizontalTab {
		width := 4 - (sourceVisualColumn(c.view, c.index)-1)%4
		c.index++
		c.virtual = width - 1
		c.visual++
	} else if IsLineEnding(code) {
		c.index++
		if code == CarriageReturnLineFeed {
			c.index++
		}
		c.visual = 1
	} else {
		_, size := utf8.DecodeRuneInString(c.view.text[c.index:])
		c.index += size
		c.visual++
		if code > 0xffff {
			c.visual++
		}
	}
	c.Previous = code
}

type savedOpenToken struct {
	frame     openToken
	children  []*Token
	view      sourceView
	range_    SourceRange
	nodeRange SourceRange
}

type tokenizerSnapshot struct {
	index, virtual, visual int
	previous               Code
	interrupt              bool
	construct              *Construct
	events, roots, depth   int
	eventTape              []Event
	rootTape               []*Token
	resolveAll             []*Resolver
	resolveTo              bool
	prefixed               bool
	matched                *Construct
	undoIndex              int
	short                  [8]savedOpenToken
	long                   []savedOpenToken
}

func (c *TokenizeContext) snapshot() tokenizerSnapshot {
	state := tokenizerSnapshot{index: c.index, virtual: c.virtual, visual: c.visual, previous: c.Previous, interrupt: c.Interrupt, construct: c.CurrentConstruct, matched: c.matched, events: len(c.Events), roots: len(c.roots), depth: len(c.stack), eventTape: c.Events, rootTape: c.roots, resolveAll: c.resolveAll, resolveTo: c.lastResolveTo, prefixed: c.prefixed, undoIndex: len(c.undo)}
	if len(c.stack) > len(state.short) {
		state.long = make([]savedOpenToken, len(c.stack))
	}
	for index, frame := range c.stack {
		entry := savedOpenToken{frame: frame, children: frame.token.Children, view: frame.token.view, range_: frame.token.Range}
		if frame.token.node != nil {
			entry.nodeRange = frame.token.node.Range
		}
		if state.long != nil {
			state.long[index] = entry
		} else {
			state.short[index] = entry
		}
	}
	return state
}

func (c *TokenizeContext) restore(state *tokenizerSnapshot) {
	c.restoreResolvers(state.undoIndex)
	c.lastResolveTo = state.resolveTo
	c.prefixed = state.prefixed
	c.matched = state.matched
	c.index, c.virtual, c.visual = state.index, state.virtual, state.visual
	c.Previous, c.Interrupt, c.CurrentConstruct = state.previous, state.interrupt, state.construct
	for _, resolver := range c.resolveAll[len(state.resolveAll):] {
		delete(c.registered, resolver)
	}
	c.resolveAll = state.resolveAll
	c.Events, c.roots = state.eventTape[:state.events], state.rootTape[:state.roots]
	if cap(c.stack) < state.depth {
		c.stack = make([]openToken, state.depth)
	} else {
		c.stack = c.stack[:state.depth]
	}
	for index := range state.depth {
		var entry savedOpenToken
		if state.long != nil {
			entry = state.long[index]
		} else {
			entry = state.short[index]
		}
		c.stack[index] = entry.frame
		entry.frame.token.Children = entry.children
		if entry.frame.token.node != nil {
			entry.frame.token.node.Range = entry.nodeRange
		}
		entry.frame.token.view, entry.frame.token.Range = entry.view, entry.range_
	}
}

func (c *TokenizeContext) run(construct *Construct, tokenizer Tokenizer) (bool, error) {
	if tokenizer == nil && construct.Rule == nil {
		return false, nil
	}
	c.running++
	previous := c.CurrentConstruct
	if !construct.Partial {
		c.CurrentConstruct = construct
	}
	if tokenizer == nil {
		matched := construct.Rule(c, &Effects{context: c})
		c.running--
		c.CurrentConstruct = previous
		if matched && c.err == nil {
			c.matched = construct
		}
		return matched, c.err
	}
	outcome := 0
	ok := State(func(Code) State { outcome = 1; return nil })
	nok := State(func(Code) State { outcome = -1; return nil })
	effects := &Effects{context: c}
	state := tokenizer(c, effects, ok, nok)
	for state != nil && c.err == nil {
		state = state(c.Peek())
	}
	c.running--
	c.CurrentConstruct = previous
	if c.err != nil {
		return false, c.err
	}
	if outcome == 0 {
		return false, fmt.Errorf("markdown: construct %q ended without ok or nok", construct.Name)
	}
	if outcome == 1 {
		c.matched = construct
	}
	return outcome == 1, nil
}

func (e *Effects) branch(constructs []Construct, ok, nok State, check, interrupt bool) State {
	return func(Code) State {
		c := e.context
		for index := range constructs {
			construct := &constructs[index]
			if c.parser != nil && !c.parser.enabled(construct.Name) || construct.Previous != nil && !construct.Previous(c.Previous) {
				continue
			}
			if !check && c.Phase == PhaseDocument && (construct.Resolve != nil || construct.ResolveTo != nil) && (c.parser == nil || !c.parser.checking) {
				// Child flow is committed by another phase. Refresh only when a
				// resolver needs its topology; ordinary continuation stays linear.
				// An enclosing attempt may still hold the previous event tape.
				c.Events = appendOpenEvents(c, c.roots, nil)
			}
			state := c.snapshot()
			c.Interrupt = interrupt || state.interrupt
			matched, err := c.run(construct, construct.Tokenize)
			if err != nil {
				c.Fail(err)
				return nil
			}
			if matched && !check {
				if !construct.Partial {
					c.CurrentConstruct = construct
				}
				if err := c.resolveConstruct(construct, state.events); err != nil {
					c.Fail(err)
					return nil
				}
			}
			c.CurrentConstruct = state.construct

			if !matched || check {
				c.restore(&state)
			} else {
				c.Interrupt = state.interrupt
			}
			if matched {
				return ok(c.Peek())
			}
		}
		return nok(e.context.Peek())
	}
}

// Attempt commits a successful branch and rolls back each nonmatching branch.
func (e *Effects) Attempt(constructs []Construct, ok, nok State) State {
	return e.branch(constructs, ok, nok, false, false)
}

// Check is lookahead: both success and failure restore cursor and token effects.
func (e *Effects) Check(constructs []Construct, ok, nok State) State {
	return e.branch(constructs, ok, nok, true, false)
}

// Interrupt performs lookahead with the interruption flag visible to tokenizers.
func (e *Effects) Interrupt(constructs []Construct, ok, nok State) State {
	return e.branch(constructs, ok, nok, true, true)
}

func (c *TokenizeContext) prependResolverPrefix() int {
	if c.prefixed {
		return 0
	}
	c.prefixed = true
	var prefix []*Token
	if c.prefix != nil {
		prefix = c.prefix()
	} else if c.parent != nil {
		prefix = c.parent.Children
		// An open document root is already linked into its parent. Only
		// siblings before this context belong to the committed prefix.
		if len(c.roots) != 0 {
			for index, token := range prefix {
				if token == c.roots[0] {
					prefix = prefix[:index]
					break
				}
			}
		}
	}
	if len(prefix) == 0 {
		return 0
	}
	events := appendEvents(prefix, nil)
	count := len(events)
	c.Events = append(events, c.Events...)
	roots := make([]*Token, 0, len(prefix)+len(c.roots))
	roots = append(roots, prefix...)
	c.roots = append(roots, c.roots...)
	return count
}

func (c *TokenizeContext) resolveConstruct(construct *Construct, start int) error {
	if c.parser != nil && c.parser.checking {
		return nil
	}
	if resolver := construct.ResolveAll; resolver != nil && resolver.Resolve != nil {
		if c.registered == nil {
			c.registered = make(map[*Resolver]bool)
		}
		if !c.registered[resolver] {
			c.registered[resolver] = true
			c.resolveAll = append(c.resolveAll, resolver)
		}
	}
	if construct.ResolveTo != nil && construct.ResolveTo.Resolve != nil {
		start += c.prependResolverPrefix()
	}
	if construct.Resolve != nil || construct.ResolveTo != nil {
		c.saveResolverUndo()
	}
	if construct.Resolve != nil && construct.Resolve.Resolve != nil {
		resolved, err := construct.Resolve.Resolve(c.Events[start:], c)
		if err != nil {
			return err
		}
		if c.err != nil {
			return c.err
		}
		c.Events = append(c.Events[:start], resolved...)
	}
	if construct.ResolveTo != nil && construct.ResolveTo.Resolve != nil {
		resolved, err := construct.ResolveTo.Resolve(c.Events, c)
		if err != nil {
			return err
		}
		if c.err != nil {
			return c.err
		}
		c.Events = resolved
		c.lastResolveTo = true
	}
	if construct.Resolve != nil || construct.ResolveTo != nil {
		roots, err := rootsFromEvents(c.Events, true)
		if err != nil {
			return err
		}
		c.roots = roots
	}
	return c.err
}

func appendOpenEvents(context *TokenizeContext, tokens []*Token, events []Event) []Event {
	for _, token := range tokens {
		events = append(events, Event{Kind: Enter, Token: token, Context: token.context})
		events = appendOpenEvents(context, token.Children, events)
		open := false
		for _, frame := range context.stack {
			if frame.token == token {
				open = true
				break
			}
		}
		if !open {
			events = append(events, Event{Kind: Exit, Token: token, Context: token.context})
		}
	}
	return events
}

// rootsFromEvents re-links concrete tokens from a resolver's event topology,
// never from an AST or inferred source boundaries.
func rootsFromEvents(events []Event, allowOpen bool) ([]*Token, error) {
	var roots, stack []*Token
	seen := make(map[*Token]bool)
	for _, event := range events {
		if event.Token == nil {
			return nil, fmt.Errorf("markdown: resolver returned a nil token")
		}
		token := event.Token
		if event.Kind == Enter {
			if seen[token] {
				return nil, fmt.Errorf("markdown: resolver entered token %q more than once", token.Type)
			}
			seen[token] = true
			token.Children = token.Children[:0]
			if len(stack) == 0 {
				roots = append(roots, token)
			} else {
				stack[len(stack)-1].append(token)
			}
			stack = append(stack, token)
		} else if event.Kind == Exit {
			if len(stack) == 0 || stack[len(stack)-1] != token {
				return nil, fmt.Errorf("markdown: resolver returned unbalanced events for %q", token.Type)
			}
			stack = stack[:len(stack)-1]
		} else {
			return nil, fmt.Errorf("markdown: resolver returned unknown event kind %d", event.Kind)
		}
	}
	if len(stack) != 0 && !allowOpen {
		return nil, fmt.Errorf("markdown: resolver left %q open", stack[len(stack)-1].Type)
	}
	return roots, nil
}

func syntaxTables(extension SyntaxExtension) [6]map[Code][]Construct {
	return [6]map[Code][]Construct{extension.Document, extension.FlowInitial, extension.Flow, extension.ContentInitial, extension.Text, extension.String}
}

func (s *parseState) initSyntax() {
	if s.syntaxReady {
		return
	}
	s.syntaxReady = true
	appendConstructs := func(extension SyntaxExtension, priority int) {
		for phase, table := range syntaxTables(extension) {
			for code, constructs := range table {
				if s.constructs[phase] == nil {
					s.constructs[phase] = make(map[Code]constructBucket)
				}
				bucket := s.constructs[phase][code]
				for index := range constructs {
					construct := &constructs[index]
					if construct.AddAfter != (priority >= 2) {
						continue
					}
					switch priority {
					case 0:
						bucket.before = append(bucket.before, construct)
					case 1:
						bucket.core = append(bucket.core, construct)
					case 2:
						bucket.after = append(bucket.after, construct)
					case 3:
						bucket.final = append(bucket.final, construct)
					}
				}
				s.constructs[phase][code] = bucket
			}
		}
	}
	for index := len(s.options.Extensions) - 1; index >= 0; index-- {
		appendConstructs(s.options.Extensions[index], 0)
	}
	for _, extension := range coreSyntaxes {
		appendConstructs(extension, 1)
	}
	for _, extension := range s.options.Extensions {
		appendConstructs(extension, 2)
	}
	for _, extension := range coreSyntaxes {
		appendConstructs(extension, 3)
	}
}

func (s *parseState) contextFor(phase Phase, view sourceView) *TokenizeContext {
	s.initSyntax()
	if len(s.constructs[phase]) == 0 {
		return nil
	}
	key := syntaxKey{phase: phase}
	if phase <= PhaseContentInitial && s.blockOwner != nil {
		key.owner = s.blockOwner
	} else {
		key.owner = s.inlineOwner
		key.base, key.length, key.anchor = view.base, len(view.text), view.anchor
		if len(view.parts) != 0 {
			key.parts = &view.parts[0]
		}
	}
	if context := s.contextMap[key]; context != nil {
		if !sameSourceView(context.view, view) {
			context.visual = 0
			context.virtual = 0
		}
		context.view = view
		return context
	}
	if s.contextMap == nil {
		s.contextMap = make(map[syntaxKey]*TokenizeContext)
	}
	context := &TokenizeContext{Phase: phase, Document: s.doc, view: view, parser: s, parent: key.owner, Previous: EOF}
	if phase == PhaseDocument && key.owner != nil && key.owner.context != nil {
		context.CurrentConstruct = key.owner.context.CurrentConstruct
	}
	s.contextMap[key] = context
	s.contexts = append(s.contexts, context)
	return context
}

func (s *parseState) trySyntax(phase Phase, view sourceView, index int, after, interrupt bool) (int, []*Token, bool, error) {
	return s.tryConstruct(phase, view, index, after, interrupt, false)
}

func (s *parseState) tryConstruct(phase Phase, view sourceView, index int, after, interrupt, container bool) (int, []*Token, bool, error) {
	c := s.contextFor(phase, view)
	var flowView sourceView
	flowReady := false
	if c == nil {
		return index, nil, false, nil
	}
	defer c.releaseResolverUndo()
	c.reset(index, interrupt)
	code := c.Peek()
	first := 0
	if after {
		first = 2
	}
	for priority := first; priority < first+2; priority++ {
		for _, key := range [2]Code{code, Fallback} {
			for _, construct := range s.constructs[phase][key].group(priority) {
				if !s.enabled(construct.Name) || (construct.Continuation != nil) != container {
					continue
				}
				c.reset(index, interrupt)
				c.view = view
				if (phase == PhaseFlowInitial || phase == PhaseFlow) && !construct.native && !construct.Concrete {
					if !flowReady {
						var err error
						flowView, err = s.blockFlowView(view, index)
						if err != nil {
							return index, nil, false, err
						}
						flowReady = true
					}
					c.view = flowView
				}
				if construct.Previous != nil && !construct.Previous(c.Previous) {
					continue
				}
				rootStart, eventStart := len(c.roots), len(c.Events)
				state := c.snapshot()
				matched, err := c.run(construct, construct.Tokenize)
				if err != nil {
					return index, nil, false, err
				}
				if !matched {
					c.restore(&state)
					continue
				}
				if c.index == index && c.virtual == state.virtual && code != EOF {
					c.restore(&state)
					continue
				}
				if !container && !construct.Partial && len(c.stack) != state.depth {
					return index, nil, false, fmt.Errorf("markdown: construct %q left a token open", construct.Name)
				}
				if !construct.Partial {
					c.CurrentConstruct = construct
				}
				if err := c.resolveConstruct(construct, eventStart); err != nil {
					return index, nil, false, err
				}
				c.matched = construct
				roots := c.roots
				if !c.lastResolveTo {
					roots = roots[rootStart:]
				}
				return c.index, roots, true, nil
			}
		}
	}
	return index, nil, false, nil
}

func semanticNode(token *Token) *Node {
	if token.node != nil {
		return token.node
	}
	kind := KindCustom
	if token.Type == "data" {
		kind = KindText
	}
	node := &Node{Kind: kind, Range: token.Range, token: token}
	if kind == KindText {
		node.Literal = token.Source()
		if value, ok := token.Data["value"].(string); ok {
			node.Literal = value
		}
	}
	token.node = node
	for _, child := range token.Children {
		appendSemantic(node, child)
	}
	return node
}

func appendSemantic(parent *Node, token *Token) {
	if token.node != nil && token.node.token == token {
		parent.AppendChild(token.node)
		return
	}
	for _, child := range token.Children {
		appendSemantic(parent, child)
	}
}

func (s *parseState) appendSyntax(parent *Node, roots []*Token, replacesPrefix bool) {
	if replacesPrefix {
		// Existing concrete margins must not acquire default custom AST nodes.
		var concreteOnly map[*Token]struct{}
		for _, token := range parent.token.Children {
			if token.node == nil {
				if concreteOnly == nil {
					concreteOnly = make(map[*Token]struct{})
				}
				concreteOnly[token] = struct{}{}
			}
		}
		for _, token := range roots {
			if _, preserved := concreteOnly[token]; !preserved {
				semanticNode(token)
			}
		}
		parent.token.Children = roots
		syncSemanticChildren(parent, roots)
		return
	}
	for _, token := range roots {
		node := semanticNode(token)
		appendNode(parent, node)
	}
}

func (s *parseState) resolveInsideSpan(tokens []*Token, view sourceView) ([]*Token, error) {
	if len(s.insideSpan) == 0 {
		return tokens, nil
	}
	context := &TokenizeContext{Phase: PhaseText, Document: s.doc, view: view, index: len(view.text), parser: s, Previous: previousSourceCode(view, len(view.text))}
	events := appendEvents(tokens, nil)
	seen := make(map[*Resolver]bool, len(s.insideSpan))
	for _, resolver := range s.insideSpan {
		if resolver == nil || resolver.Resolve == nil || seen[resolver] {
			continue
		}
		seen[resolver] = true
		context.Events = events
		resolved, err := resolver.Resolve(events, context)
		if err != nil {
			return nil, err
		}
		if context.err != nil {
			return nil, context.err
		}
		events = resolved
	}
	return rootsFromEvents(events, false)
}

func (s *parseState) finishSyntax(phase Phase, view sourceView, parent *Token) error {
	context := s.contextFor(phase, view)
	if context == nil || len(context.resolveAll) == 0 {
		return nil
	}
	return s.finishContext(context, parent)
}

func syncSemanticChildren(parent *Node, tokens []*Token) {
	for _, child := range parent.Children {
		child.parent = nil
	}
	parent.Children = parent.Children[:0]
	var attach func(*Token)
	attach = func(token *Token) {
		if node := token.node; node != nil && node.token == token && node != parent {
			if semanticContainer(node.Kind) {
				syncSemanticChildren(node, token.Children)
			}
			parent.AppendChild(node)
			return
		}
		for _, child := range token.Children {
			attach(child)
		}
	}
	for _, token := range tokens {
		attach(token)
	}
}

func semanticContainer(kind NodeKind) bool {
	switch kind {
	case KindDocument, KindParagraph, KindHeading, KindBlockQuote, KindList, KindListItem, KindEmphasis, KindStrong, KindLink, KindImage, KindStrikethrough, KindTable, KindTableRow, KindTableCell, KindFootnoteDefinition, KindCustom:
		return true
	}
	return false
}

// openDocumentContainer commits only the opening prefix. Continuations are
// attempted by the block driver after the preceding child flow has settled.
func (s *parseState) openDocumentContainer(view sourceView, index int, after, interrupt bool) (int, []*Token, *TokenizeContext, bool, error) {
	end, roots, matched, err := s.tryConstruct(PhaseDocument, view, index, after, interrupt, true)
	if err != nil || !matched {
		return index, nil, nil, false, err
	}
	context := s.contextFor(PhaseDocument, view)
	construct := context.matched
	if len(roots) == 0 || len(context.stack) == 0 {
		return index, nil, nil, false, fmt.Errorf("markdown: document construct %q did not open a container", construct.Name)
	}
	lines := sourceLines(view.slice(index, len(view.text)))
	if len(lines) == 0 || end > index+len(lines[0].body.text) {
		return index, nil, nil, false, fmt.Errorf("markdown: document prefix crosses a physical line")
	}
	return end, roots, context, true, nil
}

// mergeContainerPrefixes relocates only recorded margins. Resolver ordering and
// unrelated token ownership are preserved.
func mergeContainerPrefixes(container *Token) {
	for index := 0; index < len(container.Children); {
		margin := container.Children[index]
		if !margin.recordedMargin {
			index++
			continue
		}
		owner := recordedMarginOwner(container, margin)
		if owner == nil {
			index++
			continue
		}
		copy(container.Children[index:], container.Children[index+1:])
		container.Children[len(container.Children)-1] = nil
		container.Children = container.Children[:len(container.Children)-1]
		at := len(owner.Children)
		for childIndex, child := range owner.Children {
			if child.Range.Start.ByteOffset >= margin.Range.Start.ByteOffset {
				at = childIndex
				break
			}
		}
		owner.Children = append(owner.Children, nil)
		copy(owner.Children[at+1:], owner.Children[at:])
		owner.Children[at] = margin
	}
}

func recordedMarginOwner(container, margin *Token) *Token {
	var owner *Token
	for _, candidate := range container.Children {
		if candidate == margin || candidate.recordedMargin || len(candidate.Children) == 0 {
			continue
		}
		if candidate.Range.Start.ByteOffset > margin.Range.Start.ByteOffset || candidate.Range.End.ByteOffset < margin.Range.End.ByteOffset {
			continue
		}
		if nested := recordedMarginOwner(candidate, margin); nested != nil {
			candidate = nested
		}
		if owner == nil || candidate.Range.End.ByteOffset-candidate.Range.Start.ByteOffset < owner.Range.End.ByteOffset-owner.Range.Start.ByteOffset {
			owner = candidate
		}
	}
	return owner
}
