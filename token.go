package markdown

type Tokenizer func(ctx markdownCtx) ([]MarkdownSegment, bool)

// Any 匹配任意字符，除了换行符，但是不消费字符
func Any() Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		if ctx.Peek() == 0 || ctx.Peek() == '\n' {
			return nil, false
		}
		return []MarkdownSegment{}, true
	}
}

// String 匹配字符串序列
func String(elemType ElemType, s string) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs := []MarkdownSegment{}
		for _, r := range s {
			peek := ctx.Peek()
			if peek != r {
				ctx.ResumeState(state)
				return nil, false
			}

			segs = append(segs, MarkdownSegment{Type: elemType, Rune: r})
			ctx.Next()
		}
		return segs, true
	}
}

// Repeat 匹配重复的字符
func Repeat(elemType ElemType, target rune, minTimes, maxTimes int) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		segs := []MarkdownSegment{}
		state := ctx.PushState()
		for i := 0; i < maxTimes; i++ {
			if ctx.Peek() != target {
				if i < minTimes {
					ctx.ResumeState(state)
					return nil, false
				}
				return segs, true
			}
			segs = append(segs, MarkdownSegment{Type: elemType, Rune: target})
			ctx.Next()
		}
		if len(segs) == 0 {
			// 没有匹配到任何字符
			return nil, false
		}
		return segs, true
	}
}

// MatchRune 匹配字符集
func Runes(elemType ElemType, runes string) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs := []MarkdownSegment{}
		for _, r := range runes {
			if ctx.Peek() != r {
				ctx.ResumeState(state)
				return nil, false
			}
			segs = append(segs, MarkdownSegment{Type: elemType, Rune: r})
			ctx.Next()
		}
		return segs, true
	}
}

// Spaces 匹配空格和制表符
func Spaces(elemType ElemType) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		segs := []MarkdownSegment{}
		for {
			c := ctx.Peek()
			if c != ' ' && c != '\t' {
				return segs, true
			}
			segs = append(segs, MarkdownSegment{Type: elemType, Rune: c})
			ctx.Next()
		}
	}
}

func UntilRunes(elemType ElemType, runes string) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs := []MarkdownSegment{}
		for {
			c := ctx.Peek()
			if c == 0 {
				ctx.ResumeState(state)
				return nil, false
			}
			// 如果遇到转义字符，就吃掉转义字符和后面的字符
			if c == '\\' {
				ctx.Next()
				next := ctx.Next()
				// 如果转义符后面没有字符了，就跳出
				if next == 0 {
					ctx.ResumeState(state)
					return nil, false
				}
				segs = append(segs,
					MarkdownSegment{Type: elemType, Rune: '\\'},
					MarkdownSegment{Type: elemType, Rune: next},
				)
				continue
			}
			for _, r := range runes {
				if c == r {
					return segs, true
				}
			}
			// 如果遇到换行符，就跳出
			if c == '\n' {
				ctx.ResumeState(state)
				return nil, false
			}
			segs = append(segs, MarkdownSegment{Type: elemType, Rune: c})
			ctx.Next()
		}
	}
}

// Until 匹配直到遇到字符 r 为止，遇到转义字符 \ 会吃掉转义字符和后面的字符，遇到换行符会跳出
// 跳出指的是匹配失败
func Until(elemType ElemType, r rune) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs := []MarkdownSegment{}
		for {
			c := ctx.Peek()
			if c == 0 {
				ctx.ResumeState(state)
				return nil, false
			}
			// 如果遇到转义字符，就吃掉转义字符和后面的字符
			if c == '\\' {
				ctx.Next()
				next := ctx.Next()
				// 如果转义符后面没有字符了，就跳出
				if next == 0 {
					ctx.ResumeState(state)
					return nil, false
				}
				segs = append(segs,
					MarkdownSegment{Type: elemType, Rune: '\\'},
					MarkdownSegment{Type: elemType, Rune: next},
				)
				continue
			}
			if c == r {
				return segs, true
			}
			// 如果遇到换行符，就跳出
			if c == '\n' {
				ctx.ResumeState(state)
				return nil, false
			}
			segs = append(segs, MarkdownSegment{Type: elemType, Rune: c})
			ctx.Next()
		}
	}
}

// Or 匹配 branches 中的一个，如果有一个匹配成功，就返回
func Or(branches ...Tokenizer) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		for _, branch := range branches {
			segs, ok := branch(ctx)
			if ok {
				return segs, ok
			}
			ctx.ResumeState(state)
		}
		return nil, false
	}
}

// 线性匹配，按顺序匹配多个 matcher，如果有一个匹配失败，就跳出匹配
func Sequence(matchers ...Tokenizer) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs := []MarkdownSegment{}
		for _, matcher := range matchers {
			matchedSegs, ok := matcher(ctx)
			if !ok {
				ctx.ResumeState(state)
				return nil, false
			}
			segs = append(segs, matchedSegs...)
		}
		return segs, true
	}
}

