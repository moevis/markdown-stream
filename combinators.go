package markdown

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Rule is a composable syntax matcher. A successful rule consumes its input;
// a nonmatching rule must leave the cursor and token tape unchanged. Rules use
// the same Effects as advanced state-machine constructs.
type Rule func(*TokenizeContext, *Effects) bool

// Tokenizer adapts a rule to the advanced Construct.Tokenize interface.
func (rule Rule) Tokenizer() Tokenizer {
	return func(context *TokenizeContext, effects *Effects, ok, nok State) State {
		return func(Code) State {
			if rule != nil && rule(context, effects) {
				return ok(context.Peek())
			}
			return nok(context.Peek())
		}
	}
}

// String matches a literal in preprocessed source. A tab consumes its padding;
// CRLF is one preprocessing code, so it cannot be split by a literal match.
func String(text string) Rule { return literalRule(text, false) }

func literalRule(text string, folded bool) Rule {
	return func(context *TokenizeContext, effects *Effects) bool {
		checkpoint := context.snapshot()
		for index := 0; index < len(text); {
			code := sourceCode(text, index)
			actual := context.Peek()
			if folded {
				if code >= 'A' && code <= 'Z' {
					code += 'a' - 'A'
				}
				if actual >= 'A' && actual <= 'Z' {
					actual += 'a' - 'A'
				}
			}
			if code != actual && !(code == ' ' && actual == VirtualSpace) {
				context.restore(&checkpoint)
				return false
			}
			effects.Consume(context.Peek())
			if context.err != nil {
				return false
			}
			if code == HorizontalTab {
				for context.Peek() == VirtualSpace {
					effects.Consume(VirtualSpace)
				}
			}
			if code == CarriageReturnLineFeed {
				index += 2
			} else {
				_, size := utf8.DecodeRuneInString(text[index:])
				index += size
			}
		}
		return true
	}
}

// Satisfy consumes one preprocessing code accepted by the predicate, never EOF.
func Satisfy(predicate func(Code) bool) Rule {
	return func(context *TokenizeContext, effects *Effects) bool {
		code := context.Peek()
		if code == EOF || !predicate(code) {
			return false
		}
		effects.Consume(code)
		return context.err == nil
	}
}

// Runes consumes one rune from a set. ASCII sets are compiled to a bitset.
func Runes(set string) Rule {
	var ascii [2]uint64
	for _, code := range set {
		if code < 128 {
			ascii[code/64] |= uint64(1) << (code % 64)
		}
	}
	return Satisfy(func(code Code) bool {
		if code < 0 {
			return false
		}
		if code < 128 {
			return ascii[code/64]&(uint64(1)<<(code%64)) != 0
		}
		return strings.ContainsRune(set, rune(code))
	})
}

// Any consumes one preprocessing code, including line endings and tab padding.
func Any() Rule { return Satisfy(func(Code) bool { return true }) }

// Sequence commits only when every rule matches.
func Sequence(rules ...Rule) Rule {
	return func(context *TokenizeContext, effects *Effects) bool {
		checkpoint := context.snapshot()
		for _, rule := range rules {
			if rule == nil || !rule(context, effects) {
				context.restore(&checkpoint)
				return false
			}
		}
		return true
	}
}

// Or tries alternatives in order; a failed alternative cannot leak tokens.
func Or(rules ...Rule) Rule {
	return func(context *TokenizeContext, effects *Effects) bool {
		for _, rule := range rules {
			checkpoint := context.snapshot()
			if rule != nil && rule(context, effects) {
				return true
			}
			context.restore(&checkpoint)
			if context.err != nil {
				return false
			}
		}
		return false
	}
}

