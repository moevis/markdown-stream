package markdown

import (
	"strconv"
	"strings"
)

// blockSyntaxMatch contains only semantics derived from successful captures.
// Container continuation and indentation remain layout state, not recognizers.
type blockSyntaxMatch struct {
	kind      string
	body      sourceView
	marker    blockListMarker
	level     int
	fence     byte
	fenceSize int
	indent    int
	htmlClass int
	htmlEnd   string
	closed    bool
	align     []Alignment
}

func blockSpaceCode(code Code) bool {
	return code == ' ' || code == HorizontalTab || code == VirtualSpace
}
func blockEndingCode(code Code) bool {
	return code == LineFeed || code == CarriageReturn || code == CarriageReturnLineFeed
}

func blockEndingRule(kind TokenType) Rule { return OptionalCapture(kind, Satisfy(blockEndingCode)) }

func blockCaptured(token *Token, kind TokenType) *Token {
	for _, child := range token.Children {
		if child.Type == kind {
			return child
		}
		if found := blockCaptured(child, kind); found != nil {
			return found
		}
	}
	return nil
}

func blockCapturedAll(token *Token, kind TokenType, result []*Token) []*Token {
	for _, child := range token.Children {
		if child.Type == kind {
			result = append(result, child)
		} else {
			result = blockCapturedAll(child, kind, result)
		}
	}
	return result
}

func blockRemaining(context *TokenizeContext) sourceView {
	view := context.view.slice(context.index, len(context.view.text))
	if context.virtual > 0 && context.index > 0 {
		view = joinViews([]sourceView{paddingView(context.virtual, context.view.slice(context.index-1, context.index)), view})
	}
	if end := strings.IndexAny(view.text, "\r\n"); end >= 0 {
		return view.slice(0, end)
	}
	return view
}

func blockNative(kind string, rule Rule, action func(*TokenizeContext, *Token, *blockSyntaxMatch)) Rule {
	return Capture(TokenType(kind), rule, func(context *TokenizeContext, token *Token) error {
		match := &blockSyntaxMatch{kind: kind, body: blockRemaining(context)}
		if action != nil {
			action(context, token, match)
		}
		if token.Data == nil {
			token.Data = make(map[string]any)
		}
		token.Data["nativeBlock"] = match
		return nil
	})
}

func blockNativeMatch(roots []*Token) (*Token, *blockSyntaxMatch) {
	if len(roots) == 0 {
		return nil, nil
	}
	for _, trailing := range roots[1:] {
		if trailing.Type != "lineEnding" {
			return nil, nil
		}
	}
	token := roots[0]
	if token.context == nil || token.context.matched == nil || !token.context.matched.native {
		return nil, nil
	}
	match, _ := token.Data["nativeBlock"].(*blockSyntaxMatch)
	return token, match
}

func blockCommitCapture(token *Token) {
	delete(token.Data, "nativeBlock")
	if len(token.Data) == 0 {
		token.Data = nil
	}
}

var blockContinuationConstruct = Construct{native: true}

func blockListRule() Rule {
	digits := Capture("listItemValue", Repeat(Runes("0123456789"), 1, 9))
	ordered := Sequence(digits, Capture("listItemMarker", Runes(".)")))
	unordered := Capture("listItemMarker", Runes("*+-"))
	// Padding is a visual-column constraint; the fallback consumes one column
	// of oversized whitespace, leaving source-backed virtual tab padding.
	padding := Sequence(Repeat(blockSpaceRule, 1, 4), Not(blockSpaceRule))
	prefix := Sequence(
		blockPrefixRule, Or(ordered, unordered),
		Lookahead(Or(blockSpaceRule, blockLineEndRule)),
		OptionalCapture("listItemPrefixWhitespace", Or(padding, blockSpaceRule)),
	)
	return blockNative("listItemPrefix", prefix, blockListAction)
}

func blockListAction(context *TokenizeContext, token *Token, match *blockSyntaxMatch) {
	marker := blockCaptured(token, "listItemMarker")
	value := blockCaptured(token, "listItemValue")
	prefix := blockCaptured(token, "linePrefix")
	m := blockListMarker{indent: context.visual - 1, empty: blockBlank(match.body.text)}
	markerColumns := len(marker.view.text)
	if value != nil {
		m.ordered = true
		m.start, _ = strconv.Atoi(value.view.text)
		m.delimiter = marker.view.text[0]
		markerColumns += len(value.view.text)
	} else {
		m.marker = marker.view.text[0]
	}
	if blockCaptured(token, "listItemPrefixWhitespace") == nil {
		m.indent++
	}
	if m.empty {
		m.indent = markerColumns + 1
		if prefix != nil {
			_, columns := blockIndent(prefix.view)
			m.indent += columns
		}
	}
	match.marker = m
}

