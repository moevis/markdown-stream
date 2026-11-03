package core

import (
	"fmt"
	"strings"
)

// commitNative handles delimiter and bracket state only after the shared
// transactional construct dispatcher has accepted the complete Rule tree.
func (p *inlineParser) commitNative(token *Token, start, end int) error {
	switch token.Type {
	case "data":
		// Failed closers alter bracket state only after ordinary data commits.
		if strings.HasPrefix(token.view.text, "]") && p.s.enabled("labelEnd") && p.bracket != nil {
			p.bracket = p.bracket.previous
		}
	case "labelStartLink", "labelStartImage":
		image := token.Type == "labelStartImage"
		token.Type = "data"
		node := semanticNode(token)
		node.Kind, node.input, node.Literal = KindText, token.view, token.view.text
		item := p.add(node, start, end)
		item.marker = '['
		p.bracket = &inlineBracket{previous: p.bracket, item: item, labelStart: end, image: image, active: true}
		return nil
	case "labelEnd":
		return p.commitLabelEnd(token)
	case "attentionSequence", "strikethroughSequence":
		marker := token.view.text[0]
		token.Type = "data"
		item := &inlineItem{token: token, node: token.node, start: start, end: end, marker: marker}
		item.open, item.close = p.flanking(start, end, marker)
		p.items.append(item)
		return nil
	case "lineEnding", "hardBreakEscape", "hardBreakTrailing":
		if token.node != nil {
			if a, b := p.trimTrailing(); b > a {
				p.items.append(&inlineItem{token: makeToken("lineSuffix", p.view.slice(a, b)), start: a, end: b})
			}
		}
	case "autolink":
		for bracket := p.bracket; bracket != nil; bracket = bracket.previous {
			if !bracket.image {
				bracket.active = false
			}
		}
	case "gfmFootnoteCall":
		if err := p.commitFootnote(token); err != nil {
			return err
		}
	}
	p.items.append(&inlineItem{token: token, node: token.node, start: start, end: end})
	return nil
}

func (p *inlineParser) commitLabelEnd(close *Token) error {
	bracket := p.bracket
	if bracket != nil {
		p.bracket = bracket.previous
	}
	index := close.view.base - p.view.base
	end := index + len(close.view.text)
	if bracket == nil || !bracket.active {
		p.text(index, index+1)
		return nil
	}
	label := p.view.text[bracket.labelStart:index]
	identifier := normalizeIdentifier(label)
	definition, defined := p.s.doc.Definitions[identifier]
	defined = defined && validInlineReferenceLabel(label)
	resource := inlineFindToken(close, "resource")
	reference := inlineFindToken(close, "reference")
	if reference != nil {
		raw := inlineFindToken(reference, "referenceString").view.text
		if raw != "" {
			identifier = normalizeIdentifier(raw)
			definition, defined = p.s.doc.Definitions[identifier]
		}
	}
	if resource == nil && (!defined || reference == nil && index+1 < len(p.view.text) && p.view.text[index+1] == '[') {
		p.text(index, index+1)
		return nil
	}
	kind, tokenType := KindLink, TokenType("link")
	if bracket.image {
		kind, tokenType = KindImage, "image"
	}
	node := makeNode(kind, tokenType, p.view.slice(bracket.item.start, end))
	node.Label, node.Identifier = p.s.decodeString(label), identifier
	labelToken := makeToken("label", p.view.slice(bracket.item.start, index+1))
	opening := bracket.item.token
	labelToken.append(opening.Children...)
	content := makeToken("labelText", p.view.slice(bracket.labelStart, index))
	inner := inlineList{}
	if bracket.item.next != nil {
		inner.head, inner.tail = bracket.item.next, p.items.tail
		inner.head.prev, inner.tail.next = nil, nil
	}
	if err := p.resolve(&inner); err != nil {
		return err
	}
	if err := p.resolveSpan(&inner, content.view); err != nil {
		return err
	}
	p.attach(node, content, &inner)
	labelToken.append(content, close.Children[0])
	node.token.append(labelToken)
	if resource != nil {
		destination := inlineFindToken(resource, "resourceDestinationString")
		if destination != nil {
			value, err := p.stringValue(destination.view, destination)
			if err != nil {
				return err
			}
			node.Destination = value
		}
		if title := inlineFindToken(resource, "resourceTitleString"); title != nil {
			value, err := p.stringValue(title.view, title)
			if err != nil {
				return err
			}
			node.Title = value
		}
		node.token.append(resource)
	} else {
		node.Destination, node.Title = definition.Destination, definition.Title
		if reference != nil {
			stringToken := inlineFindToken(reference, "referenceString")
			if _, err := p.stringValue(stringToken.view, stringToken); err != nil {
				return err
			}
			node.token.append(reference)
		}
	}
	p.items.tail, bracket.item.next = bracket.item, nil
	p.items.replace(bracket.item, bracket.item, &inlineItem{node: node, token: node.token, start: bracket.item.start, end: end})
	if !bracket.image {
		for prior := p.bracket; prior != nil; prior = prior.previous {
			if !prior.image {
				prior.active = false
			}
		}
	}
	return nil
}

// String scopes use the same core/custom registry and resolver lifecycle as
// text scopes. Values are composed only after all resolver edits have landed.
func (p *inlineParser) stringValue(view sourceView, parent *Token) (string, error) {
	if parent == nil {
		parent = makeToken("string", view)
	}
	previousOwner := p.s.inlineOwner
	p.s.inlineOwner = parent
	defer func() { p.s.inlineOwner = previousOwner }()
	context := p.s.contextFor(PhaseString, view)
	if context != nil {
		context.parent, context.inline = parent, p
		context.prefix = func() []*Token { return parent.Children }
	}
	for index := 0; index < len(view.text) || context != nil && context.virtual > 0; {
		matched := false
		for _, after := range []bool{false, true} {
			end, roots, ok, err := p.s.trySyntax(PhaseString, view, index, after, false)
			if err != nil {
				return "", err
			}
			if !ok {
				continue
			}
			if context != nil && context.lastResolveTo {
				parent.Children = roots
			} else {
				parent.append(roots...)
			}
			index, matched = end, true
			break
		}
		if !matched {
			return "", fmt.Errorf("markdown: no string construct at byte %d", index)
		}
	}
	if err := p.s.finishSyntax(PhaseString, view, parent); err != nil {
		return "", err
	}
	var value strings.Builder
	for _, token := range parent.Children {
		inlineStringLiteral(&value, token)
	}
	if parent.Data == nil {
		parent.Data = make(map[string]any)
	}
	parent.Data["value"] = value.String()
	return value.String(), nil
}

func inlineStringLiteral(value *strings.Builder, token *Token) {
	if token.Type == "linePrefix" {
		return
	}
	if token.node != nil {
		value.WriteString(token.node.Literal)
		return
	}
	if literal, ok := token.Data["value"].(string); ok {
		value.WriteString(literal)
		return
	}
	if len(token.Children) == 0 {
		value.WriteString(token.view.text)
		return
	}
	for _, child := range token.Children {
		inlineStringLiteral(value, child)
	}
}
