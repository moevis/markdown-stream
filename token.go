package markdown

import "strings"

// Tokenizer matches input transactionally: failure restores the cursor.
// Use the exported factories to compose matchers for MarkdownElem.
type Tokenizer func(ctx markdownCtx) ([]MarkdownSegment, bool)

// Any recognizes a non-EOF, non-line-break rune without consuming it.
func Any() Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		c := ctx.Peek()
		return nil, c != EOF && !isLineBreak(c)
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

// Repeat matches between minTimes and maxTimes copies of target.
func Repeat(elemType ElemType, target rune, minTimes, maxTimes int) Tokenizer {
	if minTimes < 0 || maxTimes < minTimes {
		return func(markdownCtx) ([]MarkdownSegment, bool) { return nil, false }
	}
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		var segs []MarkdownSegment
		state := ctx.PushState()
		for i := range maxTimes {
			c := ctx.Peek()
			if c == EOF || c != target {
				if i < minTimes {
					ctx.ResumeState(state)
					return nil, false
				}
				return segs, true
			}
			segs = append(segs, MarkdownSegment{Type: elemType, Rune: ctx.Next()})
		}
		return segs, true
	}
}

// Runes matches an ordered rune sequence.
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

// UntilRunes stops before any target rune; escapes cannot cross a line break.
func UntilRunes(elemType ElemType, runes string) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		var segs []MarkdownSegment
		for {
			c := ctx.Peek()
			if c == EOF {
				ctx.ResumeState(state)
				return nil, false
			}
			if strings.ContainsRune(runes, c) {
				return segs, true
			}
			if isLineBreak(c) {
				ctx.ResumeState(state)
				return nil, false
			}
			if c == '\\' {
				ctx.Next()
				next := ctx.Peek()
				if next == EOF || isLineBreak(next) {
					ctx.ResumeState(state)
					return nil, false
				}
				segs = append(segs,
					MarkdownSegment{Type: elemType, Rune: '\\'},
					MarkdownSegment{Type: elemType, Rune: ctx.Next()},
				)
				continue
			}
			segs = append(segs, MarkdownSegment{Type: elemType, Rune: ctx.Next()})
		}
	}
}

// Until matches up to r without consuming it; escapes cannot cross a line break.
func Until(elemType ElemType, r rune) Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		var segs []MarkdownSegment
		for {
			c := ctx.Peek()
			if c == EOF {
				ctx.ResumeState(state)
				return nil, false
			}
			if c == r {
				return segs, true
			}
			if isLineBreak(c) {
				ctx.ResumeState(state)
				return nil, false
			}
			if c == '\\' {
				ctx.Next()
				next := ctx.Peek()
				if next == EOF || isLineBreak(next) {
					ctx.ResumeState(state)
					return nil, false
				}
				segs = append(segs,
					MarkdownSegment{Type: elemType, Rune: '\\'},
					MarkdownSegment{Type: elemType, Rune: ctx.Next()},
				)
				continue
			}
			segs = append(segs, MarkdownSegment{Type: elemType, Rune: ctx.Next()})
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

// MatchTableHead matches a pipe-delimited header and its physical line ending.
func MatchTableHead() Tokenizer {
	matcher := tableHeadMatcher()
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		segs, _, matched := matcher(ctx)
		return segs, matched
	}
}

func tableHeadMatcher() func(markdownCtx) ([]MarkdownSegment, int, bool) {
	borderType := TableElemType.SubType("border")
	textType := TableElemType.Text()
	return func(ctx markdownCtx) ([]MarkdownSegment, int, bool) {
		state := ctx.PushState()
		if ctx.Peek() != '|' {
			return nil, 0, false
		}
		segs := []MarkdownSegment{{Type: borderType, Rune: ctx.Next()}}
		columns := 0
		lastWasPipe := true
		for {
			c := ctx.Peek()
			if c == EOF {
				ctx.ResumeState(state)
				return nil, 0, false
			}
			if isLineBreak(c) {
				if columns == 0 || !lastWasPipe {
					ctx.ResumeState(state)
					return nil, 0, false
				}
				segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: ctx.Next()})
				if c == '\r' && ctx.Peek() == '\n' {
					segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: ctx.Next()})
				}
				return segs, columns, true
			}
			switch c {
			case '|':
				columns++
				lastWasPipe = true
				segs = append(segs, MarkdownSegment{Type: borderType, Rune: ctx.Next()})
			case ' ', '\t':
				segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: ctx.Next()})
			case '\\':
				ctx.Next()
				next := ctx.Peek()
				if next == EOF || isLineBreak(next) {
					ctx.ResumeState(state)
					return nil, 0, false
				}
				lastWasPipe = false
				segs = append(segs,
					MarkdownSegment{Type: textType, Rune: '\\'},
					MarkdownSegment{Type: textType, Rune: ctx.Next()},
				)
			default:
				lastWasPipe = false
				segs = append(segs, MarkdownSegment{Type: textType, Rune: ctx.Next()})
			}
		}
	}
}

