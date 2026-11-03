package markdown

import "fmt"

// blockFrame retains only the continuation, lazy-line and list-tightness state
// that cannot be expressed as independent line constructs.
type blockFrame struct {
	node      *Node
	kind      NodeKind
	marker    byte
	delimiter byte
	indent    int
	raw       []sourceView
	ending    sourceView
	blank     bool
	loose     bool
	context   *TokenizeContext
	construct *Construct
}

type blockLeaf struct {
	node        *Node
	kind        NodeKind
	lines       []sourceLine
	raw         []sourceView
	fence       byte
	fenceIndent int
	htmlClass   int
	closing     Rule
	pending     []sourceLine
}

type blockListMarker struct {
	ordered   bool
	marker    byte
	delimiter byte
	start     int
	indent    int
	empty     bool
}

func blockIndent(view sourceView) (bytes, columns int) {
	for bytes < len(view.text) {
		switch view.text[bytes] {
		case ' ':
			columns++
		case '\t':
			columns += 4 - (sourceVisualColumn(view, bytes)-1)%4
		default:
			return
		}
		bytes++
	}
	return
}

func blockBlank(text string) bool {
	for i := range text {
		if text[i] != ' ' && text[i] != '\t' {
			return false
		}
	}
	return true
}

// Removing a visual margin can leave source-backed padding from a partial tab.
func removeBlockIndent(view sourceView, columns int) (sourceView, sourceView) {
	i, col := 0, 0
	for i < len(view.text) && col < columns {
		c := view.text[i]
		if c != ' ' && c != '\t' {
			break
		}
		width := 1
		if c == '\t' {
			width = 4 - (sourceVisualColumn(view, i)-1)%4
		}
		if col+width > columns {
			return joinViews([]sourceView{paddingView(col+width-columns, view.slice(i, i+1)), view.slice(i+1, len(view.text))}), view.slice(0, i+1)
		}
		col += width
		i++
	}
	return view.slice(i, len(view.text)), view.slice(0, i)
}

func blockTrim(view sourceView) (sourceView, sourceView, sourceView) {
	a, b := 0, len(view.text)
	for a < b && (view.text[a] == ' ' || view.text[a] == '\t') {
		a++
	}
	for b > a && (view.text[b-1] == ' ' || view.text[b-1] == '\t') {
		b--
	}
	return view.slice(a, b), view.slice(0, a), view.slice(b, len(view.text))
}

func blockContentView(lines []sourceLine) sourceView {
	parts := make([]sourceView, 0, len(lines)*2)
	for i, line := range lines {
		parts = append(parts, line.body)
		if i+1 < len(lines) {
			parts = append(parts, line.ending)
		}
	}
	return joinViews(parts)
}

func setBlockView(node *Node, views []sourceView, ending sourceView) {
	view := joinViews(views)
	// A block's final physical ending belongs to its enclosing flow. Internal
	// endings remain in the logical view, including gaps around source margins.
	if (node.Kind == KindParagraph || node.Kind == KindHeading) && len(ending.text) > 0 {
		view = view.slice(0, len(view.text)-len(ending.text))
	}
	node.Range = view.span()
	node.ending = ending
	if node.Kind != KindParagraph && node.Kind != KindHeading && node.Kind != KindTableCell {
		node.input = sourceView{}
	}
	if node.token != nil {
		node.token.view = view
		node.token.Range = node.Range
	}
}

func blockRuleMatch(view sourceView, rule Rule) (*Token, *TokenizeContext, bool, error) {
	context := &TokenizeContext{view: view, visual: 1, Previous: EOF}
	context.CurrentConstruct = &blockContinuationConstruct
	if !rule(context, &Effects{context: context}) {
		return nil, context, false, context.err
	}
	context.matched = &blockContinuationConstruct
	if len(context.roots) == 0 {
		return nil, context, true, context.err
	}
	return context.roots[0], context, true, context.err
}

func blockQuotePrefix(view sourceView) (sourceView, *Token, bool) {
	token, _, ok, _ := blockRuleMatch(view, blockQuoteRule)
	if !ok {
		return view, nil, false
	}
	match, _ := token.Data["nativeBlock"].(*blockSyntaxMatch)
	blockCommitCapture(token)
	return match.body, token, true
}

func recordBlockPrefix(owner, prefix *Token) {
	prefix.recordedMargin = true
	owner.append(prefix)
}