// Repeat matches between min and max occurrences. max=0 means unbounded.
// A zero-width body is an error rather than an infinite loop.
func Repeat(rule Rule, min, max int) Rule {
	if min < 0 || max < 0 || max != 0 && max < min {
		panic("markdown: invalid repetition bounds")
	}
	return func(context *TokenizeContext, effects *Effects) bool {
		begin := context.snapshot()
		count := 0
		for max == 0 || count < max {
			checkpoint := context.snapshot()
			if rule == nil || !rule(context, effects) {
				context.restore(&checkpoint)
				break
			}
			if checkpoint.index == context.index && checkpoint.virtual == context.virtual {
				context.Fail(fmt.Errorf("markdown: repetition matched without consuming input"))
				return false
			}
			count++
		}
		if count < min || context.err != nil {
			context.restore(&begin)
			return false
		}
		return true
	}
}

// Optional commits a match, including a zero-width assertion. An absent branch
// restores its effects; unlike repetition, it cannot loop without advancing.
func Optional(rule Rule) Rule {
	return func(context *TokenizeContext, effects *Effects) bool {
		checkpoint := context.snapshot()
		if rule == nil || !rule(context, effects) {
			context.restore(&checkpoint)
		}
		return context.err == nil
	}
}

// Lookahead tests a rule without committing cursor or token effects.
func Lookahead(rule Rule) Rule {
	return func(context *TokenizeContext, effects *Effects) bool {
		checkpoint := context.snapshot()
		matched := rule != nil && rule(context, effects)
		context.restore(&checkpoint)
		return matched && context.err == nil
	}
}

// Not succeeds only when the rule does not match; it never consumes input.
func Not(rule Rule) Rule {
	lookahead := Lookahead(rule)
	return func(context *TokenizeContext, effects *Effects) bool {
		return !lookahead(context, effects) && context.err == nil
	}
}

// Until consumes up to, but not including, a matching terminator. Missing
// terminators roll back the whole rule, including across line endings.
func Until(terminator Rule) Rule {
	lookahead := Lookahead(terminator)
	return func(context *TokenizeContext, effects *Effects) bool {
		checkpoint := context.snapshot()
		for !lookahead(context, effects) {
			if context.Peek() == EOF || context.err != nil {
				context.restore(&checkpoint)
				return false
			}
			effects.Consume(context.Peek())
		}
		return context.err == nil
	}
}

// TokenAction populates a successfully captured token. External side effects
// are the extension's responsibility; token effects participate in rollback.
type TokenAction func(*TokenizeContext, *Token) error

// Capture records a concrete token around a rule, including empty spans.
func Capture(kind TokenType, rule Rule, actions ...TokenAction) Rule {
	return func(context *TokenizeContext, effects *Effects) bool {
		checkpoint := context.snapshot()
		token := effects.Enter(kind)
		if rule == nil || !rule(context, effects) {
			context.restore(&checkpoint)
			return false
		}
		effects.Exit(kind)
		for _, action := range actions {
			if action == nil {
				continue
			}
			if err := action(context, token); err != nil {
				context.Fail(err)
				return false
			}
		}
		return context.err == nil
	}
}

// Content asks the native grammar to expand captured source after outer syntax
// recognition. It works with Capture for document, flow, text, and string scopes.
func Content(kind ContentType) TokenAction {
	return func(_ *TokenizeContext, token *Token) error { token.ContentType = kind; return nil }
}

// End matches the end of a logical source scope without consuming it.
func End() Rule { return Guard(func(context *TokenizeContext) bool { return context.Peek() == EOF }) }

// Guard checks context-dependent grammar constraints without consuming input.
func Guard(predicate func(*TokenizeContext) bool) Rule {
	return func(context *TokenizeContext, _ *Effects) bool { return predicate(context) }
}

// Bind selects a dependent rule after the first rule has matched, sharing one
// transaction. This expresses matching fences and bounded nested resources.
func Bind(first Rule, next func(*TokenizeContext) Rule) Rule {
	return func(context *TokenizeContext, effects *Effects) bool {
		checkpoint := context.snapshot()
		if first != nil && first(context, effects) {
			rule := next(context)
			if rule != nil && rule(context, effects) {
				return true
			}
		}
		context.restore(&checkpoint)
		return false
	}
}

// FoldedString matches ASCII case-insensitively, as required by HTML grammar.
func FoldedString(text string) Rule { return literalRule(text, true) }
