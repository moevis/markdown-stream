package markdown

import (
	"html"
	"strconv"
	"strings"
	"unicode/utf8"
)

func inlineNative(name string, rule Rule) Construct {
	return Construct{Name: name, Rule: rule, native: true}
}

func characterEscapeRule() Rule {
	marker := Capture("escapeMarker", String("\\"))
	value := Capture("characterEscapeValue", Satisfy(func(code Code) bool {
		return code >= 0 && code < 128 && isASCIIPunctuation(byte(code))
	}))
	return Capture("characterEscape", Sequence(marker, value),
		inlineLiteralAction(KindText, func(raw string) string { return raw[1:] }))
}

func hardBreakEscapeRule() Rule {
	marker := Capture("hardBreakEscapeMarker", String("\\"))
	ending := Capture("lineEnding", inlineEndingRule)
	hardBreak := Capture("hardBreakEscape", Sequence(marker, ending),
		inlineLiteralAction(KindHardBreak, func(raw string) string { return raw[1:] }))
	prefix := OptionalCapture("linePrefix", Some(inlineSpaceRule))
	return Sequence(hardBreak, prefix)
}

func labelStartLinkRule() Rule {
	return Capture("labelStartLink", Capture("labelMarker", String("[")))
}

func inlineEscapePunctuation(code Code) bool {
	return code >= 0 && code < 128 && isASCIIPunctuation(byte(code))
}

func labelStartImageRule() Rule {
	link := Guard(canStartImage)
	marker := Capture("labelImageMarker", String("!"))
	label := Capture("labelMarker", String("["))
	return Sequence(link, Capture("labelStartImage", Sequence(marker, label)))
}

func canStartImage(context *TokenizeContext) bool {
	return !inlineDefinedFootnoteAt(context, context.index+1)
}

func ordinaryDataRule() Rule {
	ordinary := Guard(canContinueOrdinaryData)
	return Capture("data", Sequence(Any(), Repeat(Sequence(ordinary, Any()), 0, 0)), inlineLiteralAction(KindText, nil))
}

func canContinueOrdinaryData(context *TokenizeContext) bool {
	code := context.Peek()
	for priority := range 3 {
		for _, key := range [2]Code{code, Fallback} {
			for _, construct := range context.parser.constructs[context.Phase][key].group(priority) {
				if context.parser.enabled(construct.Name) && (construct.Previous == nil || construct.Previous(context.Previous)) {
					return false
				}
			}
		}
	}
	return true
}

func inlineLiteralAction(kind NodeKind, transform func(string) string) TokenAction {
	return func(context *TokenizeContext, token *Token) error {
		node := context.Node(token)
		node.Kind, node.input = kind, token.view
		node.Literal = token.view.text
		if transform != nil {
			node.Literal = transform(node.Literal)
		}
		return nil
	}
}

func characterReferenceRule() Rule {
	digits := Capture("characterReferenceValue", Repeat(Runes("0123456789"), 1, 7))
	hex := Sequence(Capture("characterReferenceMarkerHexadecimal", Runes("xX")), Capture("characterReferenceValue", Repeat(Runes("0123456789abcdefABCDEF"), 1, 6)))
	numeric := Sequence(Capture("characterReferenceMarkerNumeric", String("#")), Or(hex, digits))
	named := BindCapture("characterReferenceValue", Sequence(Satisfy(inlineAlphaCode), Repeat(Satisfy(inlineAlphanumericCode), 0, 30)), func(_ *TokenizeContext, value *Token) Rule {
		name := value.view.text
		return Guard(func(*TokenizeContext) bool { return namedEntity(name) })
	})
	return Capture("characterReference", Sequence(Capture("characterReferenceMarker", String("&")), Or(numeric, named), Capture("characterReferenceMarker", String(";"))), characterReferenceAction)
}

func characterReferenceAction(context *TokenizeContext, token *Token) error {
	raw := token.view.text
	value := html.UnescapeString(raw)
	if raw[1] == '#' {
		base, start := 10, 2
		if raw[start] == 'x' || raw[start] == 'X' {
			base, start = 16, start+1
		}
		number, _ := strconv.ParseInt(raw[start:len(raw)-1], base, 32)
		if number < 9 || number == 11 || number > 13 && number < 32 || number > 126 && number < 160 || number >= 0xd800 && number <= 0xdfff || number >= 0xfdd0 && number <= 0xfdef || number&0xffff == 0xfffe || number&0xffff == 0xffff || number > utf8.MaxRune {
			number = int64(utf8.RuneError)
		}
		value = string(rune(number))
	}
	node := context.Node(token)
	node.Kind, node.input, node.Literal = KindText, token.view, value
	return nil
}

func inlineAlphaCode(code Code) bool { return code >= 'A' && code <= 'Z' || code >= 'a' && code <= 'z' }
func inlineAlphanumericCode(code Code) bool {
	return inlineAlphaCode(code) || code >= '0' && code <= '9'
}

