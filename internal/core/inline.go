package core

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Titles keep source endings but omit continuation indentation, as the
// CommonMark title factory treats that indentation as a line prefix.
func inlineDropLinePrefixes(raw string) string {
	if !strings.ContainsAny(raw, "\r\n") {
		return raw
	}
	var value strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] != '\r' && raw[i] != '\n' {
			value.WriteByte(raw[i])
			i++
			continue
		}
		end := inlineEndingEnd(raw, i)
		value.WriteString(raw[i:end])
		i = end
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t') {
			i++
		}
	}
	return value.String()
}

func inlineTableCodeValue(raw string) string {
	var value strings.Builder
	start := 0
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		if raw[i+1] == '\\' {
			i++
		} else if raw[i+1] == '|' {
			value.WriteString(raw[start:i])
			start = i + 1
			i++
		}
	}
	if start == 0 {
		return raw
	}
	value.WriteString(raw[start:])
	return value.String()
}

type inlineItem struct {
	prev, next  *inlineItem
	token       *Token
	node        *Node
	start, end  int
	marker      byte
	open, close bool
}

type inlineList struct {
	head, tail *inlineItem
}

func (l *inlineList) append(item *inlineItem) {
	item.prev = l.tail
	if l.tail != nil {
		l.tail.next = item
	} else {
		l.head = item
	}
	l.tail = item
}

func (l *inlineList) replace(first, last, item *inlineItem) {
	item.prev, item.next = first.prev, last.next
	if item.prev != nil {
		item.prev.next = item
	} else {
		l.head = item
	}
	if item.next != nil {
		item.next.prev = item
	} else {
		l.tail = item
	}
}

func (l *inlineList) remove(item *inlineItem) {
	if item.prev != nil {
		item.prev.next = item.next
	} else {
		l.head = item.next
	}
	if item.next != nil {
		item.next.prev = item.prev
	} else {
		l.tail = item.prev
	}
}

type inlineBracket struct {
	previous      *inlineBracket
	item          *inlineItem
	labelStart    int
	image, active bool
}

type inlineParser struct {
	s       *parseState
	view    sourceView
	parent  *Node
	items   inlineList
	bracket *inlineBracket
}

func (s *parseState) parseInlines(view sourceView, parent *Node) error {
	previousOwner := s.inlineOwner
	s.inlineOwner = parent.token
	defer func() { s.inlineOwner = previousOwner }()
	p := inlineParser{s: s, view: view, parent: parent}
	context := s.contextFor(PhaseText, view)
	if context != nil {
		context.parent = parent.token
		context.prefix = p.syntaxRoots
		context.inline = &p
	}
	if err := p.parse(); err != nil {
		return err
	}
	if err := p.resolve(&p.items); err != nil {
		return err
	}
	p.attach(parent, parent.token, &p.items)
	return s.finishSyntax(PhaseText, view, parent.token)
}

func (p *inlineParser) syntaxRoots() []*Token {
	var roots []*Token
	for item := p.items.head; item != nil; item = item.next {
		roots = append(roots, item.token)
	}
	return roots
}

func (p *inlineParser) attach(parent *Node, token *Token, items *inlineList) {
	for item := items.head; item != nil; item = item.next {
		appendInlineSemantic(parent, item.token)
		if token != nil {
			token.append(item.token)
		}
	}
}

func appendInlineSemantic(parent *Node, token *Token) {
	if token.node != nil {
		parent.AppendChild(token.node)
		return
	}
	for _, child := range token.Children {
		appendInlineSemantic(parent, child)
	}
}

func (p *inlineParser) resolveSpan(items *inlineList, view sourceView) error {
	if len(p.s.insideSpan) == 0 {
		return nil
	}
	var roots []*Token
	for item := items.head; item != nil; item = item.next {
		roots = append(roots, item.token)
	}
	resolved, err := p.s.resolveInsideSpan(roots, view)
	if err != nil {
		return err
	}
	unchanged := len(resolved) == len(roots)
	if unchanged {
		for index, token := range resolved {
			if token != roots[index] {
				unchanged = false
				break
			}
		}
	}
	if unchanged {
		return nil
	}
	*items = inlineList{}
	for _, token := range resolved {
		items.append(&inlineItem{token: token, node: token.node})
	}
	return nil
}

func (p *inlineParser) add(node *Node, start, end int) *inlineItem {
	item := &inlineItem{node: node, token: node.token, start: start, end: end}
	p.items.append(item)
	return item
}

