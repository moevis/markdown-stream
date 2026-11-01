package markdown

import "fmt"

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

type MarkdownElem struct {
	Type   ElemType // 只是用来标记的，不会依赖这个字段来判断类型
	Inline bool     // 是否是行内元素，行内元素遇到换行符会被截断

	Tokenizer Tokenizer

	TrimWhites []rune

	// 可能的子元素，子元素的匹配是无序的
	SubElems []MarkdownElem

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

func (m *MarkdownElem) processSubElems(ctx markdownCtx) (processed bool) {
	for {
		c := ctx.Peek()
		if c == 0 {
			break
		}
		// 行内元素遇到换行符会马上结束
		if m.Inline && c == '\n' {
			return true
		}
		// 消耗空白字符，如果看到末尾，就结束
		if cont, whiteNum := m.consumeWhites(ctx); !cont {
			break
		} else if whiteNum > 0 {
			continue
		}
		subElementMatched := -1
		// 优先匹配子元素
		for i, sub := range m.SubElems {
			if sub.Process(ctx) {
				subElementMatched = i
				break
			}
		}

		if subElementMatched == -1 {
			// 要么当前元素没有子元素，要么现在读到的数据没有匹配到子元素
			// 那么当前字符要由当前元素来处理
			ctx.OutputRune(c, m.TextElemType)
			ctx.Next()
		} else {
			// 匹配到了子元素
			if !m.Inline {
				// 当前元素是块元素，需要负责自己处理换行符
				// subElement 可能已经消耗了前面的变量 c，所以这里要重新 peek
				c = ctx.Peek()
				if c == '\n' {
					// 子元素最后看到的是 \n，并且因为换行符的原因结束了
					if m.SubElems[subElementMatched].Inline {
						// 如果之前的子元素是行内元素，需要在这里输出换行符
						ctx.OutputRune(c, WhitespaceElemType)
						ctx.Next()
					}
				}
			}
		}

		// 如果有结束符后缀，就根据结束符后缀来判断是否结束
		if m.foundSuffix(ctx) {
			return true
		}
	}
	return true
}

func (m *MarkdownElem) Process(ctx markdownCtx) (processed bool) {
	if ctx.Peek() == 0 {
		return false
	}
	if m.Tokenizer != nil {
		if segs, ok := m.Tokenizer(ctx); ok {
			for _, seg := range segs {
				ctx.OutputRune(seg.Rune, seg.Type)
			}
			// if len(m.SubElems) == 0 {
			// 	// 如果没有子元素，表示已经匹配结束了
			// 	return true
			// }
		} else {
			// 如果 sequence pattern 没有匹配成功，就直接返回
			return false
		}
	} else {
		// 如果没有子元素，且没有 sequence pattern 的检查，就直接返回
		if len(m.SubElems) == 0 {
			return false
		}
	}
	if cont, _ := m.consumeWhites(ctx); !cont {
		return false
	}
	if len(m.SubElems) == 0 {
		if !m.TextElemType.IsEmpty() {
			// 没有子元素，但是对于文本元素，需要输出当前字符
			for {
				c := ctx.Peek()
				if c == 0 {
					break
				}
				if cont, _ := m.consumeWhites(ctx); !cont {
					return true
				}
				if m.foundSuffix(ctx) {
					return true
				}
				ctx.OutputRune(c, m.TextElemType)
				ctx.Next()
			}
		} else {
			// 没有子元素，并且不需要 parse 普通字符（TextElemType.IsEmpty() == true）
			return true
		}
	}
	// 按子元素来匹配
	return m.processSubElems(ctx)
}

// consumeWhites 消耗掉前面的空白字符，返回 false 表示已经没有内容了
func (m *MarkdownElem) consumeWhites(ctx markdownCtx) (running bool, consumeNum int) {
	if len(m.TrimWhites) > 0 {
		for {
			c := ctx.Peek()
			if c == 0 {
				return false, consumeNum
			}
			hasWhitespace := false
			for _, t := range m.TrimWhites {
				if c == t {
					consumeNum++
					hasWhitespace = true
					ctx.OutputRune(c, WhitespaceElemType)
					ctx.Next()
					break
				}
			}
			if !hasWhitespace {
				break
			}
		}
	}
	return true, consumeNum
}
