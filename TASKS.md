# 并行任务

## 共同约束

- 目标：原生 Go CommonMark 核心和完整 GFM 使用真实声明式 `Rule` 文法；不能用 Rule 包装过程式 scanner 冒充迁移。
- CST、token、位置和来源在解析时记录，AST 从实际 CST 生成；不得从 AST 猜测 CST。
- 分块输入关闭后使用与同步输入相同的文法，发布确认的 Document 和事件。
- 默认不识别 `<doc_xxx>`、`<web_xxx>`、`<url_xxx>` 业务引用；`<doc_xxx>` 仅保留显式注册的扩展示例。
- 公开 API 暂时冻结，不增加兼容 shim、第三方 Go 依赖或生产 Node 依赖。
- 不同时修改生产核心。构建、测试、格式化和最终 smoke 由主助手串行统一执行。

## A：解析器收尾

负责人：主助手。状态：已完成。

文件：`block.go`、`extensions.go`、必要的事务实现，以及 `block_container_test.go`。

- 容器续行的前瞻不得提交 resolver。
- 续行 resolver 必须看到正确的已提交 CST，不能丢失已经解析的表格等子 flow。
- 容器 Exit 必须在子 flow 确认后执行。
- 检查事件同步是否引入不必要的全前缀重复扫描；保持事务回滚和事件拓扑正确。
- 用真实公开 API smoke 和消费者可见的回归用例证明以上行为。

当前证据：续行表格 smoke 保留完整 HTML，两个实际续行位置各提交一次 resolver，Exit 在表格确认后执行。ResolveTo 不再重复加入当前容器；失败续行恢复 CST、AST 和 metadata，外部 callback 副作用保留一次。容器回归和完整 native suite 已通过。

## B：文档迁移

负责人：内部 `APIDocumentation`；主助手整合。状态：已完成整合验收。不要重复领取。

文件：`README.md`、`doc.go`；既有 `example_test.go` 本次保持不变。

- 迁移 Parse、NewParser、Document、Read、RenderHTML、RenderAST 等新 API 的说明和示例。
- 说明真实 Rule 文法及 State/Effects、resolver、compiler 生命周期。
- 区分原解析的 CST HTML 与公开 AST 编辑后的 RenderAST。
- 展示显式 GFM/Math/doc 扩展，不宣称完整 KaTeX。
- 删除旧 DSL、Start/MarkdownDoc 和旧 fuzz 命令。
- 不恢复用户已删除的七项 README 内容。

## C：外部 agent 可直接领取的只读审查

负责人：用户提供的另一个 agent。状态：已完成并返回（只读审查，未修改文件，未运行构建/测试）。主助手按 C-1 → C-2 → C-3 顺序处理。

审查文件：`block_rules.go`、`inline_grammar.go`、`inline_resource.go`、`gfm_block.go`、`gfm_inline.go`、`combinators.go`、`transactions.go`、`syntax.go`。

任务：

1. 核实 CommonMark/GFM 的匹配由真实 Sequence/Or/Repeat/Lookahead 等 Rule 组合完成，没有把旧 scanner 包装成 Rule，也没有遗留默认业务引用分支。
2. 审查 speculative Rule、Effects、resolver 的事务边界，特别是失败、零宽匹配、嵌套 resolver、source ranges 和浅复制 metadata；找消费者可见的风险，不要求嵌套对象或外部副作用自动回滚。
3. 检查现有用例是否遗漏有实际风险的语法优先级、边界或错误传播。不要添加接线、mock echo、源码文本或偶然 token 切分测试。

写入边界：不要修改生产文件、文档或测试，不运行构建、测试或格式化。需要动态复现时提供输入和可运行命令，由主助手统一执行。

返回格式：每个问题给出文件/符号、具体风险、复现输入或证据、最小修复建议；无问题则说明实际审查范围。不要重复实现 A 或 B。

### C 审查结果（已返回，原始静态报告）

以下保留原始报告和当时的复现建议；行号是审查时的坐标。C-2 的 hex-only 建议与实际 micromark 行为不符，未采用；当前处置和修复后证据见下文。

第 1 项核实通过：15 个 core construct 全部为 Rule 组合，core 中不存在 `Construct.Tokenize:`；过程式代码只出现在提交/布局阶段（`parseBlockTableMatch`、`tableSourceCells`、`blockUnmatchedLine`、缩进辅助）；无默认 doc/web/url 业务引用（生产代码零命中，`element_test.go` 断言需显式注册）。抽查 ATX 闭合、setext/thematic 优先级、表格 delimiter/中断、task-list lazy/制表符、www 下划线规则均与 fixture 一致。

