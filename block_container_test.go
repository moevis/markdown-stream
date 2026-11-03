package markdown

import (
	"errors"
	"strings"
	"testing"
)

func transitionContainer(marker string) Construct {
	prefix := Sequence(Capture("transitionPrefix", String(marker)), Optional(Capture("transitionPrefixWhitespace", blockSpaceRule)))
	return Construct{
		Name: "transitionContainer",
		Rule: func(context *TokenizeContext, effects *Effects) bool {
			effects.Enter("transitionContainer")
			return prefix(context, effects)
		},
		Continuation: func(context *TokenizeContext, effects *Effects, ok, nok State) State {
			return func(code Code) State {
				if prefix(context, effects) {
					return ok(context.Peek())
				}
				// Deliberately leave a failed token behind: the driver must roll
				// back the whole continuation, not just the prefix rule.
				effects.Enter("rejectedContinuation")
				if code != EOF {
					effects.Consume(context.Peek())
				}
				effects.Exit("rejectedContinuation")
				return nok(context.Peek())
			}
		},
	}
}

func transitionOptions(constructs ...Construct) Options {
	return Options{Extensions: []SyntaxExtension{{Document: map[Code][]Construct{Fallback: constructs}}}}
}

func transitionText(node *Node) string {
	var text strings.Builder
	node.Walk(func(child *Node) bool {
		switch child.Kind {
		case KindText:
			text.WriteString(child.Literal)
		case KindSoftBreak, KindHardBreak:
			text.WriteByte('\n')
		}
		return true
	})
	return text.String()
}

func transitionParagraph(t *testing.T, root *Node) *Node {
	t.Helper()
	var paragraph *Node
	root.Walk(func(node *Node) bool {
		if paragraph == nil && node.Kind == KindParagraph {
			paragraph = node
		}
		return paragraph == nil
	})
	if paragraph == nil {
		t.Fatal("semantic child paragraph is missing")
	}
	return paragraph
}

func TestDocumentContainerLazyAndExitTransitions(t *testing.T) {
	construct := transitionContainer("@")
	exits := 0
	construct.Exit = func(context *TokenizeContext) error {
		exits++
		found := false
		for _, event := range context.Events {
			if event.Kind == Enter && event.Token.Type == "paragraph" {
				found = true
			}
		}
		if !found {
			return errors.New("container exited before its child flow was parsed")
		}
		return nil
	}
	source := "@ one\nlazy\n@ three\n\noutside"
	document, err := Parse(source, transitionOptions(construct))
	if err != nil {
		t.Fatal(err)
	}
	if exits != 1 || len(document.Root.Children) != 2 {
		t.Fatalf("exits=%d root children=%d", exits, len(document.Root.Children))
	}
	container := document.Root.Children[0]
	if got := transitionText(transitionParagraph(t, container)); got != "one\nlazy\nthree" {
		t.Fatalf("lazy paragraph text: %q", got)
	}
	if got := rendered(t, document); got != "<p>one\nlazy\nthree</p>\n<p>outside</p>" {
		t.Fatalf("lazy paragraph HTML: %q", got)
	}
	if got := container.Token().Source(); got != "@ one\nlazy\n@ three\n" {
		t.Fatalf("container source: %q", got)
	}
	for _, event := range document.Events {
		if event.Token.Type == "rejectedContinuation" {
			t.Fatal("rejected continuation leaked into the CST")
		}
	}
}

func TestDocumentContainerConcreteChildRejectsLazyLine(t *testing.T) {
	document, err := Parse("@ ```\nunmarked\n@ after", transitionOptions(transitionContainer("@")))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Root.Children) != 3 {
		t.Fatalf("root children: %#v", document.Root.Children)
	}
	container := document.Root.Children[0]
	if len(container.Children) != 1 || container.Children[0].Kind != KindCodeBlock || container.Token().Source() != "@ ```\n" {
		t.Fatalf("first concrete container: %#v", container)
	}
	if document.Root.Children[1].Kind != KindParagraph || transitionText(document.Root.Children[1]) != "unmarked" {
		t.Fatalf("rejected lazy line: %#v", document.Root.Children[1])
	}
}

