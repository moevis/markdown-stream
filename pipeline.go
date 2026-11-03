package markdown

import "strings"

func (s *parseState) completeDocument() error {
	root := s.doc.Root
	for {
		if err := s.expandBlockContent(root.token); err != nil {
			return err
		}
		finished := false
		for _, context := range s.contexts {
			if context.Phase >= PhaseText || context.resolved || len(context.resolveAll) == 0 {
				continue
			}
			parent := context.parent
			if parent == nil {
				parent = containingToken(root.token, context.view.span())
			}
			if parent != nil {
				if err := s.finishContext(context, parent); err != nil {
					return err
				}
				finished = true
			}
		}
		if !finished {
			break
		}
	}
	if err := s.expandStringContent(root.token); err != nil {
		return err
	}
	clear(s.doc.Definitions)
	clear(s.doc.footnotes)
	s.finalizeBlockValues(root.token)
	if err := s.parseDeferredInlines(root); err != nil {
		return err
	}
	if err := s.expandInlineContent(root.token); err != nil {
		return err
	}
	mergeRecordedPrefixes(root.token)
	return nil
}

func (s *parseState) expandBlockContent(token *Token) error {
	if token.ContentType == ContentDocument || token.ContentType == ContentFlow {
		token.ContentType = ContentNone
		node := semanticNode(token)
		if err := s.parseBlocks(sourceLines(token.view), node); err != nil {
			return err
		}
	}
	for _, child := range token.Children {
		if err := s.expandBlockContent(child); err != nil {
			return err
		}
	}
	return nil
}

func (s *parseState) expandStringContent(token *Token) error {
	if token.ContentType == ContentString {
		token.ContentType = ContentNone
		if err := s.parseString(token.view, token); err != nil {
			return err
		}
	}
	for _, child := range token.Children {
		if err := s.expandStringContent(child); err != nil {
			return err
		}
	}
	return nil
}

func (s *parseState) finalizeBlockValues(token *Token) {
	if node := token.node; node != nil {
		switch node.Kind {
		case KindDefinition:
			if destination := blockCaptured(token, "definitionDestinationString"); destination != nil {
				node.Destination, _ = destination.Data["value"].(string)
			}
			if title := blockCaptured(token, "definitionTitleString"); title != nil {
				node.Title, _ = title.Data["value"].(string)
			}
			previous, exists := s.doc.Definitions[node.Identifier]
			if !exists || node.Range.Start.ByteOffset < previous.Node.Range.Start.ByteOffset {
				s.doc.Definitions[node.Identifier] = Definition{Destination: node.Destination, Title: node.Title, Node: node}
			}
		case KindCodeBlock:
			if info := blockCaptured(token, "codeFencedFenceInfo"); info != nil {
				node.Info, _ = info.Data["value"].(string)
				node.Data["language"] = node.Info
			}
			if meta := blockCaptured(token, "codeFencedFenceMeta"); meta != nil {
				node.Meta, _ = meta.Data["value"].(string)
			}
		case KindFootnoteDefinition:
			previous := s.doc.footnotes[node.Identifier]
			if previous == nil || node.Range.Start.ByteOffset < previous.Range.Start.ByteOffset {
				s.doc.footnotes[node.Identifier] = node
			}
		}
	}
	for _, child := range token.Children {
		s.finalizeBlockValues(child)
	}
}

func (s *parseState) parseDeferredInlines(node *Node) error {
	if node.token != nil {
		placeholder := -1
		for index, token := range node.token.Children {
			if token.Type == "chunkText" {
				placeholder = index
				break
			}
		}
		if placeholder >= 0 {
			concrete := node.token.Children
			node.token.Children = nil
			if err := s.parseInlines(node.input, node); err != nil {
				return err
			}
			inlines := node.token.Children
			children := make([]*Token, 0, len(concrete)-1+len(inlines))
			children = append(children, concrete[:placeholder]...)
			children = append(children, inlines...)
			children = append(children, concrete[placeholder+1:]...)
			node.token.Children = children
		}
	}
	for _, child := range node.Children {
		if err := s.parseDeferredInlines(child); err != nil {
			return err
		}
	}
	return nil
}