C-1（中）被丢弃的探测匹配会永久提交 resolver 改写：
- 位置：`transactions.go:32-35`（`running == 0` 跳过 undo）、`extensions.go:321-350,444-486,696`（Rule 执行时 running>0，顶层派发 running==0）、`block.go:675-712` 与 `:839-843`（提交匹配后丢弃）、`pipeline.go:12-27,177-201`（被丢弃探测登记的 `ResolveAll` 之后仍会重建 `parent.Children`）。
- 风险：`ResolveTo` 把已提交父 token 子树交给 resolver；丢弃后无快照、无 undo，已提交 token 的 `Node.Literal`/`Data`/拓扑改写保留。仅放宽 `saveResolverUndo` 守卫无效（`tryConstruct` defer 在 running==0 时清空 undo）。
- 修复：`parseBlocks` 的中断/list 探测改用 `blockPreview` 的 checking 语义，或丢弃前 snapshot 回滚。

C-2（中）`normalizeURI` 放过非十六进制 `%xy`：
- 位置：`syntax.go:189` 用 `isASCIIAlphanumeric` 判断 keepEscaped；mdurl/markdown-it 只保留 `%` + 两位十六进制。
- 风险：`[x](a%zz)` 输出 `href="a%zz"`（参考 `a%25zz`）；同样影响 definition destination 与脚注 id（`html.go:881,1222,1271`）。
- 证据：mdurl `encode.mjs` 的 keepEscaped 分支；fixture 中 `http://a%` → `http://a%25` 证明编码分支存在，但 652+176 项均未覆盖 `%` + 非十六进制字母数字。
- 修复：两处判定改为十六进制。

C-3（低）死代码/恒假参数：
- `inline_resource.go:113-153` 的 `matchInlineRule`/`scanDestination`/`scanLinkTitle` 无调用者；`matchInlineRule` 构造 `parser == nil`，接线后访问 `context.parser` 的规则会 nil panic，且不执行 resolver。
- `referenceLabelRule`（`inline_resource.go:76`）唯一调用点 `inline_grammar.go:178` 的 `empty`/`footnote` 恒为 false；脚注标签文法另写两份，存在漂移。
- 修复：删除未用函数/参数，或统一标签 Rule。

用例缺口（仅列有实际风险者）：C-1 无回归；`TokenAction` 错误传播无覆盖；`%` 非十六进制边界无用例；三处 999 UTF-16 标签上限 Guard 无 999/1000 边界用例。不建议新增接线、mock 回显、源码文本或偶然 token 切分断言。

C 未执行任何命令。复现由 D 用临时 `zz_review_c_test.go` 执行，随后删除：

```go
package markdown

import (
	"strings"
	"testing"
)

func TestReviewCDiscardedProbeResolver(t *testing.T) {
	ran := false
	rule := Capture("probe", Sequence(
		Guard(func(c *TokenizeContext) bool { return !c.Interrupt }),
		String("@x"),
	))
	resolver := &Resolver{Resolve: func(events []Event, c *TokenizeContext) ([]Event, error) {
		for _, event := range events {
			if event.Kind == Enter && event.Token.Type == "codeFenced" {
				ran = true
				c.Node(event.Token).Literal = "CORRUPTED\n"
			}
		}
		return events, nil
	}}
	options := Options{Extensions: []SyntaxExtension{{Flow: map[Code][]Construct{
		Fallback: {{Name: "probe", Rule: rule, ResolveTo: resolver}},
	}}}}
	document, err := Parse("- ```\n  a\n  ```\n\n  b\n  @x\n", options)
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("probe resolver did not run")
	}
	if html := rendered(t, document); strings.Contains(html, "CORRUPTED") {
		t.Fatalf("discarded probe leaked resolver rewrite: %q", html)
	}
}

func TestReviewCPercentEscape(t *testing.T) {
	assertHTML(t, "[x](a%zz)", "<p><a href=\"a%25zz\">x</a></p>\n")
}
```

当时拟定命令与预期：`go test -run 'TestReviewC' -count=1 -v`。预期 C-1 失败且 HTML 含 `CORRUPTED`，C-2 失败，修复后两者通过。此处 C-2 的 `%25zz` 和末尾换行预期均不正确；原临时用例已删除，不作为修复后验收。

### C 处置（主助手）