func attentionRule(marker byte, maximum int) Rule {
	return Sequence(Capture("attentionSequence", Repeat(String(string(marker)), 1, maximum), inlineLiteralAction(KindText, nil)), Not(String(string(marker))))
}

func hardBreakTrailingRule() Rule {
	spaces := Capture("hardBreakTrailingSpace", Repeat(String(" "), 2, 0))
	ending := Capture("lineEnding", inlineEndingRule)
	hardBreak := Capture("hardBreakTrailing", Sequence(spaces, ending),
		inlineLiteralAction(KindHardBreak, func(raw string) string {
			return raw[strings.IndexAny(raw, "\r\n"):]
		}))
	prefix := OptionalCapture("linePrefix", Some(inlineSpaceRule))
	return Sequence(hardBreak, prefix)
}

func codeTextRule(marker byte, prefix string, kind NodeKind) Rule {
	sequence := TokenType(prefix + "Sequence")
	return Sequence(Guard(func(context *TokenizeContext) bool {
		return context.index == 0 || context.view.text[context.index-1] != marker || context.inline != nil && context.inline.items.tail != nil && context.inline.items.tail.token.Type == "characterEscape"
	}), Capture(TokenType(prefix), BindCapture(sequence, Some(String(string(marker))), func(context *TokenizeContext, sequenceToken *Token) Rule {
		fence := sequenceToken.view.text
		bodyStart := context.index
		close := Sequence(Guard(func(context *TokenizeContext) bool {
			return context.index == bodyStart || context.view.text[context.index-1] != marker
		}), String(fence), Not(String(string(marker))))
		if marker == '$' {
			close = Sequence(Guard(func(context *TokenizeContext) bool {
				return inlineMathFenceUnescaped(context, bodyStart)
			}), close)
		}
		body := func(terminator Rule) Rule {
			data := Capture(TokenType(prefix+"Data"), Repeat(Sequence(Not(terminator), Satisfy(func(code Code) bool {
				return code != LineFeed && code != CarriageReturn && code != CarriageReturnLineFeed
			})), 1, 0))
			return Repeat(Sequence(Not(terminator), Or(Capture("lineEnding", inlineEndingRule), data)), 0, 0)
		}
		untrimmed := Sequence(body(close), Capture(sequence, close))
		if kind != KindCodeSpan {
			return untrimmed
		}
		paddingCharacter := Or(String(" "), inlineEndingRule)
		padding := Capture(TokenType(prefix+"Padding"), paddingCharacter)
		paddedClose := Sequence(paddingCharacter, close)
		trimmed := Sequence(padding, Not(Sequence(Many(paddingCharacter), close)), body(Or(close, paddedClose)), padding, Capture(sequence, close))
		return Or(trimmed, untrimmed)
	}), inlineCodeTextAction(prefix, kind)))
}

func inlineMathFenceUnescaped(context *TokenizeContext, bodyStart int) bool {
	escapes := 0
	for index := context.index - 1; index >= bodyStart && context.view.text[index] == '\\'; index-- {
		escapes++
	}
	return escapes%2 == 0
}

func inlineCodeTextAction(prefix string, kind NodeKind) TokenAction {
	return func(context *TokenizeContext, token *Token) error {
		node := context.Node(token)
		node.Kind, node.input = kind, token.view
		first, last := token.Children[0], token.Children[len(token.Children)-1]
		raw := context.view.text[first.view.base-context.view.base+len(first.view.text) : last.view.base-context.view.base]
		value := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", " "), "\r", " "), "\n", " ")
		if kind == KindCodeSpan && token.Children[1].Type == TokenType(prefix+"Padding") {
			value = value[1 : len(value)-1]
		}
		if kind == KindMathInline {
			value = raw
		}
		if context.inline != nil && context.inline.parent != nil && context.inline.parent.Kind == KindTableCell && kind == KindCodeSpan {
			value = inlineTableCodeValue(value)
		}
		node.Literal = value
		return nil
	}
}

func labelEndRule() Rule {
	return Sequence(Guard(canEndInlineLabel), Capture("labelEnd", BindCapture("labelMarker", String("]"), inlineLabelEndTail)))
}

func canEndInlineLabel(context *TokenizeContext) bool {
	return context.inline != nil && context.inline.bracket != nil && context.inline.bracket.active
}