func (p *inlineParser) text(start, end int) *inlineItem {
	if end <= start {
		return nil
	}
	last := p.items.tail
	if last != nil && last.marker == 0 && last.node != nil && last.node.Kind == KindText && last.token.Type == "data" && last.end == start {
		last.end = end
		last.node.Literal = p.view.text[last.start:end]
		last.token.view = p.view.slice(last.start, end)
		last.token.Range = last.token.view.span()
		last.node.Range = last.token.Range
		last.node.input = last.token.view
		return last
	}
	node := makeNode(KindText, "data", p.view.slice(start, end))
	node.Literal = p.view.text[start:end]
	return p.add(node, start, end)
}

func (p *inlineParser) syntax(phase Phase, index int, after bool) (int, bool, error) {
	end, roots, matched, err := p.s.trySyntax(phase, p.view, index, after, false)
	if err != nil || !matched {
		return index, false, err
	}
	context := p.s.contextFor(phase, p.view)
	if context != nil && context.lastResolveTo {
		previous := make(map[*Token]*inlineItem)
		for item := p.items.head; item != nil; item = item.next {
			previous[item.token] = item
		}
		retained := make(map[*inlineItem]bool)
		p.items = inlineList{}
		for _, token := range roots {
			item := previous[token]
			if item == nil {
				item = &inlineItem{token: token, node: token.node, start: token.view.base - p.view.base, end: token.view.base - p.view.base + len(token.view.text)}
			}
			item.prev, item.next = nil, nil
			p.items.append(item)
			retained[item] = true
		}
		for bracket := p.bracket; bracket != nil; bracket = bracket.previous {
			if !retained[bracket.item] {
				bracket.active = false
			}
		}
		return end, true, nil
	}
	if context != nil && context.matched != nil && context.matched.native {
		for _, token := range roots {
			start, stop := index, end
			if token.Type != "data" {
				start = token.view.base - p.view.base
				stop = start + len(token.view.text)
			}
			if err := p.commitNative(token, start, stop); err != nil {
				return index, false, err
			}
		}
	} else {
		for _, token := range roots {
			semanticNode(token)
			p.items.append(&inlineItem{token: token, node: token.node, start: index, end: end})
		}
	}
	return end, true, nil
}

func (p *inlineParser) parse() error {
	context := p.s.contextFor(PhaseText, p.view)
	for index := 0; index < len(p.view.text) || context != nil && context.virtual > 0; {
		matched := false
		for _, after := range [2]bool{false, true} {
			end, ok, err := p.syntax(PhaseText, index, after)
			if err != nil {
				return err
			}
			if ok {
				index, matched = end, true
				break
			}
		}
		if !matched {
			return fmt.Errorf("markdown: no text construct at byte %d", index)
		}
	}
	if start, end := p.trimTrailing(); end > start {
		p.items.append(&inlineItem{token: makeToken("lineSuffix", p.view.slice(start, end)), start: start, end: end})
	}
	return nil
}

func inlineEndingEnd(text string, index int) int {
	if text[index] == '\r' && index+1 < len(text) && text[index+1] == '\n' {
		return index + 2
	}
	return index + 1
}

func (p *inlineParser) trimTrailing() (int, int) {
	last := p.items.tail
	if last == nil {
		return 0, 0
	}
	end, start := last.end, last.end
	for last != nil && last.marker == 0 && last.node != nil && last.node.Kind == KindText && last.token.Type == "data" {
		value := last.node.Literal
		trimmed := strings.TrimRight(value, " \t")
		if len(trimmed) == len(value) {
			break
		}
		if trimmed == "" {
			start = last.start
			previous := last.prev
			p.items.remove(last)
			last = previous
			continue
		}
		removed := len(value) - len(trimmed)
		last.end -= removed
		start = last.end
		last.token.view = last.token.view.slice(0, len(trimmed))
		last.token.Range.End = last.token.view.position(len(trimmed), true)
		last.node.input, last.node.Range, last.node.Literal = last.token.view, last.token.Range, trimmed
		break
	}
	return start, end
}

func inlineCharacterClass(r rune, eof bool) int {
	if eof || isUnicodeWhitespace(r) {
		return 1
	}
	if isUnicodePunctuation(r) {
		return 2
	}
	return 0
}

