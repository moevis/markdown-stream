# markdown-stream

增量流式 Markdown 解析器，逐 rune 输出带类型的 `MarkdownSegment`，不构建 AST 或生成 HTML。

面向边产生边消费的场景，例如流式 LLM 输出。上游按块提供文本，下游按元素类型处理字符；需要前瞻的语法会等待后续输入，而不是保证每个输入块立即产生输出。

要求 Go 1.22 或更新版本，零第三方依赖。CommonMark/Commonmarker 一致性仍在验证中，暂不声明完全兼容。

## 快速开始

```go
package main

import (
	"fmt"

	"github.com/moevis/markdown-stream"
)

func main() {
	parser := markdown.NewParser()

	go parser.Start(markdown.MarkdownDoc)

	// 生产与消费并行，避免大输入填满缓冲区后相互等待。
	go func() {
		parser.Put("这是**粗体**和一个[链接](https://github.com)。\n")
		parser.Put("下一段……")
		parser.Close()
	}()

	for {
		seg, ok := parser.Read()
		if !ok {
			break
		}
		fmt.Printf("%s: %q\n", seg.Type, seg.Rune)
	}
}
```

`Read()` 返回 `(MarkdownSegment, bool)`；`ok == false` 表示输出已经耗尽。`Rune` 是一个已解码字符，`Type` 是它所属的元素类型。对默认 `MarkdownDoc`，依次拼接所有输出 rune 等于对全部输入块拼接后进行 Go rune 解码的结果；Markdown 标记和反斜杠也会保留。

## 流式 API

每个 Parser 只使用一次：一个 goroutine 调用 `Start(root)`，一个生产者按顺序 `Put` 并最终调用一次 `Close`，一个消费者持续读取输出。

| API | 约定 |
|---|---|
| `NewParser()` | 创建独立解析器 |
| `Start(root)` | 驱动根元素并在根元素结束时关闭输出；通常在独立 goroutine 运行 |
| `Put(delta)` | 按顺序追加 string 块；缓冲区满时阻塞，不得在 `Close` 后调用 |
| `Close()` | 标记输入结束；不取消解析、不等待输出消费完成 |
| `Read()` | 阻塞等待 segment；输出耗尽后返回 `(_, false)` |
| `ReadNonBlocking()` | 有输出时返回 `(*MarkdownSegment, true)`；暂时无输出时返回 `(nil, true)`；结束时返回 `(_, false)` |

输入缓冲容纳 1024 个 string 块，输出缓冲容纳 64 个 segment。**不要在同一 goroutine 先全部 Put，再开始 Read**：大输入会因背压阻塞。即使调用了 `Close`，输出仍需要持续消费。

`Next`、`Peek`、`PushState`、`ResumeState` 和 `OutputRune` 用于串行文法处理，不能从另一个 goroutine 与 `Start` 并发操作。预定义元素和自定义元素的切片也不应在解析期间被修改。

### UTF-8 与 EOF

块边界可以落在任意字节位置，包括一个汉字或 emoji 的中间。空块不改变输入，未完整的 UTF-8 前缀会等待后续块；非法 UTF-8 和输入结束时的残缺字节按 Go 的规则产生 `utf8.RuneError`。

游标 API 的结束标记是 `markdown.EOF`（`rune(-1)`），**不是 0**。真实 U+0000 会被正常读取和输出。

### 内存

解析器为回溯保留已经解码的输入，运行时内存随累计输入增长，并非常量空间。根元素处理完成后会释放内部 rune 缓存；大块输入和需要长距离前瞻的语法仍可能占用较多内存。

## 元素类型

`MarkdownSegment.Type` 的类型是 `ElemType`（字符串）：

| ElemType | 含义 |
|---|---|
| `doc` | 根文档 |
| `header` | 标题（`#`～`#######`，支持 h1–h7） |
| `paragraph` | 段落 |
| `list` | 无序列表（`- ` / `+ ` / `* `） |
| `quote` | 引用（`> `） |
| `table` | 管道表格（至少表头 + 对齐行） |
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
markdown.MarkdownDoc   // 默认组合：标题/表格/代码块/公式/列表/引用/段落
```

块级元素：`HeaderElem`、`TableElem`、`CodeElem`、`EquationElem`、`ListElem`、`QuoteElem`、`ParagraphElem`

行内元素：`StrongElem`、`StrongElem2`（`__`）、`ItalicElem`、`ItalicElem2`（`_`）、`LinkElem`、`ImageElem`、`IndexElem`、`InlineCodeElem`、`InlineEquationElem`

可以只解析某一种元素，例如 `go p.Start(markdown.TableElem)`。这种根元素可能在输入结束之前停止，其余输入不会自动继续解析；应用需要相应安排生产者生命周期。

## 自定义元素与 Tokenizer

解析器使用递归下降文法：`MarkdownElem` 描述元素的起始匹配、子元素、正文类型和结束匹配。通过内置 Tokenizer 工厂组合自定义语法；匹配失败时输入游标会回退。

| Tokenizer | 行为 |
|---|---|
| `Any()` | 匹配一个非 EOF、非 CR/LF 的 rune，不消费；允许 NUL |
| `String(t, s)` | 匹配字符串 `s` |
| `Repeat(t, r, min, max)` | 匹配 min–max 次；零次匹配可成功，非法上下界不匹配 |
| `Runes(t, s)` | 按顺序匹配字符串中的 rune 序列 |
| `Spaces(t)` | 匹配空格与制表符 |
| `Until(t, r)` | 匹配到 `r` 之前，不消费终止符；保留转义字符，不能用转义跨行 |
| `UntilRunes(t, rs)` | 同上，以集合 `rs` 中的任一 rune 为终止符 |
| `Or(...)` | 任选一个分支匹配 |
| `Sequence(...)` | 按序匹配，任一失败则整体失败 |

`Tokenizer` 的上下文类型未导出，外部包通过这些工厂组合匹配器，而不是自行声明上下文回调。

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

`TrimWhites` 将正文中指定的 rune 标记为 `whitespace`，不会让 `Inline: true` 的元素跨过 CR/LF。结束匹配优先于空白处理。子元素按顺序尝试，只有实际消费输入的匹配才会阻止后续分支；零宽匹配不会使父元素停滞。

## 安装

```bash
go get github.com/moevis/markdown-stream
```

## 测试

```bash
go test ./...
go test -race ./...
go vet ./...
go test ./... -cover
go test -run='^$' -fuzz=FuzzParserPreservesInput -fuzztime=10s -parallel=4
```

测试按职责拆为 `parser_test.go`、`element_test.go` 和 `token_test.go`：验证完整的类型/rune 序列、失败回溯、UTF-8 字节分块、空块、NUL、CR/LF/CRLF、实时输出和大并发输入。`example_test.go` 提供外部包的可执行用法示例；fuzz 检查默认文法的字符保留。

GitHub Actions 使用 Go 1.22 与最新稳定版运行测试和 vet，并在稳定版执行 race 检查。源码与测试保持模块根目录单包结构。

## License

MIT