func TestDocumentContainerNativeNestingAndConcreteBarrier(t *testing.T) {
	for _, fixture := range []struct {
		source string
		text   string
	}{
		{"@ > - one\n@ >   two\n@ > - three\n\noutside", "one\ntwo|three|outside"},
		{"@ @ one\n@ @ two\n\noutside", "one\ntwo|outside"},
	} {
		document, err := Parse(fixture.source, transitionOptions(transitionContainer("@")))
		if err != nil {
			t.Fatal(err)
		}
		var paragraphs []string
		document.Root.Walk(func(node *Node) bool {
			if node.Kind == KindParagraph {
				paragraphs = append(paragraphs, transitionText(node))
				return false
			}
			return true
		})
		if got := strings.Join(paragraphs, "|"); got != fixture.text {
			t.Fatalf("nesting %q paragraph text: %q", fixture.source, got)
		}
	}
	document, err := Parse("@ ```\n@ @ inside\n@ ```", transitionOptions(transitionContainer("@")))
	if err != nil {
		t.Fatal(err)
	}
	container := document.Root.Children[0]
	if len(container.Children) != 1 || container.Children[0].Kind != KindCodeBlock || container.Children[0].Literal != "@ inside\n" {
		t.Fatalf("container pierced concrete child: %#v", container.Children)
	}
}

func TestConstructConcreteControlsDocumentInterruption(t *testing.T) {
	for _, opener := range []string{"@ b", "> b", "- b"} {
		for _, concrete := range []bool{false, true} {
			flow := Construct{Name: "transitionFlow", Concrete: concrete, Rule: Capture("transitionFlow", Sequence(String("!"), Until(End())), func(context *TokenizeContext, token *Token) error {
				node := context.Node(token)
				node.Kind, node.Literal = KindCodeBlock, token.Source()
				return nil
			})}
			options := transitionOptions(transitionContainer("@"))
			options.Extensions[0].Flow = map[Code][]Construct{'!': {flow}}
			document, err := Parse("!a\n"+opener, options)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if concrete {
				want = 1
			}
			if len(document.Root.Children) != want {
				t.Fatalf("Concrete=%v opener=%q children=%d", concrete, opener, len(document.Root.Children))
			}
			if concrete && document.Root.Children[0].Literal != "!a\n"+opener {
				t.Fatalf("concrete source was stripped: %q", document.Root.Children[0].Literal)
			}
		}
	}
}

func TestDocumentContainerPartialIdentityAndOpeningTokens(t *testing.T) {
	outer := transitionContainer("@")
	outer.Name = "outerContainer"
	partial := transitionContainer("%")
	partial.Name, partial.Partial = "partialContainer", true
	opening := partial.Rule
	partial.Rule = func(context *TokenizeContext, effects *Effects) bool {
		if context.Peek() != '%' {
			return false
		}
		if context.CurrentConstruct == nil || context.CurrentConstruct.Name != outer.Name {
			context.Fail(errors.New("partial container replaced its outer identity"))
			return false
		}
		effects.Enter("nestedOpening")
		return opening(context, effects)
	}
	document, err := Parse("@ % one\n@ % two", transitionOptions(outer, partial))
	if err != nil {
		t.Fatal(err)
	}
	paragraph := transitionParagraph(t, document.Root)
	if got := transitionText(paragraph); got != "one\ntwo" {
		t.Fatalf("multiple-opening paragraph text: %q", got)
	}
	if got := rendered(t, document); got != "<p>one\ntwo</p>" {
		t.Fatalf("multiple-opening HTML: %q", got)
	}
	var nested *Node
	document.Root.Walk(func(node *Node) bool {
		if node.Token() != nil && node.Token().Type == "nestedOpening" {
			nested = node
			return false
		}
		return true
	})
	if nested == nil || transitionParagraph(t, nested) != paragraph {
		t.Fatal("opening wrapper lost semantic ownership of its paragraph")
	}
	wrapperPreserved := false
	for _, child := range nested.Token().Children {
		if child.Type == "transitionContainer" {
			wrapperPreserved = true
			if child.Source() != "% one\n% two" {
				t.Fatalf("inner opening CST source: %q", child.Source())
			}
		}
	}
	if !wrapperPreserved {
		t.Fatal("inner Effects opening wrapper was removed from the CST")
	}
}