- C-1 已修复：block 分类、paragraph/list 中断候选使用隔离的 checking 探测，未接受候选不执行或登记 resolver；只有被接受的 Flow 或 ContentInitial 才提交 ResolveTo/ResolveAll。non-concrete Flow 的 checking 与提交使用相同 Document 边界，保留正常 setext/table 与后续 blockquote。
- ResolveTo 的完整 prefix tape 以替换方式提交，避免已提交 CST/AST 前缀被重复追加。永久回归覆盖 discarded Flow、accepted Flow、accepted ContentInitial 和部分行前缀：丢弃时 resolver 零次、原 code 与 metadata 不变；接受时两类 resolver 各一次，改写只出现一次。
- C-2 未采用 hex-only 修改：真实 micromark 4.0.3 保留 `%2F`、`%2f`、`%zz`、`%ag`、`%0G`；只有不足两位或含非 ASCII alphanumeric 的序列才编码 `%`。普通 URI 与 definition destination 的大小写保留；脚注 identifier 先规范化大小写，再编码不足两位的 `%`。永久用例覆盖普通 URI、definition 和脚注 href/id 的实际目标。
- C-3 已由用户 agent 完成：删除未用的资源匹配函数，移除 `referenceLabelRule` 的恒假参数；没有恢复旧 scanner 或兼容入口。
- 新增消费者回归覆盖 TokenAction 在 Text/Flow/ContentInitial 的错误传播，以及显式 reference label、definition 和 GFM footnote 的 999/1000 UTF-16 边界（ASCII 与 astral）。
- 真实 API smoke 发现并修复 ContentInitial 部分行消费的 source slice panic：完整行计数与剩余 raw view 分开维护，空白 suffix 作为 trivia；`@x tail` 的剩余正文、Source、RawSource 和 ByteOffset 均有永久回归。
- 真实 micromark 对照发现并修复 shortcut/collapsed 对链接正文误用 raw-length 上限的问题：999 UTF-16 限制只属于显式标签与 definition/footnote 文法，不限制复用的 link text。1000 UTF-16 的 ASCII/astral shortcut 与 collapsed 均有永久回归。
- 原 discarded-probe 临时用例要求 resolver 必须运行，只用于记录修复前行为；修复后应零次运行，以 `TestBlockProbeResolversCommitOnlyAcceptedFlow` 和 `TestNonConcreteFlowPreviewUsesDocumentBoundary` 为准。不运行已删除的 `TestReviewC` 来伪造通过结果。


## D：整合与最终验收

负责人：主助手。C 审查整合和当前组合子声明入口验收已完成；本轮未重排生产文法。以下六项是 C 返回前的历史验收，不替代本次 C 修复后验收；本次证据独立列于后。

### C 修复前的历史验收

- [x] 核对文档和示例与真实公开 API 一致。
- [x] 最终 native Go 全套通过：CommonMark 652 项、GFM 176 项及其余消费者回归。
- [x] 额外 180 个参考输入的 native 对照通过：两个公开路径共 360 次比较，12 次脚注 callback 的参数和值一致，零差异。
- [x] 真实 smoke 通过：容器、GFM task separator 的 AST 渲染、扩展生命周期、source provenance、错误传播、显式 doc/Math、分块与同步结果一致。
- [x] 串行 race、vet 和 Go 1.22.12 全套测试通过；未运行 benchmark。
- [x] 清理主助手创建的临时 smoke/scaffold，保留有效 fixture、regression、参考记录和本任务文件。

### C 修复后的当前验收

- [x] 九个指定顶层消费者回归及其子用例通过，覆盖 resolver 的 probe/commit、部分行 Source 范围、TokenAction 错误、标签边界、shortcut/collapsed 和百分号目标。
- [x] 37 个真实 API parity 场景通过：逐项比较 Parse、ParseBytes、ParseReader 和单 byte 分块 Parser 的 HTML、源码、CST 事件/范围/provenance 与 AST；关闭前不发布确认结果，Document 不消费事件游标。
- [x] 三个 TokenAction 错误阶段通过同步与流式消费者，解析失败时不发布 Document 或事件。
- [x] 完整 GFM 组合与真实 micromark oracle 一致：表格对齐、删除线、task 状态、三类裸链接、脚注及自定义 clobber prefix、危险 HTML tag filtering 同时生效；Math 的 inline/block TeX 保留与转义、默认 doc/web/url 普通文本和显式 doc 示例均通过。
- [x] 公开 AST 文本编辑使 RenderAST 输出改变，RenderHTML 保留原始解析语义；smoke 不固定文本节点的偶然切分。
- [x] 最新串行完整 suite 通过：CommonMark 652 项、GFM 176 项及其余消费者回归；vet、race、Go 1.22.12 全套和 go doc 均通过。本地 Go 为 1.25.0，未运行 benchmark。
- [x] 验收前后八个生产文件的 SHA-256 完全一致：grammar.go、block_rules.go、inline_grammar.go、inline_resource.go、gfm_block.go、gfm_inline.go、extensions.go、options.go；验收窗口内这些入口与消费者未变化。
- [x] 删除本轮临时 smoke-review-c.go，保留永久回归、参考 JSON 和参考库。

本轮验收命令按以下顺序串行执行：

```sh
go test -json -count=1 ./... &&
go vet ./... &&
go test -race ./... -count=1 -timeout=30s &&
GOTOOLCHAIN=go1.22.12 go test ./... -count=1 -timeout=30s &&
go doc .
```