func MatchTableHead() Tokenizer {
	borderType := TableElemType.SubType("border")
	headerText := TableElemType.SubType("text")
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs := []MarkdownSegment{}
		if ctx.Peek() != '|' {
			ctx.ResumeState(state)
			return nil, false
		}

		segs = append(segs, MarkdownSegment{Type: borderType, Rune: ctx.Peek()})
		ctx.Next() // 消耗掉 |

		colNum := 0

		for {
			r := ctx.Peek()
			if r == 0 {
				ctx.ResumeState(state)
				return nil, false
			}
			// 如果遇到换行符，就跳出
			if r == '\n' {
				if colNum == 0 {
					// 如果一列都没有，就跳出
					ctx.ResumeState(state)
					return nil, false
				}
				segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: r})
				// 消耗掉换行符
				ctx.Next()
				return segs, true
			} else if r == '|' {
				colNum++
				segs = append(segs, MarkdownSegment{Type: borderType, Rune: r})
			} else if r == ' ' || r == '\t' {
				segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: r})
			} else {
				segs = append(segs, MarkdownSegment{Type: headerText, Rune: r})
			}
			ctx.Next()
		}
	}
}

// 不那么严格的验证，只要以 | 开头，中间有 | 或者 : 或者 - 就可以，但是不能出现其他字符
func MatchTableAlignRow() Tokenizer {
	borderType := TableElemType.SubType("border")
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs := []MarkdownSegment{}
		r := ctx.Peek()
		if r != '|' {
			ctx.ResumeState(state)
			return nil, false
		}

		segs = append(segs, MarkdownSegment{Type: borderType, Rune: ctx.Peek()})
		ctx.Next() // 消耗掉 |

		for {
			r := ctx.Peek()
			if r == 0 {
				ctx.ResumeState(state)
				return nil, false
			}
			// 如果遇到换行符，就跳出
			if r == '\n' {
				return segs, true
			} else if r == '|' || r == ':' || r == '-' {
				segs = append(segs, MarkdownSegment{Type: borderType, Rune: r})
			} else if r == ' ' || r == '\t' {
				segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: r})
			}
			ctx.Next()
		}
	}
}

// 用于判断下一行是不是表格行，如果是，MarkdownElem.SuffixPredictor 会调用这个函数，如果返回 true，表示到了表格的结尾
func NotTableRow() Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		c := ctx.Peek()
		if c == 0 {
			// 如果已经到了文件末尾，就返回 true，表示到了表格的结尾
			return nil, true
		}
		// 如果当前字符是换行符，就开始判断换行符的下一个字符是不是 |
		if c == '\n' {
			state := ctx.PushState()
			ctx.Next()
			if ctx.Peek() == '|' {
				// 如果下一个字符是 |，就表示下一行是表格行
				ctx.ResumeState(state)
				return nil, false
			} else {
				// 否则就是表格的结尾
				return []MarkdownSegment{
					{Type: WhitespaceElemType, Rune: '\n'},
					// {Type: borderType, Rune: '|'},
				}, true
			}
		}
		return nil, false
	}
}

var (
	// [name](url)
	LinkTokenizer = Sequence(
		Runes(LinkElemType.SubType("["), "["),
		Until(LinkElemType.SubType("name"), ']'),
		Runes(LinkElemType.SubType("]"), "]"),
		Runes(LinkElemType.SubType("("), "("),
		Spaces(LinkElemType.SubType("space")),
		Until(LinkElemType.SubType("url"), ')'),
		Spaces(LinkElemType.SubType("space")),
		Runes(LinkElemType.SubType(")"), ")"),
	)

	// ![alt](url)
	ImageTokenizer = Sequence(
		String(ImageElemType.SubType("!["), "!["),
		Until(ImageElemType.SubType("alt"), ']'),
		String(ImageElemType.SubType("]"), "]"),
		String(ImageElemType.SubType("("), "("),
		Spaces(ImageElemType.SubType("space")),
		Until(ImageElemType.SubType("url"), ')'),
		Spaces(ImageElemType.SubType("space")),
		String(ImageElemType.SubType(")"), ")"),
	)

	// <doc_>
	IndexRefTokenizer = Sequence(
		Runes(IndexRefElemType.SubType("prefix"), "<"),
		Or(
			String(IndexRefElemType.SubType("doc"), "doc_"),
			String(IndexRefElemType.SubType("web"), "web_"),
			String(IndexRefElemType.SubType("url"), "url_"),
		),
		Until(IndexRefElemType.SubType("id"), '>'),
		Runes(IndexRefElemType.SubType("suffix"), ">"),
	)
)
