package core

import "strings"

// blockDriver owns the resumable state of block parsing. parseBlocks feeds all
// lines at once; streaming feeds complete lines and keeps a one-line lookahead.
type blockDriver struct {
	s      *parseState
	parent *Node
	stack  []*blockFrame
	leaf   *blockLeaf
	next   int
}

type blockDriverLine struct {
	index      int
	original   sourceLine
	line       sourceLine
	opening    blockExtensionMatch
	hasOpening bool
}

type blockOpenResult uint8

const (
	blockOpenDone blockOpenResult = iota
	blockOpenContinue
	blockOpenPending
)

func (d *blockDriver) current() *Node { return d.stack[len(d.stack)-1].node }

func (d *blockDriver) finishLeaf() error {
	s := d.s
	if d.leaf == nil {
		return nil
	}
	l := d.leaf
	d.leaf = nil
	if len(l.lines) == 0 && l.kind == KindParagraph {
		return nil
	}
	ending := sourceView{}
	if len(l.raw) > 0 {
		physical := sourceLines(l.raw[len(l.raw)-1])
		if len(physical) > 0 {
			ending = physical[len(physical)-1].ending
		}
	}
	setBlockView(l.node, l.raw, ending)
	if l.kind == KindParagraph {
		return s.finishBlockParagraph(l, d.current())
	}
	l.node.Literal = linesView(l.lines).text
	return nil
}

func (d *blockDriver) closeTo(count int) error {
	for len(d.stack) > count {
		f := d.stack[len(d.stack)-1]
		if f.context != nil {
			if err := blockCloseDocument(f); err != nil {
				return err
			}
		} else {
			setBlockView(f.node, f.raw, f.ending)
		}
		if f.kind == KindList {
			f.node.Tight = !f.loose
		}
		if f.kind == KindListItem && f.blank && len(d.stack) > 2 {
			d.stack[len(d.stack)-2].blank = true
		}
		if (f.kind == KindList || f.kind == KindFootnoteDefinition) && f.blank && len(d.stack) > 2 && d.stack[len(d.stack)-2].kind == KindListItem {
			d.stack[len(d.stack)-2].blank = true
		}
		d.stack = d.stack[:len(d.stack)-1]
	}
	return nil
}

func (d *blockDriver) markChild() {
	if d.stack[len(d.stack)-1].kind == KindFootnoteDefinition {
		// Only an unconsumed blank escapes a definition when it closes.
		d.stack[len(d.stack)-1].blank = false
	}
	for j := len(d.stack) - 1; j > 0; j-- {
		if d.stack[j].kind == KindListItem {
			if j == len(d.stack)-1 && d.stack[j].blank && len(d.stack[j].node.Children) > 0 {
				d.stack[j-1].loose = true
			}
			d.stack[j].blank = false
			d.stack[j-1].blank = false
			break
		}
	}
}

func (d *blockDriver) feed(lines []sourceLine, closed bool) error {
	return d.run(lines, !closed)
}

func (d *blockDriver) canProcessLast(line sourceLine) bool {
	if d.leaf != nil {
		switch d.leaf.kind {
		case KindCodeBlock, KindMathBlock, KindHTMLBlock:
			return true
		}
		return false
	}
	return streamLastLineDelimited(line)
}

func streamLastLineDelimited(line sourceLine) bool {
	if blockBlank(line.body.text) {
		return true
	}
	body := line.body.text
	for trim := 0; trim < len(body) && trim < 3 && body[trim] == ' '; trim++ {
		body = body[1:]
	}
	if strings.HasPrefix(body, "#") || strings.HasPrefix(body, "```") || strings.HasPrefix(body, "~~~") {
		return true
	}
	_, _, ok, err := blockRuleMatch(line.full(), blockThematicRule())
	return err == nil && ok
}

func (d *blockDriver) restoreLineRaws(lengths []int, endings []sourceView) {
	count := len(d.stack)
	if count > len(lengths) {
		count = len(lengths)
	}
	for index := range count {
		d.stack[index].raw = d.stack[index].raw[:lengths[index]]
		d.stack[index].ending = endings[index]
	}
}

