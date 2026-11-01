# markdown-stream

增量流式 Markdown 解析器，逐字符输出带类型的 segment。

区别于一次性把整篇文本解析成 AST 的解析器（如 gomarkdown），本库面向**边产生边消费**的场景：上游把文本按块喂进来，下游立刻就能拿到带元素类型的字符流。典型场景是流式 LLM 输出——模型正在生成 markdown 回答时，就可以实时按结构切分（比如按段落分句交给 TTS、清洗引用标记存搜索历史），不用等全文生成完。

零外部依赖（仅标准库）。

## 快速开始

```go
package main

import (
	"fmt"

	"github.com/moevis/markdown-stream"
)

func main() {
	parser := markdown.NewParser()

	// 解析器在独立 goroutine 里运行，Start 传入根元素（MarkdownDoc 为预定义的完整文档文法）
	go parser.Start(markdown.MarkdownDoc)

	// 可以一次喂全部，也可以分多次增量喂入
	parser.Put("这是**粗体**和一个[链接](https://github.com)。\n")
	parser.Put("下一段……")
	parser.Close()

	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		fmt.Printf("%s: %q\n", seg.Type, seg.Rune)
	}
}
```

每个 `Read()` 返回一个 `MarkdownSegment{Type, Rune}`：`Rune` 是原文本中的一个字符，`Type` 是它所属的元素类型。把 segment 按 `Type` 分组拼回，就能还原出"哪些字符属于标题、哪些属于代码块、哪些是加粗文本"这类结构信息。

## 流式 API

```go
p := markdown.NewParser()
go p.Start(markdown.MarkdownDoc)   // 启动解析（每 Parser 只能 Start 一次）

p.Put("增量文本块")                 // 可多次调用；内部有 1024 容量的缓冲 channel
p.Put("另一个文本块")
p.Close()                          // 输入结束

for {
    seg, ok := p.Read()            // 阻塞读，返回 (MarkdownSegment, true)；输入关闭且耗尽后返回 (_, false)
    if !ok { break }
}
```

非阻塞读（用于不想阻塞等待输出的场景）：

```go
seg, ok := p.ReadNonBlocking()     // 无可用 segment 时返回 (nil, true)，不阻塞
```

`Start` 与 `Put` 的关系：`Start` 会在一个 goroutine 内驱动根元素的文法向下解析，解析器通过 `Peek` 在拿不到字符时自动阻塞等待 `Put` 喂入的新内容；`Close` 之后 `Peek` 返回 0，解析自然结束。所以**可以一边 Put 一边 Read**，延迟只取决于元素匹配需要的前瞻长度。

## 元素类型

`MarkdownSegment.Type` 的类型是 `ElemType`（字符串）：

| ElemType | 含义 |
|---|---|
| `doc` | 根文档 |
| `header` | 标题（`#`～`#######`，支持 h1–h7） |
| `paragraph` | 段落 |
| `list` | 无序列表（`- ` / `+ ` / `* `） |
| `quote` | 引用（`> `） |
| `table` | 表格（`| ... |`，至少表头 + 分隔行） |
| `code` | 代码块（```` ``` ````，含语言标注） |
| `equation` | 独立公式（`$$...$$`） |
| `inline-code` | 行内代码（`` ` ``） |
| `inline-equation` | 行内公式（`$...$`） |
| `strong` | 加粗（`**...**` / `__...__`） |
| `italic` | 斜体（`*...*` / `_..._`） |
| `link` | 链接（`[name](url)`） |
| `image` | 图片（`![alt](url)`） |
| `reference` | 引用标记（`<doc_xxx>` / `<web_xxx>` / `<url_xxx>`） |
| `whitespace` | 空白字符（空格/制表/换行） |

元素类型还有派生标记：`e.Text()`（正文内容）、`e.Prefix()`（起始符）、`e.Suffix()`（结束符）、`e.SubType(name)`（自定义子类型，如 `link.name`、`link.url`、`table.border`）。以标题为例，`#` 会输出为 `header.prefix`，标题文字输出为 `header.text`。

## 预定义元素

根元素：

```go
markdown.MarkdownDoc   // 完整文档：标题/表格/代码块/公式/列表/引用/段落 的任意组合
```

块级元素：`HeaderElem`、`TableElem`、`CodeElem`、`EquationElem`、`ListElem`、`QuoteElem`、`ParagraphElem`

行内元素：`StrongElem`、`StrongElem2`（`__`）、`ItalicElem`、`ItalicElem2`（`_`）、`LinkElem`、`ImageElem`、`IndexElem`、`InlineCodeElem`、`InlineEquationElem`

可以只解析某一种元素，例如 `go p.Start(markdown.TableElem)`。

## 自定义元素与 Tokenizer

解析器本身是递归下降的文法解释器：`MarkdownElem` 描述一个元素怎么开始、怎么匹配子元素、正文类型和结束符。`Tokenizer` 是匹配原语（`func(ctx) ([]MarkdownSegment, bool)`），组合子可自由拼装：

| Tokenizer | 行为 |
|---|---|
| `Any()` | 匹配任意非换行字符（不消费） |
| `String(t, s)` | 匹配字符串 `s` |
| `Repeat(t, r, min, max)` | 匹配重复的字符 `r`（min–max 次） |
| `Runes(t, s)` | 按序匹配字符集 |
| `Spaces(t)` | 匹配空格与制表符 |
| `Until(t, r)` | 匹配直到字符 `r`（处理 `\` 转义） |
| `UntilRunes(t, rs)` | 匹配直到字符集之一 |
| `Or(...)` | 任选一个分支匹配 |
| `Sequence(...)` | 按序匹配，任一失败则整体失败 |

自定义一个"以 `>>>` 开头、遇到 `<<<` 结束"的元素示例：

```go
block := markdown.MarkdownElem{
	Type:            markdown.MarkdownDocElemType.SubType("block"),
	Tokenizer:       markdown.String(markdown.MarkdownDocElemType.SubType("prefix"), ">>>"),
	TextElemType:    markdown.MarkdownDocElemType.SubType("text"),
	SuffixPredictor: markdown.String(markdown.MarkdownDocElemType.SubType("suffix"), "<<<"),
}
go p.Start(block)
```

## 设计要点

- **rune 级消费**：`Parser.Next/Peek` 按 rune 推进，`PushState/ResumeState` 支持回溯，文法匹配失败可整体回退。
- **行内/块级区分**：`Inline: true` 的元素遇到换行符立即结束；块级元素负责自己消费换行符并输出为 `whitespace`。
- **转义支持**：`Until/UntilRunes` 会把 `\` 及其后一个字符一起消费，不当作语法符。
- **大输入**：`Parser.Put` 缓冲 channel 容量 1024，`Parser` 内部按需累积内容；测试覆盖 10 万字符量级输入。

## 安装

```bash
go get github.com/moevis/markdown-stream
```

## 测试

```bash
go test ./...
```

`parser_test.go` 覆盖：完整文档解析、表格、代码块、公式、无序列表、加粗、引用标记、大负载、增量流式输入（含延迟喂入）。

## License

MIT
