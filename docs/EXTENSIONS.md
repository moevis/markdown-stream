# 扩展：GFM、数学与自定义语法

本文说明内置 GFM/数学语法和自定义语法扩展；快速开始见 [README](README.md)。

## GFM 与数学扩展

语法和 HTML 注册是独立步骤：

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

`GFM()` 包含表格、删除线、任务列表、字面自动链接和脚注；`GFMHTML()` 提供对应编译与 GFM 禁用标签过滤。默认删除线允许单、双波浪线，可用 `GFM(GFMOptions{SingleTilde: false})` 只允许双波浪线。脚注 HTML 可通过 `GFMHTMLOptions` 配置。

`Math()` 识别行内及块级 TeX，`MathHTML()` 将 TeX 保留为经过 HTML 转义的文本并添加数学类名，不运行 KaTeX，也不求值或排版 TeX。

## 语法组合子

`Rule` 是无状态匹配器 `func(*TokenizeContext, *Effects) bool`：成功消费输入并提交效果，失败保持游标和 token tape 不变，因此组合子可以安全回退。Rule 是值，声明一次即可在多个父规则中复用；`Guard` 提供零宽上下文约束，`Bind` 让后一段规则依赖前一段的捕获，`Capture` 的 `TokenAction` 在匹配成功后填充 token/节点。

核心、GFM 与 Math 的共享匹配器、命名 `Construct` 元素和阶段注册集中在 `grammar.go`。短 `Rule` 直接写在元素声明中；复杂规则在 `block_rules.go`、`inline_grammar.go`、`gfm_block.go`、`gfm_inline.go` 中实现，多步骤 `TokenAction` 和上下文条件提取为具名函数，紧邻对应规则。不引入另一套元素 DSL。外部扩展仍在 `SyntaxExtension` 对应阶段按起始 `Code` 注册 `Construct{Rule: ...}`。

`blockDriver.run` 按容器续接、叶子续接、空白处理、候选匹配和开块提交编排行处理；原生块、自定义块/容器和 GFM table 分别提交。已确认的段落中断候选直接传给开块阶段，不重复匹配；table 需要更多输入时保留原有流式回退。同步与分块解析仍共用上述文法和提交机制。

| 规则 | 行为 |
|---|---|
| `String(text)` / `FoldedString(text)` | 匹配字面文本 / ASCII 大小写不敏感文本 |
| `Runes(set)` / `Satisfy(predicate)` / `Any()` | 消费集合中一个 rune / 满足谓词的一个预处理 code / 任意一个预处理 code |
| `Sequence(rules...)` / `Or(rules...)` | 全部按序成功才提交 / 按顺序尝试分支 |
| `Repeat(rule, min, max)` | 重复 min–max 次，max 为 0 表示无上限；非法范围 panic，零宽成功导致解析错误 |
| `Many(rule)` / `Some(rule)` / `Times(rule, n)` | 0+ / 1+ / 恰好 n 次 |
| `SepBy(item, separator, min, max)` | 用 separator 分隔 item；分隔符后没有 item 时回退分隔符 |
| `Optional(rule)` | 可缺省；成功时提交，包括零宽断言，失败时恢复匹配效果 |
| `Lookahead(rule)` / `Not(rule)` | 测试匹配 / 不匹配，不提交游标或 token 效果 |
| `Until(terminator)` | 消费到终止规则之前，不消费终止符；可跨行，缺少终止符时整体回退 |
| `Capture(kind, rule, actions...)` | 记录具体 token（允许空范围），成功后执行 `TokenAction` |
| `OptionalCapture(kind, rule, actions...)` | 只在规则匹配时记录 token |
| `BindCapture(kind, rule, next, actions...)` | 捕获后把 token 交给下一段规则，共用事务 |
| `Guard(predicate)` / `End()` | 不消费输入的上下文约束 / 当前逻辑作用域结束断言 |
| `Bind(first, next)` | 根据前一匹配选择后续规则，共用事务 |

`Content(kind)` 返回 `TokenAction`，用作 `Capture` 的 action，标记捕获正文由原生文法继续展开。`TokenAction` 返回的错误会终止解析，不是普通匹配失败，不能由 `Or` 的后续分支吞掉。

## 自定义语法

复杂扩展可使用 `Rule.Tokenizer()` 或直接实现 `Tokenizer`：`State` 以 `ok`/`nok` 状态结束尝试，`Effects.Enter`/`Exit` 记录边界，`Consume` 移动游标，`Attempt` 提交成功分支，`Check`/`Interrupt` 只做前瞻。`Construct` 的 `Continuation`、`Exit` 和 `Resolve`/`ResolveTo`/`ResolveAll` 支持容器续行、退出及事件解析生命周期。核心仍用状态和效果管理容器续行、分隔符配对、引用及缩进状态。

`Resolve` 接收当前构造的事件，`ResolveTo` 接收包含已提交前缀的当前作用域事件，并以返回的完整事件 tape 替换该作用域，而非再次追加前缀；`ResolveAll` 在作用域结束时统一处理。Document 续行中的 `ResolveTo` 包含已提交的子 flow，不重复包含当前容器；失败尝试恢复解析器管理的游标、事件、token 拓扑、语义节点和浅 metadata 改写。

block 分类与中断预览不执行或登记 resolver。non-concrete Flow 在 checking 与实际提交时使用相同的 Document 容器边界。`ContentInitial` 可以只消费物理行的前缀：剩余正文继续解析，纯空白剩余保留为 flow trivia，不生成空段落，源码范围也不丢失。

`Data` map 只做浅拷贝，扩展回调的副作用不会自动回滚；解析或编译期间不要修改扩展配置及其 map。

下面复用外部包可执行的 `ExampleSyntaxExtension`，显式注册 `<doc_intro>` 语法与 HTML：

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
