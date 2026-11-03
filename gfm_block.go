package markdown

// Cell boundaries are grammar captures. Escaped pipes belong to a cell even
// inside code spans; inline parsing therefore never decides row structure.
func tableCellRule(kind TokenType) Rule {
	atom := Or(Sequence(String("\\"), Runes("\\|")), Satisfy(func(code Code) bool { return code != '|' && !blockEndingCode(code) }))
	return Capture(kind, Many(atom))
}

func tableRowRule(kind TokenType) Rule {
	divider := Capture("tableCellDivider", String("|"))
	cell := tableCellRule("tableCellSource")
	return Capture(kind, Sequence(
		blockPrefixRule, Optional(divider), SepBy(cell, divider, 1, 0),
		blockLineEndRule, blockEndingRule("lineEnding"),
	))
}

func tableDelimiterRule() Rule {
	marker := OptionalCapture("tableDelimiterMarker", String(":"))
	cell := Capture("tableDelimiterCell", Sequence(
		blockWhitespaceRule, marker,
		Capture("tableDelimiterFiller", Some(String("-"))),
		marker, blockWhitespaceRule,
	))
	divider := Capture("tableCellDivider", String("|"))
	more := Sequence(divider, Not(Sequence(blockSpacesRule, blockLineEndRule)), cell)
	start := Guard(canStartTableDelimiter)
	return Capture("tableDelimiterRow", Sequence(
		start,
		Capture("linePrefix", blockWhitespaceRule), Optional(divider), cell,
		Many(more), Optional(divider), blockWhitespaceRule,
		blockLineEndRule, blockEndingRule("lineEnding"),
	))
}

func tableSourceCells(row *Token) []*Token {
	cells := blockCapturedAll(row, "tableCellSource", nil)
	// The optional leading divider does not manufacture an empty semantic cell.
	// A trailing divider does capture an empty tail, which is concrete only.
	if len(cells) > 1 && blockBlank(cells[len(cells)-1].view.text) {
		children := row.Children
		for i := len(children) - 1; i >= 0; i-- {
			if children[i].Type == "lineEnding" {
				continue
			}
			if children[i].Type == "tableCellSource" && blockBlank(children[i].view.text) && i > 0 && children[i-1].Type == "tableCellDivider" {
				cells = cells[:len(cells)-1]
			}
			break
		}
	}
	return cells
}

func canStartTableDelimiter(context *TokenizeContext) bool {
	view := blockRemaining(context)
	_, indent := blockIndent(view)
	if context.parser.enabled("codeIndented") && indent >= 4 {
		return false
	}
	interrupt, err := context.parser.blockStarts(view, true)
	if err != nil {
		context.Fail(err)
	}
	return !interrupt && err == nil
}

func canStartTable(context *TokenizeContext) bool {
	_, indent := blockIndent(context.view.slice(context.index, len(context.view.text)))
	return !context.Interrupt && indent < 4
}

func tableDelimiterContinuation(*TokenizeContext) Rule {
	return Sequence(tableDelimiterRule(), Guard(validTableHeader))
}

func validTableHeader(context *TokenizeContext) bool {
	root := context.stack[len(context.stack)-1].token
	header := blockCaptured(root, "tableHeaderRow")
	delimiter := blockCaptured(root, "tableDelimiterRow")
	cells := tableSourceCells(header)
	if len(cells) != len(blockCapturedAll(delimiter, "tableDelimiterCell", nil)) {
		return false
	}
	// A lone pipe (ignoring layout) is not a header. Any content,
	// or two pipes enclosing an empty cell, is a valid header.
	meaningful := len(blockCapturedAll(header, "tableCellDivider", nil)) > 1
	for _, cell := range cells {
		if !blockBlank(cell.view.text) {
			meaningful = true
		}
	}
	return meaningful && (blockCaptured(delimiter, "tableCellDivider") != nil || blockCaptured(delimiter, "tableDelimiterMarker") != nil)
}