// blockContinueDocument uses the same Effects transaction as an opening
// attempt. A rejected (including lazy) prefix cannot leak tokens or ranges.
func blockContinueDocument(frame *blockFrame, line sourceLine, record bool) (sourceLine, bool, error) {
	context := frame.context
	oldView, oldIndex, oldVirtual, oldVisual, oldPrevious := context.view, context.index, context.virtual, context.visual, context.Previous
	views := append(frame.raw, line.full())
	context.view = joinViews(views)
	start := len(context.view.text) - len(line.full().text)
	context.index, context.virtual, context.visual = start, 0, 1
	context.Previous = previousSourceCode(context.view, start)
	state := context.snapshot()
	previousCheck := context.parser.checking
	context.parser.checking = previousCheck || !record
	continued, err := context.run(frame.construct, frame.construct.Continuation)
	context.parser.checking = previousCheck
	end := context.index
	if err == nil && continued && (end < start || end > start+len(line.body.text) || len(context.stack) != state.depth) {
		err = fmt.Errorf("markdown: document continuation %q crossed a line or changed its open containers", frame.construct.Name)
	}
	if err == nil && continued {
		for index, open := range context.stack {
			var previous savedOpenToken
			if state.long != nil {
				previous = state.long[index]
			} else {
				previous = state.short[index]
			}
			if open.token != previous.frame.token {
				err = fmt.Errorf("markdown: document continuation %q replaced an open container", frame.construct.Name)
				break
			}
			if record {
				for _, prefix := range open.token.Children[len(previous.children):] {
					if prefix.node == nil {
						prefix.recordedMargin = true
					}
				}
			}
		}
	}
	if err == nil && continued {
		line.body = context.view.slice(end, start+len(line.body.text))
		if context.virtual > 0 && end > start {
			line.body = joinViews([]sourceView{paddingView(context.virtual, context.view.slice(end-1, end)), line.body})
		}
	}
	if !record || !continued || err != nil {
		context.restore(&state)
		context.view, context.index, context.virtual, context.visual, context.Previous = oldView, oldIndex, oldVirtual, oldVisual, oldPrevious
	}
	context.releaseResolverUndo()
	return line, continued, err
}

func blockContinueFrame(frame *blockFrame, line sourceLine, record bool) (sourceLine, bool, error) {
	if frame.context != nil {
		return blockContinueDocument(frame, line, record)
	}
	switch frame.kind {
	case KindBlockQuote:
		body, prefix, ok := blockQuotePrefix(line.body)
		if !ok {
			return line, false, nil
		}
		if record {
			recordBlockPrefix(frame.node.token, prefix)
		}
		line.body = body
	case KindListItem, KindFootnoteDefinition:
		_, indent := blockIndent(line.body)
		if !blockBlank(line.body.text) && indent < frame.indent {
			return line, false, nil
		}
		body, prefix := removeBlockIndent(line.body, frame.indent)
		if record && len(prefix.text) > 0 {
			kind := TokenType("listItemIndent")
			if frame.kind == KindFootnoteDefinition {
				kind = "footnoteDefinitionIndent"
			}
			recordBlockPrefix(frame.node.token, makeToken(kind, prefix))
		}
		line.body = body
	}
	return line, true, nil
}

func blockContainerLine(stack []*blockFrame, line sourceLine, record bool) (sourceLine, bool, error) {
	original := line
	for _, frame := range stack[1:] {
		before := line.full()
		var ok bool
		var err error
		line, ok, err = blockContinueFrame(frame, line, record)
		if !ok || err != nil {
			return line, false, err
		}
		if record {
			frame.raw = append(frame.raw, before)
			frame.ending = original.ending
		}
	}
	return line, true, nil
}

func blockLogicalRemaining(first sourceLine, remaining []sourceLine, stack []*blockFrame) (sourceView, error) {
	views := []sourceView{first.full()}
	for _, original := range remaining {
		line, ok, err := blockContainerLine(stack, original, false)
		if err != nil {
			return sourceView{}, err
		}
		if !ok {
			break
		}
		views = append(views, line.full())
	}
	return joinViews(views), nil
}

func blockCloseDocument(frame *blockFrame) error {
	context := frame.context
	context.view = joinViews(frame.raw)
	context.index, context.virtual = len(context.view.text), 0
	context.Previous = previousSourceCode(context.view, context.index)
	context.visual = sourceVisualColumn(context.view, context.index)
	// Child flow has its own owner context. Expose its actual token tape to
	// Exit, retaining opening tokens as open events.
	context.Events = appendOpenEvents(context, context.roots, context.Events[:0])
	if frame.construct.Exit != nil {
		if err := frame.construct.Exit(context); err != nil {
			return err
		}
	}
	if context.err != nil {
		return context.err
	}
	effects := &Effects{context: context}
	for len(context.stack) > 0 {
		token := effects.Exit(context.stack[len(context.stack)-1].token.Type)
		if token.node != nil {
			token.node.ending = frame.ending
		}
	}
	return context.err
}