func TestDocumentContainerPositionsAndExitErrors(t *testing.T) {
	source := "> @\tone\r\n> @\ttwo\r\n\r\nend"
	document, err := Parse(source, transitionOptions(transitionContainer("@")))
	if err != nil {
		t.Fatal(err)
	}
	container := document.Root.Children[0].Children[0]
	paragraph := transitionParagraph(t, container)
	if paragraph.Range.Start.ByteOffset != strings.Index(source, "one") || paragraph.Range.Start.Line != 1 || paragraph.Range.End.Line != 2 {
		t.Fatalf("paragraph physical range: %#v", paragraph.Range)
	}
	if !strings.Contains(container.Token().Source(), "@\tone\r\n") {
		t.Fatalf("tab/CRLF source lost: %q", container.Token().Source())
	}
	failure := errors.New("container exit failure")
	for _, fail := range []func(*TokenizeContext) error{
		func(*TokenizeContext) error { return failure },
		func(context *TokenizeContext) error { context.Fail(failure); return nil },
	} {
		construct := transitionContainer("@")
		construct.Exit = fail
		if _, err := Parse("@ one\n\noutside", transitionOptions(construct)); !errors.Is(err, failure) {
			t.Fatalf("exit error: %v", err)
		}
	}
}

func TestFootnotePendingBlankOnlyLoosensItsOwningList(t *testing.T) {
	options := Options{Extensions: []SyntaxExtension{GFM()}}
	for _, fixture := range []struct {
		source string
		tight  bool
	}{
		{"* [^3]: Defined in a list item.\n\n  More.\n", false},
		{"* [^3]: First.\n\n      Second.\n* Next.\n", true},
		{"* First.\n\n  [^3]: Definition.\n\n      Second.\n", false},
	} {
		document, err := Parse(fixture.source, options)
		if err != nil {
			t.Fatal(err)
		}
		list := document.Root.Children[0]
		if list.Kind != KindList || list.Tight != fixture.tight {
			t.Fatalf("source=%q tight=%v want=%v", fixture.source, list.Tight, fixture.tight)
		}
	}
}

func TestDocumentTokenizerOpeningAndContinuationErrors(t *testing.T) {
	construct := transitionContainer("@")
	opening := construct.Rule
	construct.Rule = nil
	construct.Tokenize = func(context *TokenizeContext, effects *Effects, ok, nok State) State {
		return func(Code) State {
			if opening(context, effects) {
				return ok(context.Peek())
			}
			return nok(context.Peek())
		}
	}
	document, err := Parse("- first\n\n@ second\n@ third", transitionOptions(construct))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Root.Children) != 2 || document.Root.Children[0].Kind != KindList || document.Root.Children[1].Kind != KindCustom {
		t.Fatalf("document container escaped its list-item scope: %#v", document.Root.Children)
	}
	if got := transitionText(transitionParagraph(t, document.Root.Children[1])); got != "second\nthird" {
		t.Fatalf("tokenizer-opened flow: %q", got)
	}
	failure := errors.New("container continuation failure")
	construct.Continuation = func(context *TokenizeContext, effects *Effects, ok, nok State) State {
		return func(Code) State {
			context.Fail(failure)
			return nil
		}
	}
	if _, err := Parse("@ one\n@ two", transitionOptions(construct)); !errors.Is(err, failure) {
		t.Fatalf("continuation error: %v", err)
	}
}

func TestDocumentContinuationResolversPreserveChildFlow(t *testing.T) {
	source := "@ | a |\n@ | - |\n@ | b |\n\noutside"
	commits := map[int]int{}
	prefix := Construct{Name: "continuationPrefix", Rule: Capture("continuationPrefix", Sequence(String("@"), Optional(String(" "))))}
	prefix.Resolve = &Resolver{Resolve: func(events []Event, context *TokenizeContext) ([]Event, error) {
		for _, event := range events {
			if event.Kind == Enter && event.Token.Type == "continuationPrefix" {
				commits[event.Token.Range.Start.ByteOffset]++
			}
		}
		return events, nil
	}}
	container := transitionContainer("@")
	container.Continuation = func(context *TokenizeContext, effects *Effects, ok, nok State) State {
		return effects.Attempt([]Construct{prefix}, ok, nok)
	}
	exits := 0
	container.Exit = func(context *TokenizeContext) error {
		exits++
		for _, event := range context.Events {
			if event.Kind == Enter && event.Token.Type == "table" {
				return nil
			}
		}
		return errors.New("container exited without its settled table")
	}
	options := transitionOptions(container)
	options.Extensions = append(options.Extensions, GFM())
	options.HTMLExtensions = []HTMLExtension{GFMHTML()}
	document, err := Parse(source, options)
	if err != nil {
		t.Fatal(err)
	}
	if got := rendered(t, document); got != "<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>b</td>\n</tr>\n</tbody>\n</table>\n<p>outside</p>" {
		t.Fatalf("continuation resolver lost child flow: %q", got)
	}
	if exits != 1 || len(commits) != 2 {
		t.Fatalf("exits=%d committed continuations=%v", exits, commits)
	}
	for _, marker := range []string{"@ | - |", "@ | b |"} {
		if count := commits[strings.Index(source, marker)]; count != 1 {
			t.Fatalf("continuation %q committed %d times", marker, count)
		}
	}
}