func inlineLabelEndTail(context *TokenizeContext, _ *Token) Rule {
	p := context.inline
	bracket := p.bracket
	label := context.view.text[bracket.labelStart : context.index-1]
	_, defined := context.Document.Definitions[normalizeIdentifier(label)]
	defined = defined && validInlineReferenceLabel(label)
	reference := Sequence(referenceLabelRule("reference"), Guard(inlineReferenceDefined))
	collapsed := Sequence(Guard(func(*TokenizeContext) bool { return defined }), Capture("reference", Sequence(Capture("referenceMarker", String("[")), Capture("referenceString", Guard(func(*TokenizeContext) bool { return true })), Capture("referenceMarker", String("]")))))
	shortcut := Guard(func(context *TokenizeContext) bool { return defined && context.Peek() != '[' })
	return Or(resourceRule(), reference, collapsed, shortcut)
}

func inlineReferenceDefined(context *TokenizeContext) bool {
	token := inlineLastCaptured(context)
	_, exists := context.Document.Definitions[normalizeIdentifier(inlineFindToken(token, "referenceString").view.text)]
	return exists
}

func angleAutolinkRule() Rule {
	protocol := Capture("autolinkProtocol", Sequence(Satisfy(inlineAlphaCode), Repeat(Or(Satisfy(inlineAlphanumericCode), Runes("+.-")), 1, 31), String(":"), Many(Satisfy(func(code Code) bool { return code > 0x20 && code != '<' && code != '>' }))), inlineLiteralAction(KindText, nil))
	domainLabel := Bind(Repeat(Or(Satisfy(inlineAlphanumericCode), String("-")), 1, 63), func(context *TokenizeContext) Rule {
		return Guard(func(*TokenizeContext) bool { return inlineAlphanumericCode(context.Previous) })
	})
	email := Capture("autolinkEmail", Sequence(Repeat(Or(Satisfy(inlineAlphanumericCode), Runes(".!#$%&'*+-/=?^_`{|}~")), 1, 0), String("@"), Lookahead(Satisfy(inlineAlphanumericCode)), domainLabel, Repeat(Sequence(String("."), Lookahead(Satisfy(inlineAlphanumericCode)), domainLabel), 0, 0)), inlineLiteralAction(KindText, nil))
	return Capture("autolink", Sequence(Capture("autolinkMarker", String("<")), Or(protocol, email), Capture("autolinkMarker", String(">"))), angleAutolinkAction)
}

func angleAutolinkAction(context *TokenizeContext, token *Token) error {
	node := context.Node(token)
	node.Kind, node.input = KindLink, token.view
	label := token.Children[1]
	node.Destination = label.view.text
	if label.Type == "autolinkEmail" {
		node.Destination = "mailto:" + node.Destination
	}
	node.AppendChild(label.node)
	return nil
}

func htmlTextRule() Rule {
	data := func(rule Rule) Rule { return Capture("htmlTextData", rule) }
	ending := Sequence(Capture("lineEnding", inlineEndingRule), OptionalCapture("linePrefix", Some(inlineSpaceRule)))
	spacePart := Or(ending, data(Some(inlineSpaceRule)))
	space, space1 := Many(spacePart), Some(spacePart)
	name := data(Sequence(Satisfy(inlineAlphaCode), Repeat(Or(Satisfy(inlineAlphanumericCode), String("-")), 0, 0)))
	attributeName := data(Sequence(Or(Satisfy(inlineAlphaCode), Runes("_:")), Repeat(Or(Satisfy(inlineAlphanumericCode), Runes("_:.-")), 0, 0)))
	body := func(terminator Rule) Rule {
		chunk := data(Repeat(Sequence(Not(terminator), Satisfy(func(code Code) bool {
			return code != LineFeed && code != CarriageReturn && code != CarriageReturnLineFeed
		})), 1, 0))
		return Repeat(Sequence(Not(terminator), Or(ending, chunk)), 0, 0)
	}
	quoted := func(marker string) Rule {
		return Sequence(data(String(marker)), body(String(marker)), data(String(marker)))
	}
	unquoted := data(Repeat(Satisfy(func(code Code) bool {
		return !inlineCodeWhitespace(code) && code >= 0 && !strings.ContainsRune("\"'=<>`", rune(code))
	}), 1, 0))
	attribute := Sequence(space1, attributeName, Optional(Sequence(space, data(String("=")), space, Or(quoted("\""), quoted("'"), unquoted))))
	open := Sequence(data(String("<")), name, Many(attribute), space, Optional(data(String("/"))), data(String(">")))
	close := Sequence(data(String("</")), name, space, data(String(">")))
	comment := Sequence(data(String("<!--")), Or(data(String(">")), data(String("->")), Sequence(body(String("-->")), data(String("-->")))))
	processing := Sequence(data(String("<?")), body(String("?>")), data(String("?>")))
	cdata := Sequence(data(String("<![CDATA[")), body(String("]]>")), data(String("]]>")))
	declaration := Sequence(data(String("<!")), data(Satisfy(inlineAlphaCode)), body(String(">")), data(String(">")))
	return Capture("htmlText", Or(comment, processing, cdata, declaration, close, open), inlineLiteralAction(KindHTMLInline, inlineDropLinePrefixes))
}