// MatchTableAlignRow matches strict alignment cells and leaves the line ending unread.
func MatchTableAlignRow() Tokenizer {
	matcher := tableAlignMatcher()
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		segs, _, matched := matcher(ctx, nil)
		return segs, matched
	}
}

func tableAlignMatcher() func(markdownCtx, []MarkdownSegment) ([]MarkdownSegment, int, bool) {
	borderType := TableElemType.SubType("border")
	return func(ctx markdownCtx, segs []MarkdownSegment) ([]MarkdownSegment, int, bool) {
		state := ctx.PushState()
		if ctx.Peek() != '|' {
			return nil, 0, false
		}
		segs = append(segs, MarkdownSegment{Type: borderType, Rune: ctx.Next()})
		columns := 0
		for {
			c := ctx.Peek()
			for c == ' ' || c == '\t' {
				segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: ctx.Next()})
				c = ctx.Peek()
			}
			if columns > 0 && (c == EOF || isLineBreak(c)) {
				return segs, columns, true
			}
			if c == ':' {
				segs = append(segs, MarkdownSegment{Type: borderType, Rune: ctx.Next()})
				c = ctx.Peek()
			}
			dashes := 0
			for c == '-' {
				segs = append(segs, MarkdownSegment{Type: borderType, Rune: ctx.Next()})
				dashes++
				c = ctx.Peek()
			}
			if dashes < 3 {
				ctx.ResumeState(state)
				return nil, 0, false
			}
			if c == ':' {
				segs = append(segs, MarkdownSegment{Type: borderType, Rune: ctx.Next()})
				c = ctx.Peek()
			}
			for c == ' ' || c == '\t' {
				segs = append(segs, MarkdownSegment{Type: WhitespaceElemType, Rune: ctx.Next()})
				c = ctx.Peek()
			}
			if c != '|' {
				ctx.ResumeState(state)
				return nil, 0, false
			}
			segs = append(segs, MarkdownSegment{Type: borderType, Rune: ctx.Next()})
			columns++
		}
	}
}

func matchTableStart() Tokenizer {
	head := tableHeadMatcher()
	alignment := tableAlignMatcher()
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		state := ctx.PushState()
		segs, columns, matched := head(ctx)
		if !matched {
			return nil, false
		}
		segs, alignmentColumns, matched := alignment(ctx, segs)
		if !matched || columns != alignmentColumns {
			ctx.ResumeState(state)
			return nil, false
		}
		return segs, true
	}
}

// NotTableRow ends a table at a line ending followed by a non-pipe line or EOF.
func NotTableRow() Tokenizer {
	return func(ctx markdownCtx) ([]MarkdownSegment, bool) {
		c := ctx.Peek()
		if c == EOF {
			return nil, true
		}
		if !isLineBreak(c) {
			return nil, false
		}
		state := ctx.PushState()
		ctx.Next()
		crlf := c == '\r' && ctx.Peek() == '\n'
		if crlf {
			ctx.Next()
		}
		if ctx.Peek() == '|' {
			ctx.ResumeState(state)
			return nil, false
		}
		if crlf {
			return []MarkdownSegment{
				{Type: WhitespaceElemType, Rune: '\r'},
				{Type: WhitespaceElemType, Rune: '\n'},
			}, true
		}
		return []MarkdownSegment{{Type: WhitespaceElemType, Rune: c}}, true
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