func TestDocumentContinuationResolveToRollsBackChildEdits(t *testing.T) {
	source := "before\n\n@ | a |\n@ | - |\n@ | b |\n@ stop\noutside"
	stop := strings.Index(source, "@ stop")
	mutations, sawPrefix := 0, false
	prefix := Construct{Name: "rollbackPrefix", Rule: Capture("rollbackPrefix", Sequence(String("@"), Optional(String(" "))))}
	prefix.ResolveTo = &Resolver{Resolve: func(events []Event, context *TokenizeContext) ([]Event, error) {
		latest := -1
		var table *Token
		for _, event := range events {
			if event.Kind != Enter {
				continue
			}
			switch event.Token.Type {
			case "rollbackPrefix":
				latest = event.Token.Range.Start.ByteOffset
			case "paragraph":
				sawPrefix = sawPrefix || event.Token.Source() == "before"
			case "table":
				table = event.Token
			}
		}
		if latest == stop {
			if table == nil {
				return nil, errors.New("continuation ResolveTo missed its committed table")
			}
			mutations++
			table.Type = "data"
			table.Data = map[string]any{"leaked": true}
			node := context.Node(table)
			node.Kind, node.Literal, node.Children = KindText, "corrupt", nil
		}
		return events, nil
	}}
	container := transitionContainer("@")
	// The rejected line must not open a replacement container.
	container.Rule = Sequence(container.Rule, Not(String("s")))
	container.Continuation = func(context *TokenizeContext, effects *Effects, ok, nok State) State {
		after := func(code Code) State {
			if code == 's' {
				return nok(code)
			}
			return ok(code)
		}
		return effects.Attempt([]Construct{prefix}, after, nok)
	}
	options := transitionOptions(container)
	options.Extensions = append(options.Extensions, GFM())
	options.HTMLExtensions = []HTMLExtension{GFMHTML()}
	document, err := Parse(source, options)
	if err != nil {
		t.Fatal(err)
	}
	want := "<p>before</p>\n<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>b</td>\n</tr>\n</tbody>\n</table>\n<p>@ stop\noutside</p>"
	if got := rendered(t, document); got != want {
		t.Fatalf("failed continuation leaked child edits: %q", got)
	}
	if mutations != 1 || !sawPrefix {
		t.Fatalf("ResolveTo mutations=%d saw committed prefix=%t", mutations, sawPrefix)
	}
	tables := 0
	document.Root.Walk(func(node *Node) bool {
		if node.Kind == KindTable {
			tables++
			if node.Token().Type != "table" || node.Token().Data["leaked"] != nil {
				t.Fatal("failed continuation leaked token or metadata edits")
			}
		}
		return true
	})
	if tables != 1 {
		t.Fatal("failed continuation lost its semantic table")
	}
}