func (s *parseState) expandInlineContent(token *Token) error {
	if token.ContentType == ContentText {
		token.ContentType = ContentNone
		node := semanticNode(token)
		if err := s.parseInlines(token.view, node); err != nil {
			return err
		}
	} else if token.ContentType == ContentString {
		token.ContentType = ContentNone
		if err := s.parseString(token.view, token); err != nil {
			return err
		}
	}
	for _, child := range token.Children {
		if err := s.expandInlineContent(child); err != nil {
			return err
		}
	}
	return nil
}

func (s *parseState) parseString(view sourceView, parent *Token) error {
	parser := inlineParser{s: s, view: view}
	value, err := parser.stringValue(view, parent)
	if err != nil {
		return err
	}
	if parent.Data == nil {
		parent.Data = make(map[string]any)
	}
	parent.Data["value"] = value
	return nil
}

func (s *parseState) finishContext(context *TokenizeContext, parent *Token) error {
	if context.resolved {
		return nil
	}
	context.resolved = true
	context.parent = parent
	// Keep the logical grammar view; the enclosing token may include margins.
	context.index = len(context.view.text)
	events := appendEvents(parent.Children, nil)
	for _, resolver := range context.resolveAll {
		context.Events = events
		resolved, err := resolver.Resolve(events, context)
		if err != nil {
			return err
		}
		if context.err != nil {
			return context.err
		}
		events = resolved
	}
	roots, err := rootsFromEvents(events, false)
	if err != nil {
		return err
	}
	parent.Children = roots
	if parent.node != nil {
		syncSemanticChildren(parent.node, roots)
	}
	return nil
}

func containingToken(token *Token, span SourceRange) *Token {
	if token.Range.Start.ByteOffset > span.Start.ByteOffset || token.Range.End.ByteOffset < span.End.ByteOffset {
		return nil
	}
	for _, child := range token.Children {
		if result := containingToken(child, span); result != nil {
			return result
		}
	}
	return token
}

func mergeRecordedPrefixes(token *Token) {
	for _, child := range token.Children {
		mergeRecordedPrefixes(child)
	}
	if token.node == nil {
		return
	}
	switch token.node.Kind {
	case KindDocument, KindBlockQuote, KindList, KindListItem, KindFootnoteDefinition, KindCustom:
		mergeContainerPrefixes(token)
	}
}

// Concrete compilation retains the semantic values produced by tokenization.
// The public AST is an editable copy: mutating it does not rewrite events.
func cloneDocumentAST(document *Document) *Node {
	clones := make(map[*Node]*Node)
	var clone func(*Node, *Node) *Node
	clone = func(original, parent *Node) *Node {
		copy := *original
		copy.parent = parent
		copy.Children = nil
		copy.Data = copyMetadata(original.Data)
		if original.Alignments != nil {
			copy.Alignments = append([]Alignment(nil), original.Alignments...)
		}
		clones[original] = &copy
		if len(original.Children) != 0 {
			copy.Children = make([]*Node, len(original.Children))
			for index, child := range original.Children {
				copy.Children[index] = clone(child, &copy)
			}
		}
		return &copy
	}
	root := clone(document.Root, nil)
	for identifier, definition := range document.Definitions {
		if definition.Node != nil {
			if cloned := clones[definition.Node]; cloned != nil {
				definition.Node = cloned
			} else {
				definition.Node = clone(definition.Node, nil)
			}
		}
		document.Definitions[identifier] = definition
	}
	return root
}

// Serialize returns logical source, optionally expanding physical tabs to their
// source-column width. Omitted container margins are not reintroduced.
func (t *Token) Serialize(expandTabs bool) string {
	if !expandTabs || !strings.ContainsRune(t.view.text, '\t') {
		return t.view.text
	}
	var value strings.Builder
	start := 0
	for index := range t.view.text {
		if t.view.text[index] != '\t' {
			continue
		}
		value.WriteString(t.view.text[start:index])
		column := sourceVisualColumn(t.view, index)
		for range 4 - (column-1)%4 {
			value.WriteByte(' ')
		}
		start = index + 1
	}
	value.WriteString(t.view.text[start:])
	return value.String()
}

func (c *TokenizeContext) Serialize(token *Token, expandTabs bool) string {
	return token.Serialize(expandTabs)
}
