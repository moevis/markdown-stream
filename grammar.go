package markdown

import "strings"

// This file is the grammar declaration manifest: shared matchers, syntax
// elements, and phase registration live here so the whole grammar can be read
// in one place. Rule bodies stay in block_rules.go, inline_grammar.go,
// inline_resource.go, gfm_block.go, and gfm_inline.go.

// Shared block matchers.
var (
	blockSpaceRule      = Satisfy(blockSpaceCode)
	blockSpacesRule     = Repeat(blockSpaceRule, 0, 0)
	blockLineEndRule    = Or(End(), Lookahead(Satisfy(blockEndingCode)))
	blockPrefixRule     = Optional(Capture("linePrefix", Repeat(blockSpaceRule, 1, 3)))
	blockWhitespaceRule = Optional(Capture("whitespace", Repeat(blockSpaceRule, 1, 0)))
	blockRestRule       = Until(blockLineEndRule)
	blockTrailingRule   = Sequence(blockSpacesRule, blockLineEndRule)
)

// Single-expression block productions.
var (
	blockQuoteRule = blockNative("blockQuotePrefix", Sequence(
		blockPrefixRule,
		Capture("blockQuoteMarker", String(">")),
		Optional(Capture("blockQuotePrefixWhitespace", blockSpaceRule)),
	), nil)

	blockIndentedRule = blockNative("codeIndented", Sequence(
		Guard(func(context *TokenizeContext) bool { return !context.Interrupt }),
		Capture("linePrefix", Repeat(blockSpaceRule, 4, 4)),
		Capture("codeFlowValue", blockRestRule),
		blockEndingRule("lineEnding"),
	), func(context *TokenizeContext, token *Token, _ *blockSyntaxMatch) {
		context.Node(token).Kind = KindCodeBlock
	})

	blockDefinitionSpaceRule = Sequence(
		blockSpacesRule,
		Optional(Sequence(Satisfy(blockEndingCode), blockSpacesRule, Not(Satisfy(blockEndingCode)))),
	)

	blockListInterruptRule = blockInterruptListRule(blockListRule())
)

// Core block elements. Each *Elem names a Construct and its Rule; the phase
// table below registers them by name, like the old MarkdownElem table.
var (
	blockQuoteElem = Construct{Name: "blockQuote", Rule: blockQuoteRule, native: true}
	listElem       = Construct{Name: "list", Rule: blockListInterruptRule, native: true}

	setextHeadingElem = Construct{Name: "setextHeading", Rule: blockSetextRule(), native: true}
	thematicBreakElem = Construct{Name: "thematicBreak", Rule: blockThematicRule(), native: true}
	atxHeadingElem    = Construct{Name: "atxHeading", Rule: blockATXRule(), native: true}
	codeFencedElem    = Construct{Name: "codeFenced", Rule: blockFenceRule(), Concrete: true, native: true}
	htmlFlowElem      = Construct{Name: "htmlFlow", Rule: blockHTMLRule(), Concrete: true, native: true}
	codeIndentedElem  = Construct{Name: "codeIndented", Rule: blockIndentedRule, Concrete: true, native: true}
	paragraphElem     = Construct{Name: "paragraph", Rule: blockParagraphRule(), AddAfter: true, native: true}
	definitionElem    = Construct{Name: "definition", Rule: blockDefinitionRule(), native: true}
)

var commonMarkBlocks = makeCommonMarkBlockSyntax()

func commonMarkBlockSyntax() SyntaxExtension { return commonMarkBlocks }

// Core block grammar. Container margins and continuation stay in the driver;
// each production recognizes one line (or one leaf) with Rule combinators.
// Spaces include tab padding; prefixes are visual columns. EBNF: | alternation,
// ? optional, * 0+, + 1+, {m,n} bounded repeat, ; note.
//
//	blockQuote    = linePrefix{0,3} ">" space?
//	listItem      = linePrefix{0,3} (digit{1,9} [.)] | [-+*]) (space{1,4} | space)
//	thematicBreak = linePrefix{0,3} ("*" space?){3,} lineEnd
//	              | linePrefix{0,3} ("_" space?){3,} lineEnd
//	              | linePrefix{0,3} ("-" space?){3,} lineEnd
//	atxHeading    = linePrefix{0,3} "#"{1,6} (space | lineEnd) text? closing? lineEnd
//	setextHeading = linePrefix{0,3} ("="+ | "-"+) space* lineEnd   ; needs an open paragraph
//	codeFenced    = linePrefix{0,3} ("`"{3,} | "~"{3,}) info? meta? lineEnd
//	codeIndented  = space{4} rest
//	htmlFlow      = linePrefix{0,3} htmlStart rest                 ; classes 1-7
//	paragraph     = linePrefix{0,3} text
//	definition    = linePrefix{0,3} label ":" space? destination title? lineEnd
func makeCommonMarkBlockSyntax() SyntaxExtension {
	return SyntaxExtension{
		Document: map[Code][]Construct{Fallback: {blockQuoteElem, listElem}},
		Flow: map[Code][]Construct{Fallback: {
			setextHeadingElem,
			thematicBreakElem,
			atxHeadingElem,
			codeFencedElem,
			htmlFlowElem,
			codeIndentedElem,
			paragraphElem,
		}},
		ContentInitial: map[Code][]Construct{Fallback: {definitionElem}},
	}
}

