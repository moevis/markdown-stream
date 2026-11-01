package markdown

// Primary elements 主要的 markdown 元素
var (
	// 由 # 开头的标题，最多支持 7 个 #，即最大支持 h1 ~ h7，结束由 \n 结束
	HeaderElem = MarkdownElem{
		Type:      HeaderElemType,
		Tokenizer: Repeat(HeaderElemType.Prefix(), '#', 1, 7),
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
	// 表格块，由 | 开头，由 \n 结束，至少有三行
	TableElem = MarkdownElem{
		Type:            TableElemType,
		Tokenizer:       Sequence(MatchTableHead(), MatchTableAlignRow()),
		SubElems:        []MarkdownElem{LinkElem, IndexElem},
		SuffixPredictor: NotTableRow(),
		TextElemType:    TableElemType.Text(),
	}
	// 公式块，由 $$ 开头，$$ 结束，非行内元素
	EquationElem = MarkdownElem{
		Type:            EquationElemType,
		Tokenizer:       String(EquationElemType, "$$"),
		TextElemType:    EquationElemType.Text(),
		SuffixPredictor: String(EquationElemType.Suffix(), "$$"),
	}
	// 代码块，由 ``` 开头，``` 结束，非行内元素
	CodeElem = MarkdownElem{
		Type: CodeElemType,
		Tokenizer: Sequence(
			String(CodeElemType.Prefix(), "```"),
			UntilRunes(CodeElemType.SubType("syntax"), "`\n"), // 遇上 ` 或者 \n 就结束
		),
		TextElemType:    CodeElemType.Text(),
		SuffixPredictor: String(CodeElemType.Suffix(), "```"),
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
		TrimWhites: []rune{' ', '\t', '\n'},
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