func TestBlockProbeResolversCommitOnlyAcceptedFlow(t *testing.T) {
	for _, test := range []struct {
		name, source, html, literal string
		commits                     int
		contentInitial              bool
	}{
		{
			name:    "discarded",
			source:  "- ```\n  a\n  ```\n\n  b\n  @x\n",
			html:    "<ul>\n<li>\n<pre><code>a\n</code></pre>\n<p>b\n@x</p>\n</li>\n</ul>\n",
			literal: "a\n",
		},
		{
			name:    "accepted",
			source:  "- ```\n  a\n  ```\n\n  @x\n",
			html:    "<ul>\n<li>\n<pre><code>resolved\nall\n</code></pre>\n</li>\n</ul>\n",
			literal: "resolved\nall\n", commits: 1,
		},
		{
			name:    "accepted content initial",
			source:  "- ```\n  a\n  ```\n\n  @x\n",
			html:    "<ul>\n<li>\n<pre><code>resolved\nall\n</code></pre>\n</li>\n</ul>\n",
			literal: "resolved\nall\n", commits: 1, contentInitial: true,
		},
		{
			name:    "accepted content initial prefix",
			source:  "- ```\n  a\n  ```\n\n  @x tail\n",
			html:    "<ul>\n<li>\n<pre><code>resolved\nall\n</code></pre>\n<p>tail</p>\n</li>\n</ul>\n",
			literal: "resolved\nall\n", commits: 1, contentInitial: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			toCalls, allCalls := 0, 0
			resolveTo := &Resolver{Resolve: func(events []Event, context *TokenizeContext) ([]Event, error) {
				toCalls++
				for _, event := range events {
					if event.Kind == Enter && event.Token.Type == "codeFenced" {
						context.Node(event.Token).Literal = "resolved\n"
						event.Token.Data = map[string]any{"resolveTo": true}
					}
				}
				return events, nil
			}}
			resolveAll := &Resolver{Resolve: func(events []Event, context *TokenizeContext) ([]Event, error) {
				allCalls++
				for _, event := range events {
					if event.Kind == Enter && event.Token.Type == "codeFenced" {
						context.Node(event.Token).Literal += "all\n"
						if event.Token.Data == nil {
							event.Token.Data = make(map[string]any)
						}
						event.Token.Data["resolveAll"] = true
					}
				}
				return events, nil
			}}
			construct := Construct{
				Name: "probe",
				Rule: Capture("probe", Sequence(
					Guard(func(context *TokenizeContext) bool { return !context.Interrupt }),
					String("@x"),
				)),
				ResolveTo: resolveTo, ResolveAll: resolveAll,
			}
			syntax := SyntaxExtension{Flow: map[Code][]Construct{Fallback: {construct}}}
			if test.contentInitial {
				syntax.Flow, syntax.ContentInitial = nil, syntax.Flow
			}
			options := Options{Extensions: []SyntaxExtension{syntax}}
			document, err := Parse(test.source, options)
			if err != nil {
				t.Fatal(err)
			}
			if got := rendered(t, document); got != test.html {
				t.Fatalf("resolver commit changed HTML: %q", got)
			}
			if toCalls != test.commits || allCalls != test.commits {
				t.Fatalf("ResolveTo calls=%d ResolveAll calls=%d, want %d", toCalls, allCalls, test.commits)
			}
			var code, tail *Node
			document.Root.Walk(func(node *Node) bool {
				if node.Kind == KindCodeBlock {
					code = node
				}
				if node.Kind == KindText && node.Literal == "tail" {
					tail = node
				}
				return true
			})
			if code == nil || code.Literal != test.literal {
				t.Fatalf("resolver commit changed the public code node: %#v", code)
			}
			for _, key := range []string{"resolveTo", "resolveAll"} {
				if got := code.Token().Data[key]; test.commits == 0 && got != nil || test.commits == 1 && got != true {
					t.Fatalf("resolver metadata %s=%v", key, got)
				}
			}
			if offset := strings.Index(test.source, "tail"); offset >= 0 {
				if tail == nil || tail.Token().Source() != "tail" || tail.Token().RawSource() != "tail" || tail.Range.Start.ByteOffset != offset || tail.Range.End.ByteOffset != offset+len("tail") {
					t.Fatalf("content prefix lost remaining source provenance: %#v", tail)
				}
			}
		})
	}
}

func TestNonConcreteFlowPreviewUsesDocumentBoundary(t *testing.T) {
	for _, test := range []struct {
		name, source, candidate, html string
	}{
		{
			name:      "table",
			source:    "b\n| a |\n| - |\n> x\n",
			candidate: "| - |\n>",
			html:      "<p>b</p>\n<table>\n<thead>\n<tr>\n<th>a</th>\n</tr>\n</thead>\n</table>\n<blockquote>\n<p>x</p>\n</blockquote>\n",
		},
		{
			name:      "setext",
			source:    "line\n---\n> x\n",
			candidate: "---\n>",
			html:      "<h2>line</h2>\n<blockquote>\n<p>x</p>\n</blockquote>\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			construct := Construct{Name: "futureDocument", Rule: Capture("futureDocument", Sequence(
				Guard(func(context *TokenizeContext) bool { return !context.Interrupt }),
				String(test.candidate),
			))}
			options := Options{
				Extensions:     []SyntaxExtension{GFM(), {Flow: map[Code][]Construct{Fallback: {construct}}}},
				HTMLExtensions: []HTMLExtension{GFMHTML()},
			}
			assertHTML(t, test.source, test.html, options)
		})
	}
}
