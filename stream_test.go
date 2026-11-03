package markdown

import (
	"errors"
	"fmt"
	"testing"
)

func drainTokenStream(t *testing.T, stream *TokenStream) []Event {
	t.Helper()
	var events []Event
	for {
		event, status, err := stream.Next()
		if err != nil {
			t.Fatal(err)
		}
		if status == ReadEvent {
			events = append(events, event)
			continue
		}
		return events
	}
}

func assertTokenStreamPrefix(t *testing.T, got, full []Event, at string) {
	t.Helper()
	if len(got) > len(full) {
		t.Fatalf("at %q emitted %d events, document has %d", at, len(got), len(full))
	}
	for index := range got {
		if streamEventSig(got[index]) != streamEventSig(full[index]) {
			t.Fatalf("at %q event %d: got %#v, want %#v", at, index, streamEventSig(got[index]), streamEventSig(full[index]))
		}
	}
}

func streamHasType(events []Event, kind TokenType) bool {
	for _, event := range events {
		if event.Token.Type == kind {
			return true
		}
	}
	return false
}

func TestTokenStreamKeepsDocumentPrefix(t *testing.T) {
	sources := []string{
		"# h\n\n```\ncode\n```\n\ntail\n",
		"[foo]\n\n[foo]: /url\n",
		"- a\n- b\n\nparagraph\n",
		"> quote\n\nparagraph\n",
		"para\n---\n",
		"| a |\n| - |\n| b |\n",
		"hello\n\nworld",
		"```\nopen",
		"---\n",
	}
	for index, source := range sources {
		t.Run(fmt.Sprintf("%d", index), func(t *testing.T) {
			full, err := Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			stream := NewTokenStream()
			var got []Event
			for cut := range len(source) {
				if err := stream.Put(source[cut : cut+1]); err != nil {
					t.Fatal(err)
				}
				got = append(got, drainTokenStream(t, stream)...)
				assertTokenStreamPrefix(t, got, full.Events, source[:cut+1])
			}
			if err := stream.Close(); err != nil {
				t.Fatal(err)
			}
			got = append(got, drainTokenStream(t, stream)...)
			if _, status, err := stream.Next(); err != nil || status != ReadDone {
				t.Fatalf("done: status=%v err=%v", status, err)
			}
			if len(got) != len(full.Events) {
				t.Fatalf("emitted %d events, document has %d", len(got), len(full.Events))
			}
			for event := range got {
				if streamEventSig(got[event]) != streamEventSig(full.Events[event]) {
					t.Fatalf("event %d changed after close", event)
				}
			}
		})
	}
}

func TestTokenStreamEmitsClosedBlocksBeforeClose(t *testing.T) {
	stream := NewTokenStream()
	if err := stream.Put("# heading\n"); err != nil {
		t.Fatal(err)
	}
	events := drainTokenStream(t, stream)
	if !streamHasType(events, "atxHeading") {
		t.Fatalf("heading not emitted before close: %#v", events)
	}
	if err := stream.Put("\n```\ncode\n```\n"); err != nil {
		t.Fatal(err)
	}
	events = drainTokenStream(t, stream)
	if !streamHasType(events, "codeFenced") {
		t.Fatalf("closed fence not emitted before close: %#v", events)
	}
	if _, status, err := stream.Next(); err != nil || status != ReadPending {
		t.Fatalf("pending: status=%v err=%v", status, err)
	}
}

func TestTokenStreamHoldsForwardReferences(t *testing.T) {
	const source = "[foo]\n\n[foo]: /url\n"
	stream := NewTokenStream()
	if err := stream.Put("[foo]\n\n"); err != nil {
		t.Fatal(err)
	}
	if events := drainTokenStream(t, stream); len(events) != 0 {
		t.Fatalf("reference paragraph emitted early: %#v", events)
	}
	if err := stream.Put("[foo]: /url\n"); err != nil {
		t.Fatal(err)
	}
	if events := drainTokenStream(t, stream); len(events) != 0 {
		t.Fatalf("block before unresolved reference emitted early: %#v", events)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	events := drainTokenStream(t, stream)
	full, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(full.Events) {
		t.Fatalf("emitted %d events, document has %d", len(events), len(full.Events))
	}
	if !streamHasType(events, "link") {
		t.Fatalf("resolved link missing: %#v", events)
	}
}

func TestTokenStreamHoldsResolverSyntax(t *testing.T) {
	resolver := &Resolver{Resolve: func(events []Event, _ *TokenizeContext) ([]Event, error) {
		return events, nil
	}}
	syntax := SyntaxExtension{Text: map[Code][]Construct{
		'@': {{Name: "at", Rule: Capture("at", String("@")), Resolve: resolver}},
	}}
	stream := NewTokenStream(Options{Extensions: []SyntaxExtension{syntax}})
	if err := stream.Put("# heading\n"); err != nil {
		t.Fatal(err)
	}
	if events := drainTokenStream(t, stream); len(events) != 0 {
		t.Fatalf("resolver syntax streamed early: %#v", events)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if events := drainTokenStream(t, stream); len(events) == 0 {
		t.Fatal("resolver syntax lost events after close")
	}
}

func TestTokenStreamStatusAndClose(t *testing.T) {
	stream := NewTokenStream()
	if _, status, err := stream.Next(); err != nil || status != ReadPending {
		t.Fatalf("initial: status=%v err=%v", status, err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if events := drainTokenStream(t, stream); len(events) != 0 {
		t.Fatalf("empty stream events: %#v", events)
	}
	if _, status, err := stream.Next(); err != nil || status != ReadDone {
		t.Fatalf("done: status=%v err=%v", status, err)
	}
	if err := stream.Put("x"); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after close: %v", err)
	}
}

func TestTokenStreamKeepsSourceText(t *testing.T) {
	const source = "# title\n\nbody\n"
	stream := NewTokenStream()
	for cut := range len(source) {
		if err := stream.Put(source[cut : cut+1]); err != nil {
			t.Fatal(err)
		}
		for _, event := range drainTokenStream(t, stream) {
			if event.Token.Source() != source[event.Token.Range.Start.ByteOffset:event.Token.Range.End.ByteOffset] {
				t.Fatalf("token source %q does not match range", event.Token.Source())
			}
		}
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	for _, event := range drainTokenStream(t, stream) {
		if event.Token.Source() != source[event.Token.Range.Start.ByteOffset:event.Token.Range.End.ByteOffset] {
			t.Fatalf("token source %q does not match range", event.Token.Source())
		}
	}
}