func (d *blockDriver) tableTerminated(lines []sourceLine, index int, view sourceView, end int) (bool, error) {
	consumed := sourceLines(view.slice(0, end))
	count := len(consumed)
	if count == 0 {
		count = 1
	}
	for next := index + count; next < len(lines); next++ {
		line := lines[next]
		if blockBlank(line.body.text) {
			return true, nil
		}
		interrupt, err := d.s.blockStarts(line.body, false)
		if err != nil {
			return false, err
		}
		if interrupt {
			return true, nil
		}
		_, indent := blockIndent(line.body)
		if indent >= 4 && d.s.enabled("codeIndented") {
			return true, nil
		}
		_, _, ok, err := blockRuleMatch(line.full(), tableBodyRowSyntax)
		if err != nil {
			return false, err
		}
		if !ok {
			return true, nil
		}
	}
	return false, nil
}

func (d *blockDriver) run(lines []sourceLine, streaming bool) error {
	s := d.s
	oldOwner, oldParagraph := s.blockOwner, s.blockParagraph
	s.blockOwner = d.parent.token
	s.blockParagraph = false
	defer func() { s.blockOwner = oldOwner; s.blockParagraph = oldParagraph }()
	for lineIndex := d.next; lineIndex < len(lines); lineIndex++ {
		if streaming && lineIndex+1 >= len(lines) && !d.canProcessLast(lines[lineIndex]) {
			d.next = lineIndex
			return nil
		}
		state := blockDriverLine{index: lineIndex, original: lines[lineIndex], line: lines[lineIndex]}
		var rawLens []int
		var rawEndings []sourceView
		if streaming {
			rawLens = make([]int, len(d.stack))
			rawEndings = make([]sourceView, len(d.stack))
			for index := range d.stack {
				rawLens[index] = len(d.stack[index].raw)
				rawEndings[index] = d.stack[index].ending
			}
		}
		s.blockParagraph = false
		consumed, err := d.continueContainers(&state)
		if err != nil {
			return err
		}
		if consumed {
			continue
		}
		s.blockOwner = d.current().token
		consumed, err = d.continueLeaf(&state, lines)
		if err != nil {
			return err
		}
		if consumed {
			continue
		}
		if blockBlank(state.line.body.text) {
			if err := d.consumeBlankLine(&state); err != nil {
				return err
			}
			continue
		}
		result, err := d.openBlocks(&state, lines, streaming)
		if err != nil {
			return err
		}
		if result == blockOpenPending {
			d.restoreLineRaws(rawLens, rawEndings)
			d.next = state.index
			return nil
		}
		lineIndex = state.index
		d.syncListBlanks()
	}
	d.next = len(lines)
	if streaming {
		return nil
	}
	if err := d.finishLeaf(); err != nil {
		return err
	}
	return d.closeTo(1)
}

func (d *blockDriver) continueContainers(state *blockDriverLine) (bool, error) {
	matched := 1
	for j := 1; j < len(d.stack); j++ {
		f := d.stack[j]
		before := state.line.full()
		var ok bool
		var err error
		state.line, ok, err = blockContinueFrame(f, state.line, true)
		if err != nil {
			return false, err
		}
		if !ok {
			break
		}
		f.raw = append(f.raw, before)
		f.ending = state.original.ending
		matched = j + 1
	}
	if matched == len(d.stack) {
		return false, nil
	}
	s := d.s
	s.blockOwner = d.stack[matched-1].node.token
	interrupt, err := s.blockInterrupt(state.line.body)
	if err != nil {
		return false, err
	}
	open := false
	if d.leaf != nil && d.leaf.kind == KindParagraph && !interrupt {
		open, err = s.blockParagraphOpen(d.leaf)
		if err != nil {
			return false, err
		}
	}
	if open {
		for j := matched; j < len(d.stack); j++ {
			d.stack[j].raw = append(d.stack[j].raw, state.line.full())
			d.stack[j].ending = state.original.ending
		}
		d.appendParagraphLine(&state.line)
		return true, nil
	}
	if err := d.finishLeaf(); err != nil {
		return false, err
	}
	return false, d.closeTo(matched)
}

func (d *blockDriver) continueLeaf(state *blockDriverLine, lines []sourceLine) (bool, error) {
	if d.leaf == nil {
		return false, nil
	}
	switch d.leaf.kind {
	case KindCodeBlock, KindMathBlock:
		return d.continueCode(&state.line)
	case KindHTMLBlock:
		return d.continueHTML(&state.line)
	case KindParagraph:
		return d.continueParagraph(state, lines)
	}
	return false, nil
}

