package markdown

// Primary elements 主要的 markdown 元素
var (
	// 标题支持 1–7 个 #；更多的 # 回退为普通文本。
	HeaderElem = MarkdownElem{
		Type:      HeaderElemType,
		Tokenizer: headerTokenizer(),
		SubElems: []MarkdownElem{
			StrongElem,
			StrongElem2,
			ItalicElem,
			ItalicElem2,
			LinkElem,
			ImageElem,
			IndexElem,
			InlineCodeElem,
			InlineEquationElem,
		},
		TextElemType: HeaderElemType.Text(),
		Inline:       true,
	}
	// 表格需要列数一致的表头和对齐行，在下一条非管道行之前结束。
	TableElem = MarkdownElem{
		Type:       TableElemType,
		Tokenizer:  matchTableStart(),
		TrimWhites: []rune{' ', '\t', '\r', '\n'},
		SubElems: []MarkdownElem{
			{Type: TableElemType.SubType("border"), Tokenizer: String(TableElemType.SubType("border"), "|")},
			LinkElem,
			IndexElem,
		},
		SuffixPredictor: NotTableRow(),
		TextElemType:    TableElemType.Text(),
	}
	// 公式块，由 $$ 开头，$$ 结束，非行内元素
	EquationElem = MarkdownElem{
		Type:            EquationElemType,
		Tokenizer:       String(EquationElemType.Prefix(), "$$"),
		TextElemType:    EquationElemType.Text(),
		SuffixPredictor: String(EquationElemType.Suffix(), "$$"),
	}
	// 代码结束围栏从行首开始，其后只能有空格、制表符和行尾。
	CodeElem = MarkdownElem{
		Type:            CodeElemType,
		Tokenizer:       codeStart(),
		TextElemType:    CodeElemType.Text(),
		SuffixPredictor: codeEnd(),
	}
	// 无序列表，由 - 开头，由 \n 结束
	ListElem = MarkdownElem{
		Type: ListElemType,
		Tokenizer: Or(
			String(ListElemType.Prefix(), "- "),
			String(ListElemType.Prefix(), "+ "),
			String(ListElemType.Prefix(), "* "),
		),
		SubElems: []MarkdownElem{
			StrongElem,
			StrongElem2,
			ItalicElem,
			ItalicElem2,
			LinkElem,
			ImageElem,
			IndexElem,
			InlineCodeElem,
			InlineEquationElem,
		},
		TextElemType: ListElemType.Text(),
		Inline:       true,
	}
	// 段落，由 \n 结束
	ParagraphElem = MarkdownElem{
		Type:      ParagraphElemType,
		Tokenizer: Any(),
		SubElems: []MarkdownElem{
			StrongElem,
			StrongElem2,
			ItalicElem,
			ItalicElem2,
			LinkElem,
			ImageElem,
			IndexElem,
			InlineCodeElem,
			InlineEquationElem,
		},
		TextElemType: ParagraphElemType.Text(),
		Inline:       true,
	}
	// 引用，由 > 开头，由 \n 结束
	QuoteElem = MarkdownElem{
		Type:      QuoteElemType,
		Tokenizer: String(QuoteElemType.Prefix(), "> "),
		SubElems: []MarkdownElem{
			StrongElem,
			StrongElem2,
			ItalicElem,
			ItalicElem2,
			LinkElem,
			ImageElem,
			IndexElem,
			InlineCodeElem,
			InlineEquationElem,
		},
		TextElemType: QuoteElemType.Text(),
		Inline:       true,
	}
	// 根文档，由多个元素组成，在输入结束后结束
	MarkdownDoc = MarkdownElem{
		Type:       MarkdownDocElemType,
		TrimWhites: []rune{' ', '\t', '\r', '\n'},
		SubElems: []MarkdownElem{
			HeaderElem,
			TableElem,
			CodeElem,
			EquationElem,
			ListElem,
			QuoteElem,
			ParagraphElem,
		},
	}
)

func headerTokenizer() Tokenizer {
	match := Repeat(HeaderElemType.Prefix(), '#', 1, 7)
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs, matched := match(ctx)
		if !matched {
			return nil, false
		}
		if ctx.Peek() == '#' {
			ctx.ResumeState(state)
			return nil, false
		}
		return segs, true
	}
}

func codeStart() Tokenizer {
	match := String(CodeElemType.Prefix(), "```")
	syntaxType := CodeElemType.SubType("syntax")
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		segs, matched := match(ctx)
		if !matched {
			return nil, false
		}
		for {
			c := ctx.Peek()
			if c == EOF || isLineBreak(c) {
				return segs, true
			}
			segs = append(segs, MarkdownSegment{Type: syntaxType, Rune: ctx.Next()})
		}
	}
}

func codeEnd() Tokenizer {
	match := String(CodeElemType.Suffix(), "```")
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		if state.previous != EOF && !isLineBreak(state.previous) {
			return nil, false
		}
		segs, matched := match(ctx)
		if !matched {
			return nil, false
		}
		for {
			c := ctx.Peek()
			if c == EOF || isLineBreak(c) {
				return segs, true
			}
			if c != ' ' && c != '\t' {
				ctx.ResumeState(state)
				return nil, false
			}
			segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: ctx.Next()})
		}
	}
}
