package markdown

import (
	"fmt"
	"slices"
)

type ElemType string

var (
	CodeElemType        ElemType = "code"
	EquationElemType    ElemType = "equation"
	HeaderElemType      ElemType = "header"
	ImageElemType       ElemType = "image"
	IndexRefElemType    ElemType = "reference"
	InlineCodeElemType  ElemType = "inline-code"
	InlineEquationType  ElemType = "inline-equation"
	ItalicElemType      ElemType = "italic"
	LinkElemType        ElemType = "link"
	ListElemType        ElemType = "list"
	MarkdownDocElemType ElemType = "doc"
	ParagraphElemType   ElemType = "paragraph"
	QuoteElemType       ElemType = "quote"
	StrongElemType      ElemType = "strong"
	TableElemType       ElemType = "table"
	WhitespaceElemType  ElemType = "whitespace"
)

func (e ElemType) Text() ElemType {
	return ElemType(e + ".text")
}

func (e ElemType) Prefix() ElemType {
	return ElemType(e + ".prefix")
}

func (e ElemType) Suffix() ElemType {
	return ElemType(e + ".suffix")
}

func (e ElemType) SubType(name string) ElemType {
	return ElemType(fmt.Sprintf("%s.%s", e, name))
}

func (e ElemType) IsEmpty() bool {
	return len(e) == 0
}

// MarkdownElem describes an element's start, body, children, and end.
// Elements and their slices must remain unchanged while being parsed.
type MarkdownElem struct {
	Type   ElemType // 只是用来标记的，不会依赖这个字段来判断类型
	Inline bool     // 行内元素在 CR/LF 前结束，不消费行边界

	Tokenizer Tokenizer

	// TrimWhites labels matching body runes as whitespace; it does not remove them.
	TrimWhites []rune

	// 子元素按顺序尝试，第一个实际消费输入的匹配优先。
	SubElems []MarkdownElem

	// TextElemType labels body runes. With no body type or children, matching is atomic.
	TextElemType ElemType

	SuffixPredictor Tokenizer
}

func (m *MarkdownElem) foundSuffix(ctx markdownCtx) bool {
	if m.SuffixPredictor == nil {
		return false
	}
	segs, matched := m.SuffixPredictor(ctx)
	if matched {
		for _, s := range segs {
			ctx.OutputRune(s.Rune, s.Type)
		}
	}
	return matched
}

func (m *MarkdownElem) processBody(ctx markdownCtx) {
	for {
		c := ctx.Peek()
		if c == EOF || (m.Inline && isLineBreak(c)) {
			return
		}
		if m.foundSuffix(ctx) {
			return
		}
		if slices.Contains(m.TrimWhites, c) {
			ctx.OutputRune(ctx.Next(), WhitespaceElemType)
			continue
		}
		if len(m.SubElems) > 0 && isLineBreak(c) {
			ctx.OutputRune(c, WhitespaceElemType)
			ctx.Next()
			continue
		}
		if c == '\\' && !m.TextElemType.IsEmpty() && (m.Inline || len(m.SubElems) > 0) {
			ctx.OutputRune(ctx.Next(), m.TextElemType)
			next := ctx.Peek()
			if next != EOF && !isLineBreak(next) {
				ctx.OutputRune(ctx.Next(), m.TextElemType)
			}
			continue
		}
		matched := false
		for i := range m.SubElems {
			if m.SubElems[i].Process(ctx) {
				matched = true
				break
			}
		}
		if !matched {
			ctx.OutputRune(ctx.Next(), m.TextElemType)
		}
	}
}

func (m *MarkdownElem) Process(ctx markdownCtx) bool {
	if ctx.Peek() == EOF {
		return false
	}
	start := ctx.PushState()
	if m.Tokenizer != nil {
		segs, matched := m.Tokenizer(ctx)
		if !matched {
			return false
		}
		for _, seg := range segs {
			ctx.OutputRune(seg.Rune, seg.Type)
		}
	} else if len(m.SubElems) == 0 {
		return false
	}
	if len(m.SubElems) > 0 || !m.TextElemType.IsEmpty() {
		m.processBody(ctx)
	}
	return ctx.PushState().index > start.index
}

func isLineBreak(r rune) bool {
	return r == '\r' || r == '\n'
}