func (p *inlineParser) flanking(start, end int, marker byte) (bool, bool) {
	var before, after rune
	if start > 0 {
		before, _ = utf8.DecodeLastRuneInString(p.view.text[:start])
	}
	if end < len(p.view.text) {
		after, _ = utf8.DecodeRuneInString(p.view.text[end:])
	}
	b, a := inlineCharacterClass(before, start == 0), inlineCharacterClass(after, end == len(p.view.text))
	open := a == 0 || a == 2 && b != 0
	close := b == 0 || b == 2 && a != 0
	if marker != '~' {
		open = open || after != '*' && after != '_' && (p.s.attention[Code(after)] || p.s.gfm && after == '~')
		close = close || before != '*' && before != '_' && (p.s.attention[Code(before)] || p.s.gfm && before == '~')
	}
	if marker == '_' {
		return open && (b != 0 || !close), close && (a != 0 || !open)
	}
	return open, close
}

// Resolve closing delimiters in source order. Completed spans form barriers,
// so delimiter pairs can never cross a label or
// an already resolved span. Remaining markers are genuine source-backed data.
func (p *inlineParser) resolve(items *inlineList) error {
	for closer := items.head; closer != nil; {
		if !closer.close || closer.marker == 0 {
			closer = closer.next
			continue
		}
		strike := closer.marker == '~'
		var opener *inlineItem
		for candidate := closer.prev; candidate != nil; candidate = candidate.prev {
			if !candidate.open || candidate.marker != closer.marker {
				continue
			}
			a, b := candidate.end-candidate.start, closer.end-closer.start
			if strike {
				if a != b {
					continue
				}
			} else if (candidate.close || closer.open) && b%3 != 0 && (a+b)%3 == 0 {
				continue
			}
			opener = candidate
			break
		}
		if opener == nil {
			closer = closer.next
			continue
		}
		use := 1
		kind, tokenType := KindEmphasis, TokenType("emphasis")
		if strike {
			use, kind, tokenType = opener.end-opener.start, KindStrikethrough, "strikethrough"
		} else if opener.end-opener.start >= 2 && closer.end-closer.start >= 2 {
			use, kind, tokenType = 2, KindStrong, "strong"
		}
		openStart, closeEnd := opener.end-use, closer.start+use
		n := makeNode(kind, tokenType, p.view.slice(openStart, closeEnd))
		sequence := TokenType(string(tokenType) + "Sequence")
		content := makeToken(TokenType(string(tokenType)+"Text"), p.view.slice(opener.end, closer.start))
		inner := inlineList{}
		if opener.next != closer {
			inner.head, inner.tail = opener.next, closer.prev
			inner.head.prev, inner.tail.next = nil, nil
		}
		if err := p.resolve(&inner); err != nil {
			return err
		}
		if err := p.resolveSpan(&inner, content.view); err != nil {
			return err
		}
		p.attach(n, content, &inner)
		n.token.append(makeToken(sequence, p.view.slice(openStart, opener.end)), content, makeToken(sequence, p.view.slice(closer.start, closeEnd)))
		group := &inlineItem{node: n, token: n.token, start: openStart, end: closeEnd}
		opener.next, closer.prev = closer, opener
		first, last := opener, closer
		if opener.start < openStart {
			p.resizeText(opener, opener.start, openStart)
			first = closer
			closer.prev = opener
		}
		if closeEnd < closer.end {
			p.resizeText(closer, closeEnd, closer.end)
			last = first
			if first == closer {
				// Both markers remain: insert into the newly empty gap.
				group.prev, group.next = opener, closer
				opener.next, closer.prev = group, group
			} else {
				// Only the closer remains.
				group.prev, group.next = opener.prev, closer
				if opener.prev == nil {
					items.head = group
				} else {
					opener.prev.next = group
				}
				closer.prev = group
			}
		} else {
			items.replace(first, last, group)
		}
		if closeEnd < closer.end {
			continue
		}
		closer = group.next
	}
	return nil
}

func (p *inlineParser) resizeText(item *inlineItem, start, end int) {
	item.start, item.end = start, end
	item.token.view = p.view.slice(start, end)
	item.token.Range = item.token.view.span()
	item.node.input = item.token.view
	item.node.Range = item.token.Range
	item.node.Literal = p.view.text[start:end]
}

// Shortcut and collapsed references reuse link text, which has no raw length
// limit. Explicit reference labels and definitions are bounded by their rules.
func validInlineReferenceLabel(raw string) bool {
	if normalizeIdentifier(raw) == "" {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' && i+1 < len(raw) && isASCIIPunctuation(raw[i+1]) {
			i++
		} else if raw[i] == '[' || raw[i] == ']' {
			return false
		}
	}
	return true
}