func tableAction(context *TokenizeContext, token *Token, match *blockSyntaxMatch) {
	delimiter := blockCaptured(token, "tableDelimiterRow")
	for _, cell := range blockCapturedAll(delimiter, "tableDelimiterCell", nil) {
		markers := blockCapturedAll(cell, "tableDelimiterMarker", nil)
		alignment := AlignNone
		if len(markers) == 2 {
			alignment = AlignCenter
		} else if len(markers) == 1 {
			filler := blockCaptured(cell, "tableDelimiterFiller")
			if markers[0].Range.Start.ByteOffset < filler.Range.Start.ByteOffset {
				alignment = AlignLeft
			} else {
				alignment = AlignRight
			}
		}
		match.align = append(match.align, alignment)
	}
	n := context.Node(token)
	n.Kind, n.Alignments = KindTable, match.align
}

func tableRule() Rule {
	header := Sequence(Guard(canStartTable), tableRowRule("tableHeaderRow"))
	delimiter := Bind(header, tableDelimiterContinuation)
	return blockNative("table", delimiter, tableAction)
}

func footnoteDefinitionRule() Rule {
	labelAtom := Or(
		Sequence(String("\\"), Runes("[]\\")),
		Satisfy(func(code Code) bool { return code != '[' && code != ']' && !inlineCodeWhitespace(code) }),
	)
	label := Capture("footnoteDefinitionLabel", Some(labelAtom), Content(ContentString))
	prefix := Capture("footnoteDefinitionPrefix", Sequence(
		Capture("footnoteDefinitionMarker", String("[^")),
		Bind(label, footnoteDefinitionLabelContinuation),
		Capture("footnoteDefinitionMarker", String("]:")),
	))
	return blockNative("footnoteDefinition", Sequence(blockPrefixRule, prefix, blockWhitespaceRule), footnoteDefinitionAction)
}

func taskListRule() Rule {
	unchecked := Satisfy(func(code Code) bool { return blockSpaceCode(code) || blockEndingCode(code) })
	separator := Or(
		Satisfy(blockEndingCode),
		Sequence(Lookahead(Sequence(blockWhitespaceRule, Not(End()))), Or(String(" "), String("\t"))),
	)
	value := Or(
		Capture("taskListCheckValueChecked", Runes("xX")),
		Capture("taskListCheckValueUnchecked", unchecked),
	)
	firstParagraph := Guard(canStartTaskListCheck)
	check := Capture("taskListCheck", Sequence(
		Capture("taskListCheckMarker", String("[")), value,
		Capture("taskListCheckMarker", String("]")),
	))
	prefix := Sequence(firstParagraph, check, Lookahead(separator))
	return blockNative("taskListCheckPrefix", prefix, taskListCheckAction)
}

func footnoteDefinitionLabelContinuation(context *TokenizeContext) Rule {
	label := inlineLastCaptured(context)
	return Guard(func(*TokenizeContext) bool { return utf16Length(label.view.text) <= 999 })
}

func footnoteDefinitionAction(context *TokenizeContext, token *Token, _ *blockSyntaxMatch) {
	n := context.Node(token)
	n.Kind, n.Label = KindFootnoteDefinition, blockCaptured(token, "footnoteDefinitionLabel").view.text
	n.Identifier = normalizeIdentifier(n.Label)
}

func canStartTaskListCheck(context *TokenizeContext) bool {
	owner := context.parser.blockOwner
	return owner != nil && owner.node != nil && owner.node.Kind == KindListItem && blockFirstParagraph(owner.node)
}

func taskListCheckAction(_ *TokenizeContext, token *Token, match *blockSyntaxMatch) {
	match.kind = "taskListCheck"
	token.Data = map[string]any{"checked": blockCaptured(token, "taskListCheckValueChecked") != nil}
}

