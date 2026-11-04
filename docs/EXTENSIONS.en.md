# Extensions: GFM, Math, and Custom Syntax

[English](EXTENSIONS.en.md) | [简体中文](EXTENSIONS.zh-CN.md)

This document covers the built-in GFM/math extensions, the grammar combinators, and custom syntax registration. For the quick start, see [README](../README.md).

## GFM and math

Syntax and HTML are registered independently:

```go
options := markdown.Options{
	Extensions:     []markdown.SyntaxExtension{markdown.GFM(), markdown.Math()},
	HTMLExtensions: []markdown.HTMLExtension{markdown.GFMHTML(), markdown.MathHTML()},
}
html, err := markdown.HTML("- [x] ~~done~~\n\n$x < y$", options)
if err != nil {
	panic(err)
}
fmt.Println(html)
```

`GFM()` adds tables, strikethrough, task lists, literal autolinks, and footnotes; `GFMHTML()` provides their HTML compiler and GFM tag filtering. Strikethrough accepts one or two tildes by default; use `GFM(GFMOptions{SingleTilde: false})` to require two. Footnote HTML is configurable through `GFMHTMLOptions`.

`Math()` recognizes inline and block TeX; `MathHTML()` preserves TeX as HTML-escaped text with math classes. It does not run KaTeX, evaluate, or typeset TeX.

## Grammar combinators

`Rule` is a stateless matcher `func(*TokenizeContext, *Effects) bool`. On success it consumes input and commits effects; on failure it must leave the cursor and token tape unchanged, so combinators can roll back safely. Rules are values and can be declared once and reused in many parents. `Guard` adds a zero-width context constraint, `Bind` selects the next rule from the previous match, and `Capture` runs `TokenAction` callbacks after a match.

Shared matchers, named `Construct` elements, and phase registration live in `internal/core/grammar.go`. Short `Rule` declarations stay next to their element; complex rules live in `internal/core/block_rules.go`, `inline_grammar.go`, `gfm_block.go`, and `gfm_inline.go`, with multi-step `TokenAction` and context conditions extracted into named functions. External extensions register `Construct{Rule: ...}` under the starting `Code` in the matching `SyntaxExtension` phase; no second grammar is introduced.

| Rule | Behavior |
|---|---|
| `String(text)` / `FoldedString(text)` | Literal text / ASCII case-insensitive text |
| `Runes(set)` / `Satisfy(predicate)` / `Any()` | One rune from a set / one preprocessed code matching a predicate / any preprocessed code |
| `Sequence(rules...)` / `Or(rules...)` | Commit only when all succeed / try alternatives in order |
| `Repeat(rule, min, max)` | Repeat min–max times; max=0 is unbounded; invalid bounds panic; zero-width success is a parsing error |
| `Many(rule)` / `Some(rule)` / `Times(rule, n)` | 0+ / 1+ / exactly n times |
| `SepBy(item, separator, min, max)` | Separated repetition; a separator without a following item is rolled back |
| `Optional(rule)` | Optional; a successful match commits, including zero-width assertions; an absent branch restores effects |
| `Lookahead(rule)` / `Not(rule)` | Test matching / non-matching without committing cursor or token effects |
| `Until(terminator)` | Consume up to but not including the terminator; may cross line endings; roll back when missing |
| `Capture(kind, rule, actions...)` | Record a concrete token (empty spans allowed); run `TokenAction` on success |
| `OptionalCapture(kind, rule, actions...)` | Record the token only when the rule matches |
| `BindCapture(kind, rule, next, actions...)` | Capture, then pass the token to the dependent rule in one transaction |
| `Span(kind, open, close)` | Capture `open`…`close`; a missing close rolls back; delimiters do not nest |
| `Guard(predicate)` / `End()` | Zero-width context constraint / end-of-scope assertion |
| `Bind(first, next)` | Select the next rule from the previous match in one transaction |

`Content(kind)` returns a `TokenAction` for `Capture` that asks the native grammar to expand the captured body. A `TokenAction` error stops parsing; it is not an ordinary mismatch and cannot be swallowed by later `Or` branches.

## Custom syntax

Complex extensions may use `Rule.Tokenizer()` or implement `Tokenizer` directly: `State` ends an attempt with `ok`/`nok`; `Effects.Enter`/`Exit` record boundaries; `Consume` moves the cursor; `Attempt` commits a successful branch; `Check`/`Interrupt` only look ahead. `Construct` supports `Continuation`, `Exit`, and `Resolve`/`ResolveTo`/`ResolveAll` for container continuation, exit, and event resolution. The core still uses state and effects for container continuation, delimiter matching, references, and indentation.

`Resolve` receives the current construct's events; `ResolveTo` also receives the committed prefix and replaces the scope's event tape; `ResolveAll` runs once at the end of the scope. A document continuation's `ResolveTo` includes committed child flow without duplicating the current container. Failed attempts restore parser-managed cursor, events, token topology, semantic nodes, and shallow metadata.

`Data` maps are shallow-copied; callback side effects are not rolled back automatically. Do not mutate extension configuration or its maps during parsing or compilation.

The following is the externally runnable `ExampleSyntaxExtension`, registering `<doc_intro>` syntax and HTML:

```go
identifier := markdown.Capture("docIdentifier", markdown.Repeat(
	markdown.Runes("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"), 1, 0,
))
reference := markdown.Capture("docReference", markdown.Sequence(
	markdown.String("<doc_"), identifier, markdown.String(">"),
))
syntax := markdown.SyntaxExtension{Text: map[markdown.Code][]markdown.Construct{
	'<': {{Name: "docReference", Rule: reference}},
}}
html := markdown.HTMLExtension{
	Enter: map[markdown.TokenType]markdown.HTMLHandler{
		"docReference": func(context *markdown.CompileContext, token *markdown.Token) error {
			id := token.Children[0].Source()
			if err := context.Tag(`<a href="/docs/` + context.Encode(id) + `">`); err != nil {
				return err
			}
			return context.Raw(context.Encode(id))
		},
	},
	Exit: map[markdown.TokenType]markdown.HTMLHandler{
		"docReference": func(context *markdown.CompileContext, _ *markdown.Token) error {
			return context.Tag("</a>")
		},
	},
}
ordinary, err := markdown.HTML("<doc_intro>")
if err != nil {
	panic(err)
}
extended, err := markdown.HTML("<doc_intro>", markdown.Options{
	Extensions: []markdown.SyntaxExtension{syntax}, HTMLExtensions: []markdown.HTMLExtension{html},
})
if err != nil {
	panic(err)
}
fmt.Println(ordinary) // <p>&lt;doc_intro&gt;</p>
fmt.Println(extended) // <p><a href="/docs/intro">intro</a></p>
```