type blockExtensionMatch struct {
	end            int
	roots          []*Token
	context        *TokenizeContext
	replacesPrefix bool
}

func (s *parseState) tryBlockExtension(phase Phase, view sourceView, after, interrupt bool) (blockExtensionMatch, bool, error) {
	if phase == PhaseDocument {
		end, roots, context, ok, err := s.openDocumentContainer(view, 0, after, interrupt)
		if err != nil {
			return blockExtensionMatch{}, false, err
		}
		if ok && end > 0 {
			return blockExtensionMatch{end: end, roots: roots, context: context, replacesPrefix: context.lastResolveTo}, true, nil
		}
	}
	end, roots, ok, err := s.trySyntax(phase, view, 0, after, interrupt)
	if err != nil {
		return blockExtensionMatch{}, false, err
	}
	if ok && end > 0 {
		replacesPrefix := false
		if context := s.contextMap[syntaxKey{phase: phase, owner: s.blockOwner}]; context != nil {
			replacesPrefix = context.lastResolveTo
		}
		return blockExtensionMatch{end: end, roots: roots, replacesPrefix: replacesPrefix}, true, nil
	}
	return blockExtensionMatch{}, false, nil
}

func (s *parseState) matchBlock(view sourceView, interrupt bool) (blockExtensionMatch, bool, error) {
	for _, phase := range [3]Phase{PhaseDocument, PhaseFlowInitial, PhaseFlow} {
		for _, after := range [2]bool{false, true} {
			match, ok, err := s.tryBlockExtension(phase, view, after, interrupt)
			if err != nil || ok {
				return match, ok, err
			}
		}
	}
	return blockExtensionMatch{}, false, nil
}

func (s *parseState) blockInterrupt(view sourceView) (bool, error) {
	return s.blockStarts(view, true)
}

func (s *parseState) blockStarts(view sourceView, interrupt bool) (bool, error) {
	return s.blockPreview(view, interrupt, false)
}

func (s *parseState) blockPreview(view sourceView, interrupt, documentOnly bool) (bool, error) {
	if blockBlank(view.text) {
		return !documentOnly, nil
	}
	kind, matched, err := s.previewBlockKind(view, interrupt, documentOnly)
	return matched && kind != "table" && kind != "paragraph", err
}

func (s *parseState) previewBlockKind(view sourceView, interrupt, documentOnly bool) (string, bool, error) {
	previousCheck := s.checking
	contextCount, owner := len(s.contexts), s.blockOwner
	s.checking = true
	type previewContext struct {
		context *TokenizeContext
		saved   TokenizeContext
	}
	var previews [4]previewContext
	for phase := PhaseDocument; phase <= PhaseContentInitial; phase++ {
		context := s.contextMap[syntaxKey{phase: phase, owner: owner}]
		if context == nil {
			continue
		}
		previews[phase] = previewContext{context: context, saved: *context}
		context.Events, context.roots, context.stack = nil, nil, nil
	}
	defer func() {
		s.checking = previousCheck
		for phase, preview := range previews {
			if preview.context == nil {
				delete(s.contextMap, syntaxKey{phase: Phase(phase), owner: owner})
				continue
			}
			err := preview.context.err
			*preview.context = preview.saved
			if err != nil {
				preview.context.Fail(err)
			}
		}
		s.contexts = s.contexts[:contextCount]
	}()
	var match blockExtensionMatch
	var matched bool
	var err error
	if documentOnly {
		for _, after := range [2]bool{false, true} {
			match, matched, err = s.tryBlockExtension(PhaseDocument, view, after, interrupt)
			if matched || err != nil {
				break
			}
		}
	} else {
		match, matched, err = s.matchBlock(view, interrupt)
	}
	if err != nil || !matched {
		return "", false, err
	}
	// Copy the classification before restoring the context's matched construct.
	_, native := blockNativeMatch(match.roots)
	kind := ""
	if native != nil {
		kind = native.kind
	}
	return kind, true, nil
}

