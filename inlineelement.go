package markdown

import "unicode"

var (
	// 能作为子元素出现的 Markdown 元素，包括链接、图片、索引，这些相对于 em/strong 等元素来说，是比较复杂的元素，需要 peek 一下后面的字符才能确定是否是这些元素
	LinkElem = MarkdownElem{
		Type:      LinkElemType,
		Tokenizer: LinkTokenizer,
		Inline:    true,
	}
	ImageElem = MarkdownElem{
		Type:      ImageElemType,
		Tokenizer: ImageTokenizer,
		Inline:    true,
	}
	IndexElem = MarkdownElem{
		Type:      IndexRefElemType,
		Tokenizer: IndexRefTokenizer,
		Inline:    true,
	}
	InlineEquationElem = MarkdownElem{
		Type: InlineEquationType,
		Tokenizer: Sequence(
			String(InlineEquationType.Prefix(), "$"),
			Until(InlineEquationType.Text(), '$'),
			String(InlineEquationType.Suffix(), "$"),
		),
		Inline: true,
	}

	InlineCodeElem = MarkdownElem{
		Type: InlineCodeElemType,
		Tokenizer: Sequence(
			String(InlineCodeElemType.Prefix(), "`"),
			Until(InlineCodeElemType.Text(), '`'),
			String(InlineCodeElemType.Suffix(), "`"),
		),
		Inline: true,
	}

	// 使用 * 包裹的斜体元素
	ItalicElem = MarkdownElem{
		Type:            ItalicElemType,
		Tokenizer:       emphasisStart(ItalicElemType.Prefix(), "*"),
		SubElems:        []MarkdownElem{IndexElem, LinkElem},
		TextElemType:    ItalicElemType.Text(),
		SuffixPredictor: emphasisEnd(ItalicElemType.Suffix(), "*"),
		Inline:          true,
	}

	// 使用 _ 包裹的斜体元素
	ItalicElem2 = MarkdownElem{
		Type:            ItalicElemType,
		Tokenizer:       emphasisStart(ItalicElemType.Prefix(), "_"),
		SubElems:        []MarkdownElem{IndexElem, LinkElem},
		TextElemType:    ItalicElemType.Text(),
		SuffixPredictor: emphasisEnd(ItalicElemType.Suffix(), "_"),
		Inline:          true,
	}

	// 使用 ** 包裹的加粗元素
	StrongElem = MarkdownElem{
		Type:      StrongElemType,
		Tokenizer: emphasisStart(StrongElemType.Prefix(), "**"),
		SubElems: []MarkdownElem{
			IndexElem,
			LinkElem,
			ItalicElem2,
		},
		TextElemType:    StrongElemType.Text(),
		SuffixPredictor: emphasisEnd(StrongElemType.Suffix(), "**"),
		Inline:          true,
	}

	// 使用 __ 包裹的加粗元素
	StrongElem2 = MarkdownElem{
		Type:      StrongElemType,
		Tokenizer: emphasisStart(StrongElemType.Prefix(), "__"),
		SubElems: []MarkdownElem{
			IndexElem,
			LinkElem,
			ItalicElem,
		},
		TextElemType:    StrongElemType.Text(),
		SuffixPredictor: emphasisEnd(StrongElemType.Suffix(), "__"),
		Inline:          true,
	}
)

func emphasisStart(elemType ElemType, marker string) Tokenizer {
	match := String(elemType, marker)
	underscore := marker[0] == '_'
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs, matched := match(ctx)
		if !matched {
			return nil, false
		}
		after := ctx.Peek()
		if after == EOF || unicode.IsSpace(after) || (underscore && isWordRune(state.previous) && isWordRune(after)) {
			ctx.ResumeState(state)
			return nil, false
		}
		return segs, true
	}
}

func emphasisEnd(elemType ElemType, marker string) Tokenizer {
	match := String(elemType, marker)
	underscore := marker[0] == '_'
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		if unicode.IsSpace(state.previous) {
			return nil, false
		}
		segs, matched := match(ctx)
		if !matched {
			return nil, false
		}
		if underscore && isWordRune(state.previous) && isWordRune(ctx.Peek()) {
			ctx.ResumeState(state)
			return nil, false
		}
		return segs, true
	}
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r)
}
