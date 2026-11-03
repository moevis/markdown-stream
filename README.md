# markdown-stream

原生 Go CommonMark 解析器，支持流式/分块输入与自定义语法扩展，适合实时接收并正确解析 LLM 的流式输出。提供具体语法树（CST）、可编辑语义 AST 和 HTML 编译；核心文法由声明式 `Rule` 组合子构成。

- **流式输入**：`NewParser` 按块接收；关闭输入后确认并发布文档与事件。
- **自定义语法**：`SyntaxExtension` + `Rule` 组合子，与内置文法共用同一入口。
- **LLM 输出**：可直接写入模型逐块返回的文本，最终解析结果与一次性输入相同。

## 快速开始

```go
package main

import (
	"fmt"

	markdown "github.com/moevis/markdown-stream"
)

func main() {
	html, err := markdown.HTML("这是**粗体**和一个[链接](https://github.com)。")
	if err != nil {
		panic(err)
	}
	fmt.Println(html)
}
```

## 分块输入 API

```go
parser := markdown.NewParser()
if err := parser.Put("**Hi"); err != nil {
	panic(err)
}
if _, err := parser.Write([]byte("**")); err != nil {
	panic(err)
}
if err := parser.Close(); err != nil {
	panic(err)
}
document, err := parser.Document()
if err != nil {
	panic(err)
}
var html strings.Builder
if err := markdown.RenderHTML(&html, document); err != nil {
	panic(err)
}
fmt.Println(html.String())
// <p><strong>Hi</strong></p>
```

完整可执行示例见 `example_test.go`。

## 流式 token

`NewTokenStream` 在输入仍在到达时返回已经不可变的顶层块 token；事件流始终是完整 `Parse` 事件的前缀。解码、分行和块解析都按新增输入增量处理；含 `[`/`[^` 的段落、未闭合的容器或代码块会延迟到 `Close`，带 resolver/自定义 Tokenizer 的 Options 回退到完整解析。

```go
stream := markdown.NewTokenStream()
if err := stream.Put("# title\n"); err != nil {
	panic(err)
}
event, status, err := stream.Next() // ReadEvent：atxHeading Enter
```

`Parse` 与 `Parser.Read` 仍只在输入关闭并确认后发布完整文档。

## 可编辑 AST

`Document.Root` 是可编辑语义 AST。下面的例子修改文本节点，再分别用 `RenderHTML`（保留解析时的具体语义）和 `RenderAST`（反映修改后的树）渲染：

```go
document, err := markdown.Parse("hello")
if err != nil {
	panic(err)
}
document.Root.Walk(func(node *markdown.Node) bool {
	if node.Kind == markdown.KindText {
		node.Literal = "edited"
	}
	return true
})
var original, edited strings.Builder
if err := markdown.RenderHTML(&original, document); err != nil {
	panic(err)
}
if err := markdown.RenderAST(&edited, document.Root); err != nil {
	panic(err)
}
fmt.Println(original.String()) // <p>hello</p>
fmt.Println(edited.String())   // <p>edited</p>
```

## 扩展

内置 GFM 与数学语法（`GFM()` + `GFMHTML()`，`Math()` + `MathHTML()`）；自定义语法使用同一套 `Rule` 组合子。用法、组合子说明和 `ExampleSyntaxExtension` 见 [EXTENSIONS.md](EXTENSIONS.md)。

## 安装

```bash
go get github.com/moevis/markdown-stream
```

## License

MIT