// Math block element and registration.
var mathFlowElem = Construct{Name: "mathFlow", Rule: mathBlockRule(), Concrete: true, native: true}

var mathBlocks = SyntaxExtension{Flow: map[Code][]Construct{Fallback: {mathFlowElem}}, features: featureMath}

func mathBlockSyntax() SyntaxExtension { return mathBlocks }

// Shared inline matchers.
var (
	inlineEndingRule = Satisfy(func(code Code) bool {
		return code == LineFeed || code == CarriageReturn || code == CarriageReturnLineFeed
	})
	inlineSpaceRule      = Satisfy(func(code Code) bool { return code == ' ' || code == HorizontalTab || code == VirtualSpace })
	inlineWhitespaceRule = Repeat(Or(Capture("lineEnding", inlineEndingRule), Capture("linePrefix", Repeat(inlineSpaceRule, 1, 0))), 0, 0)
	inlineEscapeRule     = Sequence(String("\\"), Satisfy(func(code Code) bool { return code >= 0 && code < 128 && isASCIIPunctuation(byte(code)) }))
)

// Core inline elements, registered by code below.
var (
	characterEscapeElem    = inlineNative("characterEscape", characterEscapeRule())
	characterReferenceElem = inlineNative("characterReference", characterReferenceRule())
	lineEndingElem         = inlineNative("lineEnding", Sequence(Capture("lineEnding", inlineEndingRule, inlineLiteralAction(KindSoftBreak, nil)),
		Optional(Capture("linePrefix", Repeat(inlineSpaceRule, 1, 0)))))
	stringLineEndingElem    = inlineNative("lineEnding", Sequence(Capture("lineEnding", inlineEndingRule), Optional(Capture("linePrefix", Repeat(inlineSpaceRule, 1, 0)))))
	hardBreakEscapeElem     = inlineNative("hardBreakEscape", hardBreakEscapeRule())
	codeTextElem            = inlineNative("codeText", codeTextRule('`', "codeText", KindCodeSpan))
	attentionStarElem       = inlineNative("attention", attentionRule('*', 0))
	attentionUnderscoreElem = inlineNative("attention", attentionRule('_', 0))
	labelStartLinkElem      = inlineNative("labelStartLink", labelStartLinkRule())
	labelStartImageElem     = inlineNative("labelStartImage", labelStartImageRule())
	labelEndElem            = inlineNative("labelEnd", labelEndRule())
	autolinkElem            = inlineNative("autolink", angleAutolinkRule())
	htmlTextElem            = inlineNative("htmlText", htmlTextRule())
	hardBreakTrailingElem   = inlineNative("hardBreakTrailing", hardBreakTrailingRule())
	dataElem                = Construct{Name: "data", Rule: ordinaryDataRule(), AddAfter: true, native: true}
	mathTextElem            = inlineNative("mathText", codeTextRule('$', "mathText", KindMathInline))
)

// Core inline grammar. Delimiter pairing and bracket resolution run only after
// the complete Rule tree commits (inline.go, inline_state.go).
// EBNF: | alternation, ? optional, * 0+, + 1+, {m,n} bounded repeat, ; note.
//
//	escape    = "\" asciiPunctuation
//	reference = "&" (numeric | named) ";"
//	codeSpan  = "`"{n} text "`"{n}                                  ; equal fence length
//	attention = "*"+ | "_"+
//	labelEnd  = "]" ( resource | reference | collapsed | shortcut )
//	autolink  = "<" ( scheme ":" ... | email ) ">"
//	htmlText  = comment | processing | CDATA | declaration | closeTag | openTag
//	data      = rune that starts no higher-priority construct
func commonMarkInlineSyntax() SyntaxExtension {
	return SyntaxExtension{
		Text: map[Code][]Construct{
			'\\':     {hardBreakEscapeElem, characterEscapeElem},
			'&':      {characterReferenceElem},
			'`':      {codeTextElem},
			'*':      {attentionStarElem},
			'_':      {attentionUnderscoreElem},
			'[':      {labelStartLinkElem},
			'!':      {labelStartImageElem},
			']':      {labelEndElem},
			'<':      {autolinkElem, htmlTextElem},
			LineFeed: {lineEndingElem}, CarriageReturn: {lineEndingElem}, CarriageReturnLineFeed: {lineEndingElem},
			' ':      {hardBreakTrailingElem},
			Fallback: {dataElem},
		},
		String: map[Code][]Construct{
			'\\':     {characterEscapeElem},
			'&':      {characterReferenceElem},
			LineFeed: {stringLineEndingElem}, CarriageReturn: {stringLineEndingElem}, CarriageReturnLineFeed: {stringLineEndingElem},
			Fallback: {dataElem},
		},
		AttentionMarkers: []Code{'*', '_'},
	}
}

