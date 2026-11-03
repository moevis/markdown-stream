package markdown

import (
	"errors"
	"strings"
	"testing"
)

func TestRuleAlternativesRollbackCapturedSyntax(t *testing.T) {
	rule := Or(Capture("failed", Sequence(String("@"), Until(String("!")), String("!"))), Capture("data", String("@ok"), func(context *TokenizeContext, token *Token) error {
		node := context.Node(token)
		node.Kind, node.Literal = KindText, "accepted"
		return nil
	}))
	options := Options{Extensions: []SyntaxExtension{{Text: map[Code][]Construct{'@': {{Name: "alternative", Rule: rule}}}}}}
	document, err := Parse("@ok tail", options)
	if err != nil {
		t.Fatal(err)
	}
	if actual := rendered(t, document); actual != "<p>accepted tail</p>" {
		t.Fatalf("HTML: %q", actual)
	}
	for _, event := range document.Events {
		if event.Token.Type == "failed" {
			t.Fatal("failed branch leaked into concrete syntax")
		}
	}
}

func TestEffectsPartialLookaheadAndResolvers(t *testing.T) {
	outer := Construct{Name: "outer"}
	resolutions := 0
	child := Construct{Name: "child", Partial: true}
	child.Rule = Capture("data", String("@a"), func(context *TokenizeContext, token *Token) error {
		if context.CurrentConstruct == nil || context.CurrentConstruct.Name != "outer" {
			return errors.New("partial replaced current construct")
		}
		node := context.Node(token)
		node.Kind, node.Literal = KindText, token.Source()
		return nil
	})
	child.Resolve = &Resolver{Resolve: func(events []Event, context *TokenizeContext) ([]Event, error) {
		resolutions++
		context.Node(events[0].Token).Literal = "resolved"
		return events, nil
	}}
	outer.Tokenize = func(context *TokenizeContext, effects *Effects, ok, nok State) State {
		effects.Enter("outer")
		finish := State(func(code Code) State { effects.Exit("outer"); return ok(code) })
		attempt := effects.Attempt([]Construct{child}, finish, nok)
		interrupt := effects.Interrupt([]Construct{child}, attempt, nok)
		return effects.Check([]Construct{child}, interrupt, nok)
	}
	assertHTML(t, "@a", "<p>resolved</p>", Options{Extensions: []SyntaxExtension{{Text: map[Code][]Construct{'@': {outer}}}}})
	if resolutions != 1 {
		t.Fatalf("committed resolver calls: %d", resolutions)
	}
}

func TestResolverRewritesRollbackWithOuterRule(t *testing.T) {
	inner := Construct{Name: "inner", Rule: Capture("data", String("@ok"), func(context *TokenizeContext, token *Token) error {
		node := context.Node(token)
		node.Kind, node.Literal = KindText, token.Source()
		return nil
	})}
	prefixRewritten := false
	inner.ResolveTo = &Resolver{Resolve: func(events []Event, context *TokenizeContext) ([]Event, error) {
		for _, event := range events {
			if event.Kind == Enter && event.Token.Type == "data" {
				if event.Token.Source() == "before" {
					prefixRewritten = true
				}
				context.Node(event.Token).Literal = "corrupted"
				event.Token.Data = map[string]any{"changed": true}
			}
		}
		return events, nil
	}}
	attempt := Rule(func(context *TokenizeContext, effects *Effects) bool {
		accepted := false
		ok := State(func(Code) State { accepted = true; return nil })
		state := effects.Attempt([]Construct{inner}, ok, func(Code) State { return nil })
		for state != nil {
			state = state(context.Peek())
		}
		return accepted
	})
	fallback := Capture("data", String("@ok"), func(context *TokenizeContext, token *Token) error {
		node := context.Node(token)
		node.Kind, node.Literal = KindText, "accepted"
		return nil
	})
	outer := Construct{Name: "outer", Rule: Or(Sequence(attempt, String("?")), fallback)}
	assertHTML(t, "before @ok", "<p>before accepted</p>", Options{Extensions: []SyntaxExtension{{Text: map[Code][]Construct{'@': {outer}}}}})
	if !prefixRewritten {
		t.Fatal("nested ResolveTo did not see committed prefix")
	}
}

func TestInvalidResolverAndRuleErrorsPropagate(t *testing.T) {
	failure := errors.New("resolver failure")
	for _, resolver := range []*Resolver{
		{Resolve: func([]Event, *TokenizeContext) ([]Event, error) { return nil, failure }},
		{Resolve: func(events []Event, context *TokenizeContext) ([]Event, error) {
			context.Fail(failure)
			return events, nil
		}},
	} {
		_, err := Parse("@", Options{Extensions: []SyntaxExtension{{Text: map[Code][]Construct{'@': {{Name: "failure", Rule: Capture("value", String("@")), Resolve: resolver}}}}}})
		if !errors.Is(err, failure) {
			t.Fatalf("resolver error: %v", err)
		}
	}
	duplicate := &Resolver{Resolve: func(events []Event, _ *TokenizeContext) ([]Event, error) { return append(events, events...), nil }}
	if _, err := Parse("@", Options{Extensions: []SyntaxExtension{{Text: map[Code][]Construct{'@': {{Name: "duplicate", Rule: Capture("value", String("@")), Resolve: duplicate}}}}}}); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("duplicate token topology: %v", err)
	}
	if _, err := Parse("@", Options{Extensions: []SyntaxExtension{{Text: map[Code][]Construct{'@': {{Name: "zero", Rule: Repeat(Lookahead(String("@")), 1, 0)}}}}}}); err == nil || !strings.Contains(err.Error(), "without consuming") {
		t.Fatalf("zero-width repetition: %v", err)
	}
}

func TestTokenActionErrorsStopAlternatives(t *testing.T) {
	failure := errors.New("token action failure")
	for _, phase := range []string{"text", "flow", "content initial"} {
		t.Run(phase, func(t *testing.T) {
			construct := Construct{Name: "actionError", Rule: Or(
				Capture("failed", String("@"), func(*TokenizeContext, *Token) error { return failure }),
				Capture("data", String("@")),
			)}
			var syntax SyntaxExtension
			constructs := map[Code][]Construct{'@': {construct}}
			switch phase {
			case "text":
				syntax.Text = constructs
			case "flow":
				syntax.Flow = constructs
			case "content initial":
				syntax.ContentInitial = constructs
			}
			options := Options{Extensions: []SyntaxExtension{syntax}}
			if document, err := Parse("@ tail", options); document != nil || !errors.Is(err, failure) {
				t.Fatalf("action error was swallowed: document %v, error %v", document, err)
			}
			parser := NewParser(options)
			for _, chunk := range []string{"@", " tail"} {
				if err := parser.Put(chunk); err != nil {
					t.Fatal(err)
				}
			}
			if err := parser.Close(); err != nil {
				t.Fatal(err)
			}
			if document, err := parser.Document(); document != nil || !errors.Is(err, failure) {
				t.Fatalf("action error was published as a document: document %v, error %v", document, err)
			}
			if _, available, err := parser.Read(); available || !errors.Is(err, failure) {
				t.Fatalf("action error was published as an event: available %v, error %v", available, err)
			}
			if _, status, err := parser.ReadNonBlocking(); status != ReadDone || !errors.Is(err, failure) {
				t.Fatalf("non-blocking action error: status %v, error %v", status, err)
			}
		})
	}
}