func blockInterruptListRule(rule Rule) Rule {
	return Sequence(Not(blockThematicRule()), rule, Guard(canInterruptBlockList))
}

func canInterruptBlockList(context *TokenizeContext) bool {
	if !context.Interrupt {
		return true
	}
	root := context.roots[len(context.roots)-1]
	match, _ := root.Data["nativeBlock"].(*blockSyntaxMatch)
	if match == nil {
		return false
	}
	owner := context.parser.blockOwner
	if owner != nil && owner.node != nil && owner.node.Kind == KindList {
		return true
	}
	return !match.marker.empty && (!match.marker.ordered || match.marker.start == 1)
}

func blockThematicRule() Rule {
	alternatives := make([]Rule, 0, 3)
	for _, marker := range []string{"*", "_", "-"} {
		unit := Sequence(Capture("thematicBreakSequence", String(marker)), blockWhitespaceRule)
		alternatives = append(alternatives, Sequence(Repeat(unit, 3, 0), blockLineEndRule))
	}
	return blockNative("thematicBreak", Sequence(blockPrefixRule, Or(alternatives...), blockEndingRule("lineEnding")), nil)
}

func blockATXRule() Rule {
	hashes := Capture("atxHeadingSequence", Some(String("#")))
	closing := Sequence(Some(blockSpaceRule), Some(String("#")), blockTrailingRule)
	trailing := Sequence(Some(blockSpaceRule), blockLineEndRule)
	content := Capture("chunkText", Until(Or(Lookahead(closing), Lookahead(trailing), blockLineEndRule)))
	body := Or(
		Sequence(hashes, blockTrailingRule),
		Sequence(content, Optional(Sequence(blockWhitespaceRule, hashes)), blockWhitespaceRule),
	)
	line := Sequence(
		blockPrefixRule, Capture("atxHeadingSequence", Repeat(String("#"), 1, 6)),
		Lookahead(Or(blockSpaceRule, blockLineEndRule)), blockWhitespaceRule,
		body, blockLineEndRule,
	)
	heading := blockNative("atxHeading", line, blockATXAction)
	return Sequence(heading, blockEndingRule("lineEnding"))
}

func blockATXAction(context *TokenizeContext, token *Token, match *blockSyntaxMatch) {
	match.level = len(blockCaptured(token, "atxHeadingSequence").view.text)
	n := context.Node(token)
	n.Kind, n.Level = KindHeading, match.level
	if content := blockCaptured(token, "chunkText"); content != nil {
		n.input = content.view
	} else {
		n.input = token.view.slice(len(token.view.text), len(token.view.text))
	}
}

func blockSetextRule() Rule {
	underline := Or(
		Capture("setextHeadingLineSequence", Some(String("="))),
		Capture("setextHeadingLineSequence", Some(String("-"))),
	)
	line := Sequence(
		Guard(func(context *TokenizeContext) bool { return context.parser.blockParagraph }),
		blockPrefixRule, underline, blockWhitespaceRule,
		blockLineEndRule,
	)
	heading := blockNative("setextHeadingLine", line, blockSetextAction)
	return Sequence(heading, blockEndingRule("lineEnding"))
}

func blockSetextAction(_ *TokenizeContext, token *Token, match *blockSyntaxMatch) {
	match.level = 2
	if blockCaptured(token, "setextHeadingLineSequence").view.text[0] == '=' {
		match.level = 1
	}
}

func blockFenceRule() Rule {
	word := Some(Satisfy(func(code Code) bool { return !blockSpaceCode(code) && !blockEndingCode(code) }))
	metaEnd := Or(Lookahead(Sequence(Some(blockSpaceRule), blockLineEndRule)), blockLineEndRule)
	info := Sequence(
		blockWhitespaceRule, OptionalCapture("codeFencedFenceInfo", word, Content(ContentString)),
		blockWhitespaceRule, OptionalCapture("codeFencedFenceMeta", Until(metaEnd), Content(ContentString)),
		blockWhitespaceRule,
	)
	alternatives := make([]Rule, 0, 2)
	for _, marker := range []string{"`", "~"} {
		body := info
		if marker == "`" {
			containsBacktick := Sequence(Until(Or(String("`"), blockLineEndRule)), String("`"))
			body = Sequence(Not(containsBacktick), info)
		}
		alternatives = append(alternatives, Sequence(
			Capture("codeFencedFenceSequence", Repeat(String(marker), 3, 0)), body, blockLineEndRule,
		))
	}
	line := Sequence(blockPrefixRule, Capture("codeFencedFence", Or(alternatives...)), blockEndingRule("lineEnding"))
	return blockNative("codeFenced", line, blockFenceAction)
}

