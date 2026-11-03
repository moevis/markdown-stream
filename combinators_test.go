package markdown

import "testing"

func runCombinator(t *testing.T, source string, rule Rule) (*TokenizeContext, bool) {
	t.Helper()
	decoded, err := decodeSource(source, UTF8)
	if err != nil {
		t.Fatal(err)
	}
	context := &TokenizeContext{view: decoded.view(), visual: 1}
	return context, rule(context, &Effects{context: context})
}

func TestSepByCombinator(t *testing.T) {
	item, separator := String("a"), String(",")
	for _, test := range []struct {
		source string
		min    int
		max    int
		ok     bool
		index  int
	}{
		{"a,a,a", 1, 0, true, 5},
		{"a,", 1, 0, true, 1},
		{",a", 1, 0, false, 0},
		{"", 0, 0, true, 0},
		{"a,a,a", 1, 2, true, 3},
		{"a,a", 2, 2, true, 3},
		{"a", 2, 2, false, 0},
	} {
		context, ok := runCombinator(t, test.source, SepBy(item, separator, test.min, test.max))
		if ok != test.ok || context.index != test.index {
			t.Fatalf("%q min=%d max=%d: ok=%v index=%d, want ok=%v index=%d", test.source, test.min, test.max, ok, context.index, test.ok, test.index)
		}
	}
	context, ok := runCombinator(t, "a", SepBy(Lookahead(String("a")), Lookahead(String("a")), 0, 0))
	if ok || context.err == nil {
		t.Fatalf("zero-width separated item: ok=%v err=%v", ok, context.err)
	}
}

func TestBindCaptureCombinator(t *testing.T) {
	word := Some(Satisfy(func(code Code) bool { return code >= 'a' && code <= 'z' }))
	rule := BindCapture("word", word, func(_ *TokenizeContext, token *Token) Rule {
		return Guard(func(*TokenizeContext) bool { return token.Source() == "ok" })
	})
	if context, ok := runCombinator(t, "ok", rule); !ok || context.index != 2 {
		t.Fatalf("accepted: ok=%v index=%d", ok, context.index)
	}
	if context, ok := runCombinator(t, "no", rule); ok || context.index != 0 {
		t.Fatalf("rejected: ok=%v index=%d", ok, context.index)
	}
}

func TestManySomeTimesAndOptionalCapture(t *testing.T) {
	if context, ok := runCombinator(t, "aaa", Many(String("a"))); !ok || context.index != 3 {
		t.Fatalf("Many: ok=%v index=%d", ok, context.index)
	}
	if context, ok := runCombinator(t, "", Some(String("a"))); ok || context.index != 0 {
		t.Fatalf("Some empty: ok=%v index=%d", ok, context.index)
	}
	if context, ok := runCombinator(t, "aa", Times(String("a"), 2)); !ok || context.index != 2 {
		t.Fatalf("Times: ok=%v index=%d", ok, context.index)
	}
	rule := OptionalCapture("letter", String("a"))
	if context, ok := runCombinator(t, "", rule); !ok || context.index != 0 || len(context.roots) != 0 {
		t.Fatalf("OptionalCapture absent: ok=%v index=%d roots=%d", ok, context.index, len(context.roots))
	}
	if context, ok := runCombinator(t, "a", rule); !ok || context.index != 1 || len(context.roots) != 1 {
		t.Fatalf("OptionalCapture present: ok=%v index=%d roots=%d", ok, context.index, len(context.roots))
	}
}
