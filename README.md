# markdown-stream

[English](README.md) | [简体中文](README.zh-CN.md)

A streaming-first Go Markdown parser with CommonMark + GFM, a concrete syntax tree (CST), an editable AST, HTML rendering, and declarative parser combinators. Designed for real-time LLM output and custom syntax extensions, with no third-party dependencies.

[![Go Reference](https://pkg.go.dev/badge/github.com/moevis/markdown-stream.svg)](https://pkg.go.dev/github.com/moevis/markdown-stream)
[![CI](https://github.com/moevis/markdown-stream/actions/workflows/ci.yml/badge.svg)](https://github.com/moevis/markdown-stream/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/moevis/markdown-stream)](go.mod)
[![Coverage](https://img.shields.io/badge/coverage-83.7%25-brightgreen)](https://github.com/moevis/markdown-stream/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/moevis/markdown-stream)](LICENSE)

## Why markdown-stream

- **Streaming tokens** — feed chunks with `Put`/`Write` and read confirmed CST token events with `Next`. Events are always a prefix of the full parse and are never retracted.
- **CommonMark + GFM** — the core grammar plus tables, strikethrough, task lists, literal autolinks, footnotes, and math through explicit extensions.
- **CST and editable AST** — concrete tokens retain logical source, raw source, source ranges, and UTF-16 positions. `RenderHTML` keeps the parsed concrete semantics; `RenderAST` renders the edited semantic tree.
- **Declarative grammar** — every built-in and custom construct is a `Rule` tree (`Sequence`, `Or`, `Repeat`, `SepBy`, `Span`, `Guard`, `Bind`, ...). Extensions use the same engine as the core.
- **Zero dependencies** — standard library only.

## Compatibility

| Area | Coverage | Status |
|---|---|---|
| CommonMark 0.31.2 | 652/652 spec examples | Full |
| GFM | 176/176 reference fixtures: tables, strikethrough, task lists, literal autolinks, footnotes | Full |
| Math | inline and block TeX preserved with math classes; no KaTeX evaluation | Extension |
| CST / AST / HTML | concrete tokens, source ranges, UTF-16 positions, editable AST, `RenderHTML` / `RenderAST` | Full |
| Streaming | incremental confirmed token events; always a prefix of the full parse | Full |
| Raw HTML / protocols | parsed; rendering and dangerous protocols require explicit opt-in | Full |
| Extensions | same declarative `Rule` grammar for built-ins and custom syntax | Full |

CommonMark is covered by all 652 spec examples; GFM by the 176 reference fixtures in `internal/core/testdata`. Streaming emits confirmed top-level tokens, not provisional character-level tokens.

## Quick start

```go
package main

import (
	"fmt"

	markdown "github.com/moevis/markdown-stream"
)

func main() {
	html, err := markdown.HTML("This is **bold** and a [link](https://github.com).")
	if err != nil {
		panic(err)
	}
	fmt.Println(html)
}
```

## Streaming tokens

Append text chunks with `Put` (or `Write`), then call `Next` to read confirmed token events. `ReadPending` means no further event is confirmed yet; call `Close` at the end of input and continue until `ReadDone`.

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
for _, chunk := range []string{"# Ti", "tle\n\n", "body\n\n"} {
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

Configurations with resolvers or custom tokenizers fall back to a full parse after `Close`. Use `Parse` when you need a confirmed `Document`.

## Editable AST

`Document.Root` is an editable semantic tree. The example edits text nodes, then renders both the original concrete semantics and the edited tree:

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

## Extensions

Enable GFM and math explicitly:

```go
options := markdown.Options{
	Extensions:     []markdown.SyntaxExtension{markdown.GFM(), markdown.Math()},
	HTMLExtensions: []markdown.HTMLExtension{markdown.GFMHTML(), markdown.MathHTML()},
}
html, err := markdown.HTML("- [x] ~~done~~\n\n$x < y$", options)
```

Custom syntax uses the same `Rule` combinators and does not require a separate tokenizer:

```go
criticMarkup := markdown.Or(
	markdown.Span("criticAdd", "{++", "++}"),
	markdown.Span("criticDelete", "{--", "--}"),
	markdown.Span("criticComment", "{>>", "<<}"),
)
syntax := markdown.SyntaxExtension{Text: map[markdown.Code][]markdown.Construct{
	'{': {{Name: "criticMarkup", Rule: criticMarkup}},
}}
```

See [Extensions](docs/EXTENSIONS.en.md) for the combinator set, GFM/math options, resolver lifecycle, and a complete `ExampleSyntaxExtension`.

## Installation

```bash
go get github.com/moevis/markdown-stream
```

## License

MIT