func (d *blockDriver) continueCode(line *sourceLine) (bool, error) {
	l := d.leaf
	if l.fence != 0 {
		closing, _, ok, err := blockRuleMatch(line.full(), l.closing)
		if err != nil {
			return false, err
		}
		if ok {
			l.raw = append(l.raw, line.full())
			l.node.token.append(closing)
			l.node.Data["closed"] = true
			return true, d.finishLeaf()
		}
		body, prefix := removeBlockIndent(line.body, l.fenceIndent)
		if len(prefix.text) > 0 {
			recordBlockPrefix(l.node.token, makeToken("linePrefix", prefix))
		}
		l.lines = append(l.lines, sourceLine{body: body, ending: line.ending})
		l.raw = append(l.raw, line.full())
		kind := TokenType("codeFlowValue")
		if l.kind == KindMathBlock {
			kind = "mathFlowValue"
		}
		l.node.token.append(makeToken(kind, body))
		if len(line.ending.text) > 0 {
			l.node.token.append(makeToken("lineEnding", line.ending))
		}
		return true, nil
	}
	if blockBlank(line.body.text) {
		l.pending = append(l.pending, *line)
		return true, nil
	}
	_, indent := blockIndent(line.body)
	if indent >= 4 {
		for _, pending := range append(l.pending, *line) {
			body, prefix := removeBlockIndent(pending.body, 4)
			l.lines = append(l.lines, sourceLine{body: body, ending: pending.ending})
			l.raw = append(l.raw, pending.full())
			if len(prefix.text) > 0 {
				recordBlockPrefix(l.node.token, makeToken("linePrefix", prefix))
			}
			if len(body.text) > 0 {
				l.node.token.append(makeToken("codeFlowValue", body))
			}
			if len(pending.ending.text) > 0 {
				l.node.token.append(makeToken("lineEnding", pending.ending))
			}
		}
		l.pending = nil
		return true, nil
	}
	if len(l.pending) > 0 && d.stack[len(d.stack)-1].kind == KindListItem {
		d.stack[len(d.stack)-1].blank = true
		d.stack[len(d.stack)-2].blank = true
	}
	return false, d.finishLeaf()
}

func (d *blockDriver) continueHTML(line *sourceLine) (bool, error) {
	l := d.leaf
	if l.htmlClass >= 6 && blockBlank(line.body.text) {
		return false, d.finishLeaf()
	}
	l.lines = append(l.lines, *line)
	l.raw = append(l.raw, line.full())
	l.node.token.append(makeToken("htmlFlowData", line.body))
	if len(line.ending.text) > 0 {
		l.node.token.append(makeToken("lineEnding", line.ending))
	}
	if l.closing != nil {
		_, _, closed, err := blockRuleMatch(line.body, l.closing)
		if err != nil {
			return false, err
		}
		if closed {
			return true, d.finishLeaf()
		}
	}
	return true, nil
}

