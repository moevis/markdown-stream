# markdown-stream

[English](README.md) | 简体中文

[![Go Reference](https://pkg.go.dev/badge/github.com/moevis/markdown-stream.svg)](https://pkg.go.dev/github.com/moevis/markdown-stream)
[![CI](https://github.com/moevis/markdown-stream/actions/workflows/ci.yml/badge.svg)](https://github.com/moevis/markdown-stream/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/moevis/markdown-stream)](go.mod)
[![Coverage](https://img.shields.io/badge/coverage-83.7%25-brightgreen)](https://github.com/moevis/markdown-stream/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/moevis/markdown-stream)](LICENSE)

原生 Go CommonMark 解析器，支持流式 token 输出与自定义语法扩展。提供具体语法树（CST）、可编辑语义 AST 和 HTML 编译；核心文法由声明式 `Rule` 组合子构成。

- **流式解析**：`NewTokenStream` 接收分块输入，通过 `Next` 返回已确认的 token 事件。
- **自定义语法**：`SyntaxExtension` + `Rule` 组合子，与内置文法共用同一入口。
- **LLM 输出**：可直接写入模型逐块返回的文本，最终解析结果与一次性输入相同。

## 兼容性

| 范围 | 覆盖 | 状态 |
|---|---|---|
| CommonMark 0.31.2 | 652/652 规范示例 | 完全支持 |
| GFM | 176/176 参考 fixture：表格、删除线、任务列表、字面自动链接、脚注 | 完全支持 |
| Math | 行内与块级 TeX 保留并添加数学类名；不运行 KaTeX | 扩展 |
| CST / AST / HTML | 具体 token、源码范围、UTF-16 位置、可编辑 AST、`RenderHTML` / `RenderAST` | 完全支持 |
| 流式 | 增量确认 token 事件，始终是完整解析的前缀 | 完全支持 |
| 原始 HTML / 协议 | 可解析；渲染与危险协议需要显式开启 | 完全支持 |
| 扩展 | 内置与自定义语法共用声明式 `Rule` | 完全支持 |

CommonMark 覆盖全部 652 个规范示例；GFM 覆盖 `internal/core/testdata` 中的 176 个参考 fixture。流式输出的是已确认的顶层 token，不是可撤回的字符级 token。

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

## 流式输入

将文本块交给 `Put` / `Write`，每次追加后调用 `Next` 读取已确认的 token 事件。`ReadPending` 表示当前没有更多已确认事件；输入结束时调用 `Close`，继续读取直到 `ReadDone`。

```go
stream := markdown.NewTokenStream()
drain := func() {
	for {
		event, status, err := stream.Next()
		if err != nil {
			panic(err)
		}
		if status != markdown.ReadEvent {
			return
		}
		if event.Kind == markdown.Enter {
			fmt.Printf("%s %q\n", event.Token.Type, event.Token.Source())
		}
	}
}
for _, chunk := range []string{"# 标", "题\n\n", "正文\n\n"} {
	if err := stream.Put(chunk); err != nil {
		panic(err)
	}
	drain()
}
if err := stream.Close(); err != nil {
	panic(err)
}
drain()
```

带 resolver 或自定义 Tokenizer 的配置在输入结束后整篇解析；需要完整 `Document` 时使用 `Parse`。

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

内置 GFM 与数学语法（`GFM()` + `GFMHTML()`，`Math()` + `MathHTML()`）；自定义语法使用同一套 `Rule` 组合子。内置元素以命名 `Construct` 组织，短规则直接呈现在元素声明中，复杂规则使用邻近的具名条件与语义动作。用法、规则组织和 `ExampleSyntaxExtension` 见 [EXTENSIONS.zh-CN.md](docs/EXTENSIONS.zh-CN.md)。

### 示例：CriticMarkup

用现有组合子识别 `{++新增++}`、`{--删除--}`、`{~~旧~>新~~}`、`{>>批注<<}`，不需要自定义 Tokenizer：

```go
separator := markdown.String("~>")
replacementEnd := markdown.String("~~}")
criticMarkup := markdown.Or(
	markdown.Span("criticAdd", "{++", "++}"),
	markdown.Span("criticDelete", "{--", "--}"),
	markdown.Capture("criticReplace", markdown.Sequence(
		markdown.String("{~~"), markdown.Until(markdown.Or(separator, replacementEnd)),
		separator, markdown.Until(replacementEnd), replacementEnd,
	)),
	markdown.Span("criticComment", "{>>", "<<}"),
)
syntax := markdown.SyntaxExtension{Text: map[markdown.Code][]markdown.Construct{
	'{': {{Name: "criticMarkup", Rule: criticMarkup}},
}}

document, err := markdown.Parse("{++新增++} {--删除--} {>>批注<<}", markdown.Options{
	Extensions: []markdown.SyntaxExtension{syntax},
})
if err != nil {
	panic(err)
}
for _, event := range document.Events {
	if event.Kind != markdown.Enter {
		continue
	}
	switch event.Token.Type {
	case "criticAdd", "criticDelete", "criticReplace", "criticComment":
		fmt.Println(event.Token.Type, event.Token.Source())
	}
}
// criticAdd {++新增++}
// criticDelete {--删除--}
// criticComment {>>批注<<}
```

## 安装

```bash
go get github.com/moevis/markdown-stream
```

## License

MIT