func (s *parseState) matchParagraphContinuation(view sourceView) (blockExtensionMatch, bool, error) {
	match, matched, err := s.matchBlock(view, true)
	if matched || err != nil {
		return match, matched, err
	}
	kind, matched, err := s.previewBlockKind(view, false, false)
	if err != nil || !matched || kind != "setextHeadingLine" && kind != "table" {
		return blockExtensionMatch{}, false, err
	}
	return s.matchBlock(view, false)
}

// Non-concrete flow can be interrupted by a new document container. Concrete
// flow receives those same bytes intact; the block driver never opens a
// container inside its token span. Both paths execute the construct once.
func (s *parseState) blockFlowView(view sourceView, index int) (sourceView, error) {
	lines := sourceLines(view.slice(index, len(view.text)))
	if len(lines) < 2 {
		return view, nil
	}
	cursor := index + len(lines[0].full().text)
	for _, line := range lines[1:] {
		starts, err := s.blockPreview(line.full(), true, true)
		if err != nil {
			return sourceView{}, err
		}
		if starts {
			return view.slice(0, cursor), nil
		}
		cursor += len(line.full().text)
	}
	return view, nil
}

func blockConsumeExtension(lines []sourceLine, index int, view sourceView, end int, stack []*blockFrame) (int, sourceLine, sourceLine, bool, error) {
	consumed := sourceLines(view.slice(0, end))
	count := len(consumed)
	if count == 0 {
		count = 1
	}
	for j := 1; j < count; j++ {
		if _, _, err := blockContainerLine(stack, lines[index+j], true); err != nil {
			return index, sourceLine{}, sourceLine{}, false, err
		}
	}
	index += count - 1
	original := lines[index]
	last := consumed[len(consumed)-1]
	if len(last.ending.text) > 0 {
		return index, original, sourceLine{}, false, nil
	}
	physical := sourceLines(view)
	line := physical[count-1]
	line.body = line.body.slice(len(last.body.text), len(line.body.text))
	return index, original, line, true, nil
}

func (s *parseState) parseBlocks(lines []sourceLine, parent *Node) error {
	d := &blockDriver{s: s, parent: parent, stack: []*blockFrame{{node: parent, kind: parent.Kind}}}
	return d.run(lines, false)
}

func blockUnmatchedLine(line sourceLine) *blockLeaf {
	// Disabling paragraph syntax does not discard ordinary source. This final
	// data fallback performs layout only; it does not recognize block grammar.
	body, prefix := removeBlockIndent(line.body, 3)
	n := makeNode(KindParagraph, "paragraph", line.full())
	if len(prefix.text) > 0 {
		recordBlockPrefix(n.token, makeToken("linePrefix", prefix))
	}
	return &blockLeaf{node: n, kind: KindParagraph, lines: []sourceLine{{body: body, ending: line.ending}}, raw: []sourceView{line.full()}}
}

func blockFirstParagraph(parent *Node) bool {
	for _, child := range parent.Children {
		if child.Kind != KindDefinition {
			return false
		}
	}
	return true
}

func blockRemainingViews(lines []sourceLine) []sourceView {
	views := make([]sourceView, 0, len(lines))
	for _, line := range lines {
		views = append(views, line.full())
	}
	return views
}