func mathInlineSyntax() SyntaxExtension {
	return SyntaxExtension{Text: map[Code][]Construct{'$': {mathTextElem}}, features: featureMath}
}

// Core syntax assembly (block + inline), consumed by initSyntax.
var coreSyntaxes = [2]SyntaxExtension{commonMarkBlockSyntax(), commonMarkInlineSyntax()}

// GFM block elements and registration.
var (
	gfmFootnoteDefinitionElem = Construct{Name: "gfmFootnoteDefinition", Rule: footnoteDefinitionRule(), native: true}
	tableElem                 = Construct{Name: "table", Rule: tableRule(), AddAfter: true, native: true}
	taskListCheckElem         = Construct{Name: "tasklistCheck", Rule: taskListRule(), native: true}
	tableBodyRowSyntax        = tableRowRule("tableRow")
)

var gfmBlocks = makeGFMBlockSyntax()

func gfmBlockSyntax() SyntaxExtension { return gfmBlocks }

// GFM block grammar.
// EBNF: | alternation, ? optional, * 0+, + 1+, {m,n} bounded repeat, ; note.
//
//	table              = tableRow line tableDelimiterRow line tableRow*
//	footnoteDefinition = linePrefix{0,3} "[^" label "]:" space?
//	taskListCheck      = "[ ]" | "[x]" | "[X]" | "[" lineEnding "]" ; first paragraph of an item
func makeGFMBlockSyntax() SyntaxExtension {
	return SyntaxExtension{
		Flow:           map[Code][]Construct{Fallback: {gfmFootnoteDefinitionElem, tableElem}},
		ContentInitial: map[Code][]Construct{Fallback: {taskListCheckElem}},
		features:       featureGFM,
	}
}

// GFM inline grammar. singleTilde lowers the strikethrough fence from 2 to 1.
// EBNF: | alternation, ? optional, * 0+, + 1+, {m,n} bounded repeat, ; note.
//
//	strikethrough = "~"{2,3} text "~"{2,3}          ; "~"{1,2} when singleTilde
//	footnoteCall  = "[^" label "]"                 ; label must be defined
//	email         = local "@" domain ("." domain)+
//	www           = "www." domain path?
//	protocol      = ("http://" | "https://") domain path?
func gfmInlineSyntax(single bool) SyntaxExtension {
	footnote := inlineNative("gfmFootnoteCall", footnoteCallRule())
	minimum := 2
	if single {
		minimum = 1
	}
	strikethrough := inlineNative("strikethrough", Sequence(
		Guard(func(context *TokenizeContext) bool {
			return context.index == 0 || context.view.text[context.index-1] != '~' || context.inline != nil && context.inline.items.tail != nil && context.inline.items.tail.token.Type == "characterEscape"
		}),
		Capture("strikethroughSequence", Repeat(String("~"), minimum, 2), inlineLiteralAction(KindText, nil)),
		Not(String("~")),
	))
	extension := SyntaxExtension{
		Text: map[Code][]Construct{
			'~': {strikethrough},
			'[': {footnote},
		},
		AttentionMarkers: []Code{'~'}, features: featureGFM,
	}
	email := inlineNative("emailAutolink", literalEmailRule())
	email.Previous = func(code Code) bool { return code != '/' && !gfmAtext(code) }
	for code := Code(0); code < 128; code++ {
		if gfmAtext(code) {
			extension.Text[code] = append(extension.Text[code], email)
		}
	}
	www := inlineNative("wwwAutolink", literalURLRule(true))
	www.Previous = func(code Code) bool {
		return code == EOF || inlineCodeWhitespace(code) || strings.ContainsRune("(*_[]~", rune(code))
	}
	for _, code := range [2]Code{'w', 'W'} {
		extension.Text[code] = append(extension.Text[code], www)
	}
	protocol := inlineNative("protocolAutolink", literalURLRule(false))
	protocol.Previous = func(code Code) bool { return !inlineAlphaCode(code) }
	for _, code := range [2]Code{'h', 'H'} {
		extension.Text[code] = append(extension.Text[code], protocol)
	}
	return extension
}