func (d *blockDriver) continueParagraph(state *blockDriverLine, lines []sourceLine) (bool, error) {
	l := d.leaf
	if blockBlank(state.line.body.text) {
		return false, d.finishLeaf()
	}
	s := d.s
	s.blockParagraph = true
	view, err := blockLogicalRemaining(state.line, lines[state.index+1:], d.stack)
	if err != nil {
		return false, err
	}
	match := &state.opening
	var ok bool
	*match, ok, err = s.matchParagraphContinuation(view)
	if err != nil {
		return false, err
	}
	s.blockParagraph = false
	_, native := blockNativeMatch(match.roots)
	if ok && native != nil && native.kind == "setextHeadingLine" {
		blockCommitCapture(match.roots[0])
		l.node.Kind = KindHeading
		l.node.Level = native.level
		l.node.token.Type = "setextHeading"
		if err := s.finishBlockParagraph(l, d.current()); err != nil {
			return false, err
		}
		d.leaf = nil
		if l.node.parent != nil {
			l.raw = append(l.raw, state.line.full())
			setBlockView(l.node, l.raw, state.line.ending)
			l.node.token.append(match.roots[0])
			d.current().token.append(match.roots[1:]...)
			return true, nil
		}
	} else if !ok {
		d.appendParagraphLine(&state.line)
		return true, nil
	} else {
		state.hasOpening = true
		if err := d.finishLeaf(); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (d *blockDriver) appendParagraphLine(line *sourceLine) {
	body, prefix := removeBlockIndent(line.body, 3)
	if len(prefix.text) > 0 {
		recordBlockPrefix(d.leaf.node.token, makeToken("linePrefix", prefix))
	}
	d.leaf.lines = append(d.leaf.lines, sourceLine{body: body, ending: line.ending})
	d.leaf.raw = append(d.leaf.raw, line.full())
}

func (d *blockDriver) consumeBlankLine(state *blockDriverLine) error {
	if d.stack[len(d.stack)-1].kind == KindFootnoteDefinition {
		d.stack[len(d.stack)-1].blank = true
	}
	if d.stack[len(d.stack)-1].kind == KindListItem {
		f := d.stack[len(d.stack)-1]
		f.blank = true
		d.stack[len(d.stack)-2].blank = true
		if len(f.node.Children) == 0 {
			blockUndoFrameLine(f, state.original)
			if err := d.closeTo(len(d.stack) - 1); err != nil {
				return err
			}
		}
	}
	d.appendLineEnding(&state.line, "lineEndingBlank")
	return nil
}

func (d *blockDriver) appendLineEnding(line *sourceLine, kind TokenType) {
	if len(line.body.text) > 0 {
		recordBlockPrefix(d.current().token, makeToken("linePrefix", line.body))
	}
	if len(line.ending.text) > 0 {
		d.current().token.append(makeToken(kind, line.ending))
	}
}

func (d *blockDriver) openBlocks(state *blockDriverLine, lines []sourceLine, streaming bool) (blockOpenResult, error) {
	s := d.s
	for {
		s.blockOwner = d.current().token
		if blockBlank(state.line.body.text) {
			d.appendLineEnding(&state.line, "lineEnding")
			return blockOpenDone, nil
		}
		if d.stack[len(d.stack)-1].kind == KindList {
			_, _, item, err := blockRuleMatch(state.line.full(), blockListInterruptRule)
			if err != nil {
				return blockOpenDone, err
			}
			if !item || !s.enabled("list") {
				if err := d.closeListLine(&state.original); err != nil {
					return blockOpenDone, err
				}
				continue
			}
		}
		view, err := blockLogicalRemaining(state.line, lines[state.index+1:], d.stack)
		if err != nil {
			return blockOpenDone, err
		}
		if !state.hasOpening && d.stack[len(d.stack)-1].kind == KindList {
			kind, matched, err := s.previewBlockKind(view, false, false)
			if err != nil {
				return blockOpenDone, err
			}
			if matched && kind != "" && kind != "listItemPrefix" {
				if err := d.closeListLine(&state.original); err != nil {
					return blockOpenDone, err
				}
				continue
			}
		}
		match := &state.opening
		var ok bool
		if state.hasOpening {
			state.hasOpening = false
			ok = true
		} else {
			*match, ok, err = s.matchBlock(view, false)
		}
		if err != nil {
			return blockOpenDone, err
		}
		if !ok {
			d.leaf = blockUnmatchedLine(state.line)
			return blockOpenDone, nil
		}
		token, native := blockNativeMatch(match.roots)
		var result blockOpenResult
		if native == nil {
			result, err = d.commitExtension(state, lines, view, match)
		} else {
			blockCommitCapture(token)
			if native.kind == "table" {
				return d.commitTableBlock(state, lines, view, token, match, streaming)
			}
			result, err = d.commitNativeBlock(state, token, native, match.roots)
		}
		if err != nil {
			return blockOpenDone, err
		}
		if result != blockOpenContinue {
			return result, nil
		}
	}
}

func (d *blockDriver) closeListLine(original *sourceLine) error {
	blockUndoFrameLine(d.stack[len(d.stack)-1], *original)
	return d.closeTo(len(d.stack) - 1)
}

func (d *blockDriver) commitExtension(state *blockDriverLine, lines []sourceLine, view sourceView, match *blockExtensionMatch) (blockOpenResult, error) {
	d.markChild()
	if match.context != nil {
		for _, open := range match.context.stack {
			match.context.Node(open.token)
			for _, prefix := range open.token.Children {
				isOpen := false
				for _, candidate := range match.context.stack {
					if candidate.token == prefix {
						isOpen = true
						break
					}
				}
				if !isOpen && prefix.node == nil {
					prefix.recordedMargin = true
				}
			}
		}
		// Link open CST wrappers from the inside out. Retain tokenizer-authored
		// semantic children; no source or semantic subtree is reparsed.
		for index := len(match.context.stack) - 1; index >= 0; index-- {
			open := match.context.stack[index].token
			for _, child := range open.Children {
				appendSemantic(open.node, child)
			}
		}
	}
	d.s.appendSyntax(d.current(), match.roots, match.replacesPrefix)
	if match.context != nil {
		context := match.context
		owner := context.stack[len(context.stack)-1].token
		n := semanticNode(owner)
		frame := &blockFrame{node: n, kind: n.Kind, context: context, construct: context.matched, raw: []sourceView{state.line.full()}, ending: state.original.ending}
		d.stack = append(d.stack, frame)
		state.line.body = view.slice(match.end, len(state.line.body.text))
		if context.virtual > 0 && match.end > 0 {
			state.line.body = joinViews([]sourceView{paddingView(context.virtual, view.slice(match.end-1, match.end)), state.line.body})
		}
		return blockOpenContinue, nil
	}
	var partial bool
	var err error
	state.index, state.original, state.line, partial, err = blockConsumeExtension(lines, state.index, view, match.end, d.stack)
	if err != nil {
		return blockOpenDone, err
	}
	if partial {
		return blockOpenContinue, nil
	}
	return blockOpenDone, nil
}

func (d *blockDriver) commitTableBlock(state *blockDriverLine, lines []sourceLine, view sourceView, token *Token, match *blockExtensionMatch, streaming bool) (blockOpenResult, error) {
	if streaming {
		ended, err := d.tableTerminated(lines, state.index, view, match.end)
		if err != nil {
			return blockOpenDone, err
		}
		if !ended {
			return blockOpenPending, nil
		}
	}
	d.markChild()
	count, err := d.s.parseBlockTableMatch(token, *match, view, lines[state.index:], d.current(), d.stack)
	if err != nil {
		return blockOpenDone, err
	}
	state.index += count - 1
	return blockOpenDone, nil
}

func (d *blockDriver) commitNativeBlock(state *blockDriverLine, token *Token, native *blockSyntaxMatch, roots []*Token) (blockOpenResult, error) {
	switch native.kind {
	case "blockQuotePrefix":
		d.markChild()
		n := makeNode(KindBlockQuote, "blockQuote", state.line.full())
		appendNode(d.current(), n)
		n.token.append(token)
		d.stack = append(d.stack, &blockFrame{node: n, kind: KindBlockQuote, raw: []sourceView{state.line.full()}, ending: state.original.ending})
		state.line.body = native.body
		return blockOpenContinue, nil
	case "listItemPrefix":
		return d.openListItem(state, token, native)
	case "footnoteDefinition":
		d.markChild()
		n := token.node
		appendNode(d.current(), n)
		d.s.registerBlockFootnote(n)
		d.stack = append(d.stack, &blockFrame{node: n, kind: KindFootnoteDefinition, indent: 4, raw: []sourceView{state.line.full()}, ending: state.original.ending})
		state.line.body = native.body
		if blockBlank(state.line.body.text) {
			return blockOpenDone, nil
		}
		return blockOpenContinue, nil
	case "paragraph":
		d.startParagraph(state, token)
	case "atxHeading":
		d.markChild()
		n := token.node
		n.ending = state.line.ending
		appendNode(d.current(), n)
		d.current().token.append(roots[1:]...)
	case "codeFenced", "mathFlow":
		d.markChild()
		n := token.node
		appendNode(d.current(), n)
		n.ending = state.line.ending
		if native.closed {
			setBlockView(n, []sourceView{state.line.full()}, state.line.ending)
		} else {
			d.leaf = &blockLeaf{node: n, kind: n.Kind, fence: native.fence, fenceIndent: native.indent, closing: blockFenceClosingRule(native.fence, native.fenceSize, n.Kind == KindMathBlock), raw: []sourceView{state.line.full()}}
		}
	case "codeIndented":
		d.markChild()
		n := token.node
		appendNode(d.current(), n)
		body := blockCaptured(token, "codeFlowValue").view
		d.leaf = &blockLeaf{node: n, kind: KindCodeBlock, lines: []sourceLine{{body: body, ending: state.line.ending}}, raw: []sourceView{state.line.full()}}
	case "htmlFlow":
		if err := d.startHTMLBlock(state, token, native); err != nil {
			return blockOpenDone, err
		}
	case "thematicBreak":
		d.markChild()
		n := &Node{Kind: KindThematicBreak, Range: token.Range, token: token, ending: state.line.ending}
		token.node = n
		appendNode(d.current(), n)
	}
	return blockOpenDone, nil
}

func (d *blockDriver) openListItem(state *blockDriverLine, token *Token, native *blockSyntaxMatch) (blockOpenResult, error) {
	m := native.marker
	if d.stack[len(d.stack)-1].kind == KindList {
		f := d.stack[len(d.stack)-1]
		if f.node.Ordered != m.ordered || f.marker != m.marker || f.delimiter != m.delimiter {
			blockUndoFrameLine(f, state.original)
			if err := d.closeTo(len(d.stack) - 1); err != nil {
				return blockOpenDone, err
			}
		}
	}
	if d.stack[len(d.stack)-1].kind != KindList {
		d.markChild()
		typ := TokenType("listUnordered")
		if m.ordered {
			typ = "listOrdered"
		}
		n := makeNode(KindList, typ, state.line.full())
		n.Ordered = m.ordered
		n.Start = m.start
		n.Tight = true
		appendNode(d.current(), n)
		d.stack = append(d.stack, &blockFrame{node: n, kind: KindList, marker: m.marker, delimiter: m.delimiter, raw: []sourceView{state.line.full()}, ending: state.original.ending})
	} else {
		f := d.stack[len(d.stack)-1]
		if f.blank {
			f.loose = true
		}
		f.blank = false
	}
	n := makeNode(KindListItem, "listItem", state.line.full())
	appendNode(d.current(), n)
	n.token.append(token)
	d.stack = append(d.stack, &blockFrame{node: n, kind: KindListItem, indent: m.indent, raw: []sourceView{state.line.full()}, ending: state.original.ending})
	state.line.body = native.body
	if blockBlank(state.line.body.text) {
		if len(state.line.ending.text) > 0 {
			n.token.append(makeToken("lineEnding", state.line.ending))
		}
		return blockOpenDone, nil
	}
	return blockOpenContinue, nil
}

func (d *blockDriver) startParagraph(state *blockDriverLine, token *Token) {
	d.markChild()
	n := token.node
	// Fill text only after leading definitions and physical continuations are known.
	children := n.token.Children[:0]
	for _, child := range n.token.Children {
		if child.Type != "chunkText" && child.Type != "lineEnding" {
			children = append(children, child)
		}
	}
	n.token.Children = children
	d.leaf = &blockLeaf{node: n, kind: KindParagraph, lines: []sourceLine{{body: n.input, ending: state.line.ending}}, raw: []sourceView{state.line.full()}}
}

func (d *blockDriver) startHTMLBlock(state *blockDriverLine, token *Token, native *blockSyntaxMatch) error {
	d.markChild()
	n := token.node
	appendNode(d.current(), n)
	d.leaf = &blockLeaf{node: n, kind: KindHTMLBlock, htmlClass: native.htmlClass, lines: []sourceLine{state.line}, raw: []sourceView{state.line.full()}}
	if native.htmlEnd != "" {
		d.leaf.closing = blockHTMLTerminatorRule(native.htmlClass, native.htmlEnd)
		_, _, closed, err := blockRuleMatch(state.line.body, d.leaf.closing)
		if err != nil {
			return err
		}
		if closed {
			return d.finishLeaf()
		}
	}
	return nil
}

func (d *blockDriver) syncListBlanks() {
	for j := 1; j < len(d.stack); j++ {
		if d.stack[j].kind == KindList && j+1 < len(d.stack) && d.stack[j+1].blank {
			d.stack[j].blank = true
		}
	}
}
