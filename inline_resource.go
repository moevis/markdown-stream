package markdown

// Resource grammar is shared by inline links and block definitions.
func destinationBodyRule(depth int) Rule {
	ordinary := Satisfy(func(code Code) bool { return code > ' ' && code != 0x7f && code != '(' && code != ')' })
	var nested Rule
	if depth < 32 {
		nested = Bind(String("("), func(*TokenizeContext) Rule {
			return Sequence(Repeat(destinationBodyRule(depth+1), 0, 0), String(")"))
		})
	}
	return Or(inlineEscapeRule, nested, ordinary)
}

func destinationRule(prefix string, allowEmpty bool) Rule {
	minimum := 1
	if allowEmpty {
		minimum = 0
	}
	literal := Capture(TokenType(prefix+"Literal"), Sequence(
		Capture(TokenType(prefix+"LiteralMarker"), String("<")),
		Capture(TokenType(prefix+"String"), Repeat(Or(inlineEscapeRule, Satisfy(func(code Code) bool {
			return code != '<' && code != '>' && code != LineFeed && code != CarriageReturn && code != CarriageReturnLineFeed
		})), 0, 0)),
		Capture(TokenType(prefix+"LiteralMarker"), String(">")),
	))
	raw := Capture(TokenType(prefix+"Raw"), Sequence(Not(String("<")), Capture(TokenType(prefix+"String"), Sequence(Repeat(destinationBodyRule(0), minimum, 0), Not(String("(")), Guard(func(context *TokenizeContext) bool {
		return context.Peek() == EOF || context.Peek() == ')' || inlineCodeWhitespace(context.Peek())
	})))))
	return Capture(TokenType(prefix), Or(literal, raw))
}

func titleRule(prefix string) Rule {
	return Capture(TokenType(prefix), Bind(Capture(TokenType(prefix+"Marker"), Runes("\"'(")), func(context *TokenizeContext) Rule {
		marker := context.view.text[context.index-1]
		if marker == '(' {
			marker = ')'
		}
		ending := Sequence(inlineEndingRule, Repeat(inlineSpaceRule, 0, 0), Not(inlineEndingRule))
		body := Repeat(Or(inlineEscapeRule, ending, Satisfy(func(code Code) bool {
			return code != Code(marker) && (marker != ')' || code != '(') && code != LineFeed && code != CarriageReturn && code != CarriageReturnLineFeed
		})), 0, 0)
		return Sequence(Capture(TokenType(prefix+"String"), body), Capture(TokenType(prefix+"Marker"), String(string(marker))))
	}))
}

func resourceRule() Rule {
	space := inlineWhitespaceRule
	return Capture("resource", Sequence(
		Capture("resourceMarker", String("(")), space,
		Or(Lookahead(String(")")), Sequence(destinationRule("resourceDestination", false),
			Optional(Sequence(Lookahead(Or(inlineSpaceRule, inlineEndingRule)), space, titleRule("resourceTitle"))), space)),
		Capture("resourceMarker", String(")")),
	))
}

func referenceLabelRule(prefix string) Rule {
	return Capture(TokenType(prefix), Sequence(
		Capture(TokenType(prefix+"Marker"), String("[")),
		Bind(Capture(TokenType(prefix+"String"), Repeat(Or(
			Sequence(String("\\"), Runes("[]\\")),
			Satisfy(func(code Code) bool { return code != '[' && code != ']' }),
		), 1, 0)), func(context *TokenizeContext) Rule {
			token := inlineLastCaptured(context)
			return Guard(func(*TokenizeContext) bool {
				return utf16Length(token.view.text) <= 999 && normalizeIdentifier(token.view.text) != ""
			})
		}),
		Capture(TokenType(prefix+"Marker"), String("]")),
	))
}

func inlineCodeWhitespace(code Code) bool {
	return code == ' ' || code == HorizontalTab || code == VirtualSpace || code == LineFeed || code == CarriageReturn || code == CarriageReturnLineFeed
}

func inlineLastCaptured(context *TokenizeContext) *Token {
	if len(context.stack) > 0 {
		children := context.stack[len(context.stack)-1].token.Children
		if len(children) > 0 {
			return children[len(children)-1]
		}
	}
	if len(context.roots) > 0 {
		return context.roots[len(context.roots)-1]
	}
	return nil
}

func inlineFindToken(token *Token, kind TokenType) *Token {
	if token.Type == kind {
		return token
	}
	for _, child := range token.Children {
		if found := inlineFindToken(child, kind); found != nil {
			return found
		}
	}
	return nil
}
