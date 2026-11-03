package markdown

import (
	"strings"
	"unicode/utf8"
)

func footnoteCallRule() Rule {
	return Capture("gfmFootnoteCall", Sequence(
		Capture("gfmFootnoteCallLabelMarker", String("[")), Capture("gfmFootnoteCallMarker", String("^")),
		Bind(Capture("gfmFootnoteCallString", Repeat(Or(Sequence(String("\\"), Runes("[]\\")), Satisfy(func(code Code) bool {
			return code != '[' && code != ']' && !inlineCodeWhitespace(code)
		})), 1, 0)), func(context *TokenizeContext) Rule {
			label := inlineLastCaptured(context).view.text
			return Guard(func(context *TokenizeContext) bool {
				if utf16Length(label) > 999 {
					return false
				}
				_, defined := context.Document.footnotes[normalizeIdentifier(label)]
				return defined
			})
		}), Capture("gfmFootnoteCallLabelMarker", String("]")),
	))
}

func inlineDefinedFootnoteAt(context *TokenizeContext, index int) bool {
	if context.parser == nil || !context.parser.gfm || !context.parser.enabled("gfmFootnoteCall") || index >= len(context.view.text) {
		return false
	}
	checkpoint := context.snapshot()
	context.index, context.virtual = index, 0
	matched := footnoteCallRule()(context, &Effects{context: context})
	context.restore(&checkpoint)
	return matched
}

func (p *inlineParser) commitFootnote(token *Token) error {
	stringToken := inlineFindToken(token, "gfmFootnoteCallString")
	identifier := normalizeIdentifier(stringToken.view.text)
	label, err := p.stringValue(stringToken.view, stringToken)
	if err != nil {
		return err
	}
	node := &Node{Kind: KindFootnoteReference, Range: token.Range, input: token.view, token: token, Identifier: identifier, Label: label, Literal: label}
	token.node = node
	count := p.s.doc.footnoteCalls[identifier] + 1
	p.s.doc.footnoteCalls[identifier] = count
	definition := p.s.doc.footnotes[identifier]
	if count == 1 {
		p.s.doc.footnoteOrder = append(p.s.doc.footnoteOrder, definition)
	}
	for index, candidate := range p.s.doc.footnoteOrder {
		if candidate == definition {
			node.Start = index + 1
			break
		}
	}
	node.Data = map[string]any{"count": count}
	return nil
}

func gfmAtext(code Code) bool {
	return inlineAlphanumericCode(code) || code == '+' || code == '-' || code == '.' || code == '_'
}

func gfmPreviousWww(r rune, eof bool) bool {
	return eof || r == '(' || r == '*' || r == '_' || r == '[' || r == ']' || r == '~' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
}

func literalLinkGuard(www bool, email bool) Rule {
	return Guard(func(context *TokenizeContext) bool {
		if context.inline != nil && context.inline.bracket != nil {
			return false
		}
		index := context.index
		var previous rune
		if index > 0 {
			previous, _ = utf8.DecodeLastRuneInString(context.view.text[:index])
		}
		if email {
			return index == 0 || previous != '/' && !gfmAtext(Code(previous))
		}
		if www {
			return gfmPreviousWww(previous, index == 0)
		}
		return !inlineAlphaCode(Code(previous))
	})
}

func literalEmailRule() Rule {
	domainPart := Repeat(Or(Satisfy(inlineAlphanumericCode), Runes("-_")), 1, 0)
	return Sequence(literalLinkGuard(false, true), Capture("literalAutolink", Capture("literalAutolinkEmail", Sequence(
		Repeat(Satisfy(gfmAtext), 1, 0), String("@"), domainPart,
		Repeat(Sequence(String("."), Lookahead(Satisfy(inlineAlphanumericCode)), domainPart), 1, 0),
		Guard(func(context *TokenizeContext) bool { return inlineAlphaCode(context.Previous) }),
	), inlineLiteralAction(KindText, nil)), literalLinkAction("mailto:")))
}

// Terminal punctuation is a grammar lookahead, not a post-hoc URL trim. An
// entity-like suffix is excluded regardless of whether its name is registered.
func literalURLTrailRule() Rule {
	boundary := Or(End(), Lookahead(Satisfy(func(code Code) bool {
		return code == '<' || inlineCodeWhitespace(code) || code >= 0 && isUnicodeWhitespace(rune(code))
	})))
	entity := Sequence(String("&"), Repeat(Satisfy(inlineAlphaCode), 1, 0), String(";"))
	punctuation := Runes("!\"')*,.:;?_~")
	bracketBoundary := Sequence(String("]"), Or(End(), Lookahead(Runes("([")), boundary))
	return Lookahead(Sequence(Repeat(Or(punctuation, entity, Sequence(Not(bracketBoundary), String("]"))), 0, 0), Or(boundary, bracketBoundary)))
}

func literalURLRule(www bool) Rule {
	prefix, kind := Or(FoldedString("https://"), FoldedString("http://")), TokenType("literalAutolinkHttp")
	if www {
		prefix, kind = Lookahead(Sequence(FoldedString("www."), Not(End()))), "literalAutolinkWww"
	}
	return Sequence(literalLinkGuard(www, false), Capture("literalAutolink", Capture(kind, Bind(prefix, func(context *TokenizeContext) Rule {
		start := context.index
		trail := literalURLTrailRule()
		domainCharacter := Satisfy(func(code Code) bool {
			return code >= 0 && !isUnicodeWhitespace(rune(code)) && code > 0x20 && (code == '-' || code == '.' || code == '_' || !isUnicodePunctuation(rune(code)))
		})
		domain := Sequence(Lookahead(Satisfy(func(code Code) bool {
			return code > 0x20 && code != 0x7f && !isUnicodeWhitespace(rune(code)) && !isUnicodePunctuation(rune(code))
		})),
			Repeat(Sequence(Not(trail), domainCharacter), 1, 0), Guard(func(context *TokenizeContext) bool {
				raw := context.view.text[start:context.index]
				lastDot := strings.LastIndexByte(raw, '.')
				previousDot := -1
				if lastDot >= 0 {
					previousDot = strings.LastIndexByte(raw[:lastDot], '.')
				}
				if strings.Contains(raw[previousDot+1:], "_") {
					return false
				}
				return strings.Trim(raw, ".-_") != ""
			}))
		return Bind(domain, func(context *TokenizeContext) Rule {
			pathStart := context.index
			balancedClosing := Guard(func(context *TokenizeContext) bool {
				if context.Peek() != ')' {
					return false
				}
				raw := context.view.text[pathStart:context.index]
				return strings.Count(raw, "(") > strings.Count(raw, ")")
			})
			pathCharacter := Satisfy(func(code Code) bool {
				return code != '<' && !inlineCodeWhitespace(code) && (code < 0 || !isUnicodeWhitespace(rune(code)))
			})
			return Repeat(Sequence(Or(balancedClosing, Not(trail)), pathCharacter), 0, 0)
		})
	}), inlineLiteralAction(KindText, nil)), literalLinkAction(func() string {
		if www {
			return "http://"
		}
		return ""
	}())))
}

func literalLinkAction(prefix string) TokenAction {
	return func(context *TokenizeContext, token *Token) error {
		node := context.Node(token)
		node.Kind, node.input, node.Destination = KindLink, token.view, prefix+token.view.text
		node.AppendChild(token.Children[0].node)
		return nil
	}
}