func blockFenceAction(context *TokenizeContext, token *Token, match *blockSyntaxMatch) {
	sequence := blockCaptured(token, "codeFencedFenceSequence")
	match.fence, match.fenceSize = sequence.view.text[0], len(sequence.view.text)
	if prefix := blockCaptured(token, "linePrefix"); prefix != nil {
		_, match.indent = blockIndent(prefix.view)
	}
	n := context.Node(token)
	n.Kind, n.Data = KindCodeBlock, map[string]any{"closed": false}
	if info := blockCaptured(token, "codeFencedFenceInfo"); info != nil {
		n.Info = context.parser.decodeString(info.view.text)
	}
	if meta := blockCaptured(token, "codeFencedFenceMeta"); meta != nil {
		n.Meta = context.parser.decodeString(meta.view.text)
	}
	n.Data["language"] = n.Info
}

func blockFenceClosingRule(marker byte, size int, math bool) Rule {
	fence, sequence := TokenType("codeFencedFence"), TokenType("codeFencedFenceSequence")
	if math {
		fence = "mathFlowFence"
		sequence = "mathFlowFenceSequence"
	}
	return Capture(fence, Sequence(blockPrefixRule, Capture(sequence, Repeat(String(string(marker)), size, 0)), blockWhitespaceRule, blockLineEndRule, blockEndingRule("lineEnding")))
}

func blockHTMLTagRule() Rule {
	letter := Satisfy(func(code Code) bool { return code >= 'a' && code <= 'z' || code >= 'A' && code <= 'Z' })
	name := Sequence(letter, Many(Or(letter, Runes("0123456789-"))))
	attributeName := Sequence(Or(letter, Runes("_:")), Many(Or(letter, Runes("0123456789_.:-"))))
	quoted := func(marker string) Rule {
		return Sequence(String(marker), Many(Satisfy(func(code Code) bool { return code != Code(marker[0]) && !blockEndingCode(code) })), String(marker))
	}
	value := Or(quoted("\""), quoted("'"), Some(Satisfy(func(code Code) bool {
		return !blockSpaceCode(code) && !blockEndingCode(code) && !strings.ContainsRune("\"'=<>`", rune(code))
	})))
	attribute := Sequence(Some(blockSpaceRule), attributeName, Optional(Sequence(blockSpacesRule, String("="), blockSpacesRule, value)))
	opening := Sequence(String("<"), name, Many(attribute), blockSpacesRule, Optional(String("/")), String(">"))
	closing := Sequence(String("</"), name, blockSpacesRule, String(">"))
	return Sequence(Or(opening, closing), blockSpacesRule, blockLineEndRule)
}

var blockHTMLNames = []string{"address", "article", "aside", "base", "basefont", "blockquote", "body", "caption", "center", "col", "colgroup", "dd", "details", "dialog", "dir", "div", "dl", "dt", "fieldset", "figcaption", "figure", "footer", "form", "frame", "frameset", "h1", "h2", "h3", "h4", "h5", "h6", "head", "header", "hr", "html", "iframe", "legend", "li", "link", "main", "menu", "menuitem", "nav", "noframes", "ol", "optgroup", "option", "p", "param", "search", "section", "summary", "table", "tbody", "td", "tfoot", "th", "thead", "title", "tr", "track", "ul"}

func blockHTMLRule() Rule {
	boundary := Lookahead(Or(blockSpaceRule, String(">"), blockLineEndRule))
	starts := make([]Rule, 0, 7)
	start := func(class int, end string, rule Rule) Rule {
		return Capture("htmlFlowData", Sequence(Lookahead(rule), blockRestRule), func(_ *TokenizeContext, token *Token) error {
			token.Data = map[string]any{"class": class, "end": end}
			return nil
		})
	}
	for _, name := range []string{"script", "pre", "style", "textarea"} {
		starts = append(starts, start(1, "</"+name+">", Sequence(FoldedString("<"+name), boundary)))
	}
	starts = append(starts, start(2, "-->", String("<!--")), start(3, "?>", String("<?")), start(4, ">", Sequence(String("<!"), Runes("ABCDEFGHIJKLMNOPQRSTUVWXYZ"))), start(5, "]]>", String("<![CDATA[")))
	names := make([]Rule, 0, len(blockHTMLNames))
	for _, name := range blockHTMLNames {
		names = append(names, Sequence(FoldedString(name), Lookahead(Or(blockSpaceRule, String(">"), String("/>"), blockLineEndRule))))
	}
	starts = append(starts, start(6, "", Sequence(String("<"), Optional(String("/")), Or(names...))), start(7, "", Sequence(Guard(func(context *TokenizeContext) bool { return !context.Interrupt }), blockHTMLTagRule())))
	return blockNative("htmlFlow", Sequence(blockPrefixRule, Or(starts...), blockEndingRule("lineEnding")), blockHTMLAction)
}