func (s *parseState) registerBlockFootnote(node *Node) {
	if s.doc.footnotes == nil {
		s.doc.footnotes = make(map[string]*Node)
	}
	if _, exists := s.doc.footnotes[node.Identifier]; !exists {
		s.doc.footnotes[node.Identifier] = node
	}
}

func (s *parseState) parseBlockTableMatch(token *Token, match blockExtensionMatch, view sourceView, physical []sourceLine, parent *Node, stack []*blockFrame) (int, error) {
	n := token.node
	appendNode(parent, n)
	header := blockCaptured(token, "tableHeaderRow")
	delimiter := blockCaptured(token, "tableDelimiterRow")
	capturedLines := sourceLines(view.slice(0, match.end))
	count := len(capturedLines)
	for j := 1; j < count; j++ {
		if _, _, err := blockContainerLine(stack, physical[j], true); err != nil {
			return count, err
		}
	}
	token.Children = nil
	head := makeToken("tableHead", view.slice(0, match.end))
	token.append(head)
	s.blockTableCapturedRow(n, head, header, true)
	head.append(delimiter)
	views := []sourceView{view.slice(0, match.end)}
	ending := capturedLines[len(capturedLines)-1].ending
	var body *Token
	for _, original := range physical[count:] {
		line, ok, err := blockContainerLine(stack, original, false)
		if err != nil {
			return count, err
		}
		if !ok || blockBlank(line.body.text) {
			break
		}
		interrupt, err := s.blockStarts(line.body, false)
		if err != nil {
			return count, err
		}
		if interrupt {
			break
		}
		_, indent := blockIndent(line.body)
		if indent >= 4 && s.enabled("codeIndented") {
			break
		}
		row, _, ok, err := blockRuleMatch(line.full(), tableBodyRowSyntax)
		if err != nil {
			return count, err
		}
		if !ok {
			break
		}
		line, _, err = blockContainerLine(stack, original, true)
		if err != nil {
			return count, err
		}
		if body == nil {
			body = makeToken("tableBody", line.full())
			token.append(body)
		}
		s.blockTableCapturedRow(n, body, row, false)
		views = append(views, line.full())
		ending = line.ending
		count++
	}
	if body != nil {
		body.view = joinViews(views[1:])
		body.Range = body.view.span()
	}
	setBlockView(n, views, ending)
	return count, nil
}

func (s *parseState) blockTableCapturedRow(table *Node, wrapper *Token, token *Token, header bool) {
	token.Type = "tableRow"
	row := &Node{Kind: KindTableRow, Range: token.Range, token: token, Data: map[string]any{"header": header}}
	token.node = row
	table.AppendChild(row)
	wrapper.append(token)
	if endings := blockCaptured(token, "lineEnding"); endings != nil {
		row.ending = endings.view
	}
	cells := tableSourceCells(token)
	index := 0
	for _, child := range token.Children {
		if child.Type != "tableCellSource" {
			continue
		}
		column := index
		index++
		if column >= len(cells) || column >= len(table.Alignments) {
			child.Type = "tableCellOverflow"
			continue
		}
		child.Type = "tableCell"
		content, leading, trailing := blockTrim(child.view)
		cell := &Node{Kind: KindTableCell, Range: child.Range, token: child, input: content, Alignment: table.Alignments[column], Data: map[string]any{"header": header}}
		child.node = cell
		row.AppendChild(cell)
		if len(leading.text) > 0 {
			child.append(makeToken("whitespace", leading))
		}
		child.append(makeToken("chunkText", content))
		if len(trailing.text) > 0 {
			child.append(makeToken("whitespace", trailing))
		}
	}
	for i := len(cells); i < len(table.Alignments); i++ {
		view := token.view.slice(len(token.view.text), len(token.view.text))
		cell := makeNode(KindTableCell, "tableCell", view)
		cell.Alignment = table.Alignments[i]
		cell.Data = map[string]any{"header": header}
		cell.token.append(makeToken("chunkText", view))
		appendNode(row, cell)
	}
}