func (s *parseState) finishBlockParagraph(l *blockLeaf, parent *Node) error {
	s.blockOwner = parent.token
	lines := l.lines
	raw := l.raw
	for len(lines) > 0 {
		view := linesView(lines)
		end, roots, ok, err := s.trySyntax(PhaseContentInitial, view, 0, false, false)
		if err != nil {
			return err
		}
		if !ok {
			end, roots, ok, err = s.trySyntax(PhaseContentInitial, view, 0, true, false)
			if err != nil {
				return err
			}
		}
		if !ok || end == 0 {
			break
		}
		_, native := blockNativeMatch(roots)
		if native != nil && native.kind == "taskListCheck" {
			break
		}
		consumed := sourceLines(view.slice(0, end))
		count := len(consumed)
		if native != nil && native.kind == "definition" {
			n := roots[0].node
			if count <= len(raw) {
				setBlockView(n, raw[:count], n.ending)
			}
			if _, exists := s.doc.Definitions[n.Identifier]; !exists {
				s.doc.Definitions[n.Identifier] = Definition{Destination: n.Destination, Title: n.Title, Node: n}
			}
		}
		if native != nil {
			blockCommitCapture(roots[0])
		}
		context := s.contextMap[syntaxKey{phase: PhaseContentInitial, owner: parent.token}]
		s.appendSyntax(parent, roots, context != nil && context.lastResolveTo)
		if len(roots) > 0 {
			blockMoveParagraphPrefixes(l.node.token, roots[len(roots)-1], view.slice(0, end).span().End.ByteOffset)
		}
		lines = sourceLines(view.slice(end, len(view.text)))
		// Content prefixes may stop within a physical line. Only complete
		// lines advance the original raw views; the suffix owns the rest.
		completed := count
		partial := end < len(view.text) && len(consumed[count-1].ending.text) == 0
		if partial {
			completed--
		}
		if completed <= len(raw) {
			raw = raw[completed:]
			if partial {
				raw[0] = lines[0].full()
			}
		} else {
			raw = blockRemainingViews(lines)
		}
		if partial && blockBlank(lines[0].body.text) {
			// An empty suffix is flow trivia, not an empty paragraph.
			if len(lines[0].body.text) > 0 {
				parent.token.append(makeToken("whitespace", lines[0].body))
			}
			if len(lines[0].ending.text) > 0 {
				parent.token.append(makeToken("lineEnding", lines[0].ending))
			}
			lines, raw = lines[1:], raw[1:]
		}
	}
	if len(lines) == 0 {
		return nil
	}
	n := l.node
	view := blockContentView(lines)
	view, leading, trailing := blockTrim(view)
	n.input = view
	if len(leading.text) > 0 {
		n.token.append(makeToken("whitespace", leading))
	}
	if n.Kind == KindParagraph && parent.Kind == KindListItem && blockFirstParagraph(parent) {
		end, roots, ok, err := s.trySyntax(PhaseContentInitial, view, 0, false, false)
		if err != nil {
			return err
		}
		_, native := blockNativeMatch(roots)
		if ok && native != nil && native.kind == "taskListCheck" {
			token := roots[0]
			checked, _ := token.Data["checked"].(bool)
			parent.Task = true
			parent.Checked = checked
			n.Task = true
			n.Checked = checked
			marker := blockCaptured(token, "taskListCheck")
			if marker != nil {
				marker.node = n
			}
			for _, child := range token.Children {
				n.token.append(child)
			}
			n.input = view.slice(end, len(view.text))
		}
	}
	n.token.append(makeToken("chunkText", n.input))
	if len(trailing.text) > 0 {
		n.token.append(makeToken("whitespace", trailing))
	}
	l.raw = raw
	setBlockView(n, raw, lines[len(lines)-1].ending)
	appendNode(parent, n)
	if ending := lines[len(lines)-1].ending; len(ending.text) > 0 {
		token := makeToken("lineEnding", ending)
		token.context = n.token.context
		if n.Kind == KindHeading {
			// The text-to-underline ending is heading syntax, not inline content.
			n.token.append(token)
		} else {
			parent.token.append(token)
		}
	}
	return nil
}

func (s *parseState) parseBlockDefinition(lines []sourceLine) (*Node, int, bool, error) {
	view := linesView(lines)
	end, roots, ok, err := s.trySyntax(PhaseContentInitial, view, 0, false, false)
	if err != nil {
		return nil, 0, false, err
	}
	_, native := blockNativeMatch(roots)
	if !ok || native == nil || native.kind != "definition" {
		return nil, 0, false, nil
	}
	return roots[0].node, len(sourceLines(view.slice(0, end))), true, nil
}

func (s *parseState) blockParagraphOpen(leaf *blockLeaf) (bool, error) {
	lines := leaf.lines
	for len(lines) > 0 {
		_, count, ok, err := s.parseBlockDefinition(lines)
		if err != nil {
			return false, err
		}
		if !ok {
			break
		}
		lines = lines[count:]
	}
	return len(lines) > 0, nil
}

func blockUndoFrameLine(frame *blockFrame, line sourceLine) {
	if len(frame.raw) == 0 || frame.raw[len(frame.raw)-1].span().End != line.full().span().End {
		return
	}
	frame.raw = frame.raw[:len(frame.raw)-1]
	frame.ending = sourceView{}
	if len(frame.raw) > 0 {
		physical := sourceLines(frame.raw[len(frame.raw)-1])
		if len(physical) > 0 {
			frame.ending = physical[len(physical)-1].ending
		}
	}
}

func blockMoveParagraphPrefixes(from, to *Token, end int) {
	kept := from.Children[:0]
	for _, token := range from.Children {
		if token.Range.Start.ByteOffset < end {
			to.append(token)
		} else {
			kept = append(kept, token)
		}
	}
	from.Children = kept
}
