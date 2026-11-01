package markdown

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
		Tokenizer:       String(ItalicElemType.Prefix(), "*"),
		SubElems:        []MarkdownElem{IndexElem, LinkElem},
		TextElemType:    ItalicElemType.Text(),
		SuffixPredictor: String(ItalicElemType.Suffix(), "*"),
		Inline:          true,
	}

	// 使用 _ 包裹的斜体元素
	ItalicElem2 = MarkdownElem{
		Type:            ItalicElemType,
		Tokenizer:       String(ItalicElemType.Prefix(), "_"),
		SubElems:        []MarkdownElem{IndexElem, LinkElem},
		TextElemType:    ItalicElemType.Text(),
		SuffixPredictor: Runes(ItalicElemType.Suffix(), "_"),
		Inline:          true,
	}

	// 使用 ** 包裹的加粗元素
	StrongElem = MarkdownElem{
		Type:      StrongElemType,
		Tokenizer: String(StrongElemType.Prefix(), "**"),
		SubElems: []MarkdownElem{
			IndexElem,
			LinkElem,
			// ItalicElem, // 使用**加粗元素中不能包含*元素
			ItalicElem2,
		},
		TextElemType:    StrongElemType.Text(),
		SuffixPredictor: String(StrongElemType.Suffix(), "**"),
		Inline:          true,
	}

	// 使用 __ 包裹的加粗元素
	StrongElem2 = MarkdownElem{
		Type:      StrongElemType.Prefix(),
		Tokenizer: String(StrongElemType, "__"),
		SubElems: []MarkdownElem{
			IndexElem,
			LinkElem,
			ItalicElem,
			// ItalicElem2, // 使用__加粗元素中不能包含_元素
		},
		TextElemType:    StrongElemType.Text(),
		SuffixPredictor: String(StrongElemType.Suffix(), "__"),
		Inline:          true,
	}
)