func blockHTMLAction(context *TokenizeContext, token *Token, match *blockSyntaxMatch) {
	data := blockCaptured(token, "htmlFlowData")
	match.htmlClass, _ = data.Data["class"].(int)
	match.htmlEnd, _ = data.Data["end"].(string)
	context.Node(token).Kind = KindHTMLBlock
}

func blockHTMLTerminatorRule(class int, end string) Rule {
	terminator := String(end)
	if class == 1 {
		terminator = FoldedString(end)
	}
	return Sequence(Until(terminator), terminator)
}

func blockParagraphRule() Rule {
	line := Sequence(
		Guard(func(context *TokenizeContext) bool { return !context.Interrupt }),
		blockPrefixRule, Capture("chunkText", Sequence(Not(blockLineEndRule), blockRestRule)),
	)
	paragraph := blockNative("paragraph", line, blockParagraphAction)
	return Sequence(paragraph, blockEndingRule("lineEnding"))
}

func blockParagraphAction(context *TokenizeContext, token *Token, _ *blockSyntaxMatch) {
	n := context.Node(token)
	n.Kind, n.input = KindParagraph, blockCaptured(token, "chunkText").view
}

// Definitions permit one physical ending in each separating whitespace run.
// Optional titles are a checkpoint: an invalid next-line title must leave that
// next line available to paragraph parsing, while preserving the destination.
func blockDefinitionRule() Rule {
	ending := Sequence(Satisfy(blockEndingCode), blockSpacesRule, Not(Satisfy(blockEndingCode)))
	ordinary := Satisfy(func(code Code) bool { return code != '[' && code != ']' && !blockEndingCode(code) })
	labelBody := Some(Or(inlineEscapeRule, ending, ordinary))
	label := Capture("definitionLabel", Sequence(
		Capture("definitionLabelMarker", String("[")),
		BindCapture("definitionLabelString", labelBody, func(_ *TokenizeContext, label *Token) Rule {
			return Guard(func(*TokenizeContext) bool {
				return utf16Length(label.view.text) <= 999 && normalizeIdentifier(label.view.text) != ""
			})
		}, Content(ContentString)),
		Capture("definitionLabelMarker", String("]")),
	))
	space := Capture("whitespace", Sequence(
		Lookahead(Or(blockSpaceRule, Satisfy(blockEndingCode))), blockDefinitionSpaceRule,
	))
	title := Sequence(space, titleRule("definitionTitle"), blockWhitespaceRule, blockLineEndRule)
	definition := Sequence(
		blockPrefixRule, label, Capture("definitionMarker", String(":")), Optional(space),
		destinationRule("definitionDestination", false), Optional(title),
		blockWhitespaceRule, blockLineEndRule, blockEndingRule("lineEnding"),
	)
	return blockNative("definition", definition, blockDefinitionAction)
}

func blockDefinitionAction(context *TokenizeContext, token *Token, _ *blockSyntaxMatch) {
	n := context.Node(token)
	n.Kind, n.Label = KindDefinition, blockCaptured(token, "definitionLabelString").view.text
	n.Identifier = normalizeIdentifier(n.Label)
	destination := blockCaptured(token, "definitionDestinationString")
	destination.ContentType = ContentString
	n.Destination = context.parser.decodeString(destination.view.text)
	if title := blockCaptured(token, "definitionTitleString"); title != nil {
		title.ContentType = ContentString
		n.Title = context.parser.decodeString(inlineDropLinePrefixes(title.view.text))
	}
	if ending := blockCaptured(token, "lineEnding"); ending != nil {
		n.ending = ending.view
	}
}

func mathBlockRule() Rule {
	closing := Sequence(String("$$"), blockTrailingRule)
	value := Capture("mathFlowValue", Until(Or(Lookahead(closing), blockLineEndRule)))
	body := Or(
		Sequence(blockWhitespaceRule, blockLineEndRule),
		Sequence(value, Capture("mathFlowFence", String("$$")), blockWhitespaceRule, blockLineEndRule),
	)
	line := Sequence(blockPrefixRule, Capture("mathFlowFence", String("$$")), body, blockEndingRule("lineEnding"))
	return blockNative("mathFlow", line, blockMathAction)
}

func blockMathAction(context *TokenizeContext, token *Token, match *blockSyntaxMatch) {
	n := context.Node(token)
	n.Kind = KindMathBlock
	if value := blockCaptured(token, "mathFlowValue"); value != nil {
		n.Literal, match.closed = value.view.text, true
	}
	if prefix := blockCaptured(token, "linePrefix"); prefix != nil {
		_, match.indent = blockIndent(prefix.view)
	}
	match.fence, match.fenceSize = '$', 2
	n.Data = map[string]any{"closed": match.closed}
}
