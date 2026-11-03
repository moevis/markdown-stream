package markdown

// blockDriver owns the resumable state of block parsing. parseBlocks currently
// feeds all lines at once; streaming can feed complete lines incrementally.
type blockDriver struct {
	s      *parseState
	parent *Node
	stack  []*blockFrame
	leaf   *blockLeaf
}

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

func (d *blockDriver) run(lines []sourceLine) error {
	s := d.s
	oldOwner, oldParagraph := s.blockOwner, s.blockParagraph
	s.blockOwner = d.parent.token
	s.blockParagraph = false
	defer func() { s.blockOwner = oldOwner; s.blockParagraph = oldParagraph }()
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		original := lines[lineIndex]
		line := original
		matched := 1
		var opening *blockExtensionMatch
		s.blockParagraph = false
		for j := 1; j < len(d.stack); j++ {
			f := d.stack[j]
			before := line.full()
			var ok bool
			var err error
			line, ok, err = blockContinueFrame(f, line, true)
			if err != nil {
				return err
			}
			if !ok {
				goto matchedContainers
			}
			f.raw = append(f.raw, before)
			f.ending = original.ending
			matched = j + 1
		}
	matchedContainers:
		if matched < len(d.stack) {
			s.blockOwner = d.stack[matched-1].node.token
			interrupt, err := s.blockInterrupt(line.body)
			if err != nil {
				return err
			}
			open := false
			if d.leaf != nil && d.leaf.kind == KindParagraph && !interrupt {
				open, err = s.blockParagraphOpen(d.leaf)
				if err != nil {
					return err
				}
			}
			if open {
				for j := matched; j < len(d.stack); j++ {
					d.stack[j].raw = append(d.stack[j].raw, line.full())
					d.stack[j].ending = original.ending
				}
				body, prefix := removeBlockIndent(line.body, 3)
				if len(prefix.text) > 0 {
					recordBlockPrefix(d.leaf.node.token, makeToken("linePrefix", prefix))
				}
				d.leaf.lines = append(d.leaf.lines, sourceLine{body: body, ending: line.ending})
				d.leaf.raw = append(d.leaf.raw, line.full())
				continue
			}
			if err := d.finishLeaf(); err != nil {
				return err
			}
			if err := d.closeTo(matched); err != nil {
				return err
			}
		}
		s.blockOwner = d.current().token
		if d.leaf != nil {
			l := d.leaf
			switch l.kind {
			case KindCodeBlock, KindMathBlock:
				if l.fence != 0 {
					closing, _, ok, err := blockRuleMatch(line.full(), l.closing)
					if err != nil {
						return err
					}
					if ok {
						l.raw = append(l.raw, line.full())
						l.node.token.append(closing)
						l.node.Data["closed"] = true
						if err := d.finishLeaf(); err != nil {
							return err
						}
						continue
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
					continue
				}
				if blockBlank(line.body.text) {
					l.pending = append(l.pending, line)
					continue
				}
				_, indent := blockIndent(line.body)
				if indent >= 4 {
					for _, pending := range append(l.pending, line) {
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
					continue
				}
				if len(l.pending) > 0 && d.stack[len(d.stack)-1].kind == KindListItem {
					d.stack[len(d.stack)-1].blank = true
					d.stack[len(d.stack)-2].blank = true
				}
				if err := d.finishLeaf(); err != nil {
					return err
				}
			case KindHTMLBlock:
				if l.htmlClass >= 6 && blockBlank(line.body.text) {
					if err := d.finishLeaf(); err != nil {
						return err
					}
				} else {
					l.lines = append(l.lines, line)
					l.raw = append(l.raw, line.full())
					l.node.token.append(makeToken("htmlFlowData", line.body))
					if len(line.ending.text) > 0 {
						l.node.token.append(makeToken("lineEnding", line.ending))
					}
					if l.closing != nil {
						_, _, closed, err := blockRuleMatch(line.body, l.closing)
						if err != nil {
							return err
						}
						if closed {
							if err := d.finishLeaf(); err != nil {
								return err
							}
						}
					}
					continue
				}
			case KindParagraph:
				if blockBlank(line.body.text) {
					if err := d.finishLeaf(); err != nil {
						return err
					}
					break
				}
				s.blockParagraph = true
				view, err := blockLogicalRemaining(line, lines[lineIndex+1:], d.stack)
				if err != nil {
					return err
				}
				match, ok, err := s.matchParagraphContinuation(view)
				if err != nil {
					return err
				}
				s.blockParagraph = false
				_, native := blockNativeMatch(match.roots)
				if ok && native != nil && native.kind == "setextHeadingLine" {
					blockCommitCapture(match.roots[0])
					l.node.Kind = KindHeading
					l.node.Level = native.level
					l.node.token.Type = "setextHeading"
					if err := s.finishBlockParagraph(l, d.current()); err != nil {
						return err
					}
					d.leaf = nil
					if l.node.parent != nil {
						l.raw = append(l.raw, line.full())
						setBlockView(l.node, l.raw, line.ending)
						l.node.token.append(match.roots[0])
						d.current().token.append(match.roots[1:]...)
						continue
					}
				} else if !ok {
					body, prefix := removeBlockIndent(line.body, 3)
					if len(prefix.text) > 0 {
						recordBlockPrefix(l.node.token, makeToken("linePrefix", prefix))
					}
					l.lines = append(l.lines, sourceLine{body: body, ending: line.ending})
					l.raw = append(l.raw, line.full())
					continue
				} else {
					opening = &match
					if err := d.finishLeaf(); err != nil {
						return err
					}
				}
			}
		}
		if blockBlank(line.body.text) {
			if d.stack[len(d.stack)-1].kind == KindFootnoteDefinition {
				d.stack[len(d.stack)-1].blank = true
			}
			if d.stack[len(d.stack)-1].kind == KindListItem {
				f := d.stack[len(d.stack)-1]
				f.blank = true
				d.stack[len(d.stack)-2].blank = true
				if len(f.node.Children) == 0 {
					blockUndoFrameLine(f, original)
					if err := d.closeTo(len(d.stack) - 1); err != nil {
						return err
					}
				}
			}
			if len(line.body.text) > 0 {
				recordBlockPrefix(d.current().token, makeToken("linePrefix", line.body))
			}
			if len(line.ending.text) > 0 {
				d.current().token.append(makeToken("lineEndingBlank", line.ending))
			}
			continue
		}
		for {
			s.blockOwner = d.current().token
			if blockBlank(line.body.text) {
				if len(line.body.text) > 0 {
					recordBlockPrefix(d.current().token, makeToken("linePrefix", line.body))
				}
				if len(line.ending.text) > 0 {
					d.current().token.append(makeToken("lineEnding", line.ending))
				}
				break
			}
			if d.stack[len(d.stack)-1].kind == KindList {
				_, _, item, err := blockRuleMatch(line.full(), blockListInterruptRule)
				if err != nil {
					return err
				}
				if !item || !s.enabled("list") {
					blockUndoFrameLine(d.stack[len(d.stack)-1], original)
					if err := d.closeTo(len(d.stack) - 1); err != nil {
						return err
					}
					continue
				}
			}
			view, err := blockLogicalRemaining(line, lines[lineIndex+1:], d.stack)
			if err != nil {
				return err
			}
			if opening == nil && d.stack[len(d.stack)-1].kind == KindList {
				kind, matched, err := s.previewBlockKind(view, false, false)
				if err != nil {
					return err
				}
				if matched && kind != "" && kind != "listItemPrefix" {
					blockUndoFrameLine(d.stack[len(d.stack)-1], original)
					if err := d.closeTo(len(d.stack) - 1); err != nil {
						return err
					}
					continue
				}
			}
			var match blockExtensionMatch
			var ok bool
			if opening != nil {
				match = *opening
				opening = nil
				ok = true
			} else {
				match, ok, err = s.matchBlock(view, false)
			}
			if err != nil {
				return err
			}
			if !ok {
				d.leaf = blockUnmatchedLine(line)
				break
			}
			token, native := blockNativeMatch(match.roots)
			if native == nil {
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
					// Link every still-open CST wrapper before attaching child flow
					// to the innermost owner. Retain tokenizer-authored semantic
					// children; no source or semantic subtree is reparsed.
					for index := len(match.context.stack) - 1; index >= 0; index-- {
						open := match.context.stack[index].token
						for _, child := range open.Children {
							appendSemantic(open.node, child)
						}
					}
				}
				s.appendSyntax(d.current(), match.roots, match.replacesPrefix)
				if match.context != nil {
					context := match.context
					owner := context.stack[len(context.stack)-1].token
					n := semanticNode(owner)
					frame := &blockFrame{node: n, kind: n.Kind, context: context, construct: context.matched, raw: []sourceView{line.full()}, ending: original.ending}
					d.stack = append(d.stack, frame)
					line.body = view.slice(match.end, len(line.body.text))
					if context.virtual > 0 && match.end > 0 {
						line.body = joinViews([]sourceView{paddingView(context.virtual, view.slice(match.end-1, match.end)), line.body})
					}
					continue
				}
				var partial bool
				lineIndex, original, line, partial, err = blockConsumeExtension(lines, lineIndex, view, match.end, d.stack)
				if err != nil {
					return err
				}
				if partial {
					continue
				}
				break
			}
			blockCommitCapture(token)
			switch native.kind {
			case "blockQuotePrefix":
				d.markChild()
				n := makeNode(KindBlockQuote, "blockQuote", line.full())
				appendNode(d.current(), n)
				n.token.append(token)
				d.stack = append(d.stack, &blockFrame{node: n, kind: KindBlockQuote, raw: []sourceView{line.full()}, ending: original.ending})
				line.body = native.body
				continue
			case "listItemPrefix":
				m := native.marker
				if d.stack[len(d.stack)-1].kind == KindList {
					f := d.stack[len(d.stack)-1]
					if f.node.Ordered != m.ordered || f.marker != m.marker || f.delimiter != m.delimiter {
						blockUndoFrameLine(f, original)
						if err := d.closeTo(len(d.stack) - 1); err != nil {
							return err
						}
					}
				}
				if d.stack[len(d.stack)-1].kind != KindList {
					d.markChild()
					typ := TokenType("listUnordered")
					if m.ordered {
						typ = "listOrdered"
					}
					n := makeNode(KindList, typ, line.full())
					n.Ordered = m.ordered
					n.Start = m.start
					n.Tight = true
					appendNode(d.current(), n)
					d.stack = append(d.stack, &blockFrame{node: n, kind: KindList, marker: m.marker, delimiter: m.delimiter, raw: []sourceView{line.full()}, ending: original.ending})
				} else {
					f := d.stack[len(d.stack)-1]
					if f.blank {
						f.loose = true
					}
					f.blank = false
				}
				n := makeNode(KindListItem, "listItem", line.full())
				appendNode(d.current(), n)
				n.token.append(token)
				d.stack = append(d.stack, &blockFrame{node: n, kind: KindListItem, indent: m.indent, raw: []sourceView{line.full()}, ending: original.ending})
				line.body = native.body
				if blockBlank(line.body.text) {
					if len(line.ending.text) > 0 {
						n.token.append(makeToken("lineEnding", line.ending))
					}
					break
				}
				continue
			case "footnoteDefinition":
				d.markChild()
				n := token.node
				appendNode(d.current(), n)
				s.registerBlockFootnote(n)
				d.stack = append(d.stack, &blockFrame{node: n, kind: KindFootnoteDefinition, indent: 4, raw: []sourceView{line.full()}, ending: original.ending})
				line.body = native.body
				if blockBlank(line.body.text) {
					break
				}
				continue
			case "table":
				d.markChild()
				count, err := s.parseBlockTableMatch(token, match, view, lines[lineIndex:], d.current(), d.stack)
				if err != nil {
					return err
				}
				lineIndex += count - 1
			case "paragraph":
				d.markChild()
				n := token.node
				// Its text placeholder is filled only after leading definitions and
				// all physical paragraph continuations are known.
				children := n.token.Children[:0]
				for _, child := range n.token.Children {
					if child.Type != "chunkText" && child.Type != "lineEnding" {
						children = append(children, child)
					}
				}
				n.token.Children = children
				d.leaf = &blockLeaf{node: n, kind: KindParagraph, lines: []sourceLine{{body: n.input, ending: line.ending}}, raw: []sourceView{line.full()}}
			case "atxHeading":
				d.markChild()
				n := token.node
				n.ending = line.ending
				appendNode(d.current(), n)
				d.current().token.append(match.roots[1:]...)
			case "codeFenced", "mathFlow":
				d.markChild()
				n := token.node
				appendNode(d.current(), n)
				n.ending = line.ending
				if native.closed {
					setBlockView(n, []sourceView{line.full()}, line.ending)
				} else {
					d.leaf = &blockLeaf{node: n, kind: n.Kind, fence: native.fence, fenceIndent: native.indent, closing: blockFenceClosingRule(native.fence, native.fenceSize, n.Kind == KindMathBlock), raw: []sourceView{line.full()}}
				}
			case "codeIndented":
				d.markChild()
				n := token.node
				appendNode(d.current(), n)
				body := blockCaptured(token, "codeFlowValue").view
				d.leaf = &blockLeaf{node: n, kind: KindCodeBlock, lines: []sourceLine{{body: body, ending: line.ending}}, raw: []sourceView{line.full()}}
			case "htmlFlow":
				d.markChild()
				n := token.node
				appendNode(d.current(), n)
				d.leaf = &blockLeaf{node: n, kind: KindHTMLBlock, htmlClass: native.htmlClass, lines: []sourceLine{line}, raw: []sourceView{line.full()}}
				if native.htmlEnd != "" {
					d.leaf.closing = blockHTMLTerminatorRule(native.htmlClass, native.htmlEnd)
					_, _, closed, err := blockRuleMatch(line.body, d.leaf.closing)
					if err != nil {
						return err
					}
					if closed {
						if err := d.finishLeaf(); err != nil {
							return err
						}
					}
				}
			case "thematicBreak":
				d.markChild()
				n := &Node{Kind: KindThematicBreak, Range: token.Range, token: token, ending: line.ending}
				token.node = n
				appendNode(d.current(), n)
			}
			break
		}
		for j := 1; j < len(d.stack); j++ {
			if d.stack[j].kind == KindList && j+1 < len(d.stack) && d.stack[j+1].blank {
				d.stack[j].blank = true
			}
		}
	}
	if err := d.finishLeaf(); err != nil {
		return err
	}
	return d.closeTo(1)
}
