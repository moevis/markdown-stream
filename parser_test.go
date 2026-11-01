package markdown

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

type typedText struct {
	typeName ElemType
	text     string
}

func segments(parts ...typedText) []MarkdownSegment {
	var result []MarkdownSegment
	for _, part := range parts {
		for _, r := range part.text {
			result = append(result, MarkdownSegment{Type: part.typeName, Rune: r})
		}
	}
	return result
}

func parseChunks(t *testing.T, root MarkdownElem, chunks ...string) []MarkdownSegment {
	t.Helper()
	p := NewParser()
	go p.Start(root)
	go func() {
		for _, chunk := range chunks {
			p.Put(chunk)
		}
		p.Close()
	}()
	result := make(chan []MarkdownSegment, 1)
	go func() {
		var output []MarkdownSegment
		for {
			seg, ok := p.Read()
			if !ok {
				result <- output
				return
			}
			output = append(output, seg)
		}
	}()
	select {
	case output := <-result:
		return output
	case <-time.After(3 * time.Second):
		t.Fatal("parser did not finish after input was closed")
		return nil
	}
}

func assertSegments(t *testing.T, got []MarkdownSegment, want ...typedText) {
	t.Helper()
	expected := segments(want...)
	if !slices.Equal(got, expected) {
		t.Fatalf("segments differ:\n got: %#v\nwant: %#v", got, expected)
	}
}

func segmentText(segs []MarkdownSegment) string {
	var text strings.Builder
	for _, seg := range segs {
		text.WriteRune(seg.Rune)
	}
	return text.String()
}

func TestParserStreamsBeforeClose(t *testing.T) {
	p := NewParser()
	go p.Start(MarkdownDoc)
	firstChunk := make(chan struct{}, 1)
	finished := make(chan []MarkdownSegment, 1)
	go func() {
		var output []MarkdownSegment
		for {
			seg, ok := p.Read()
			if !ok {
				finished <- output
				return
			}
			output = append(output, seg)
			if len(output) == len("hello ") {
				firstChunk <- struct{}{}
			}
		}
	}()
	closed := false
	defer func() {
		if !closed {
			p.Close()
		}
	}()
	p.Put("hello ")
	select {
	case <-firstChunk:
	case <-time.After(3 * time.Second):
		t.Fatal("plain text was buffered until input close")
	}
	p.Put("**world**")
	p.Close()
	closed = true
	select {
	case output := <-finished:
		assertSegments(t, output,
			typedText{"paragraph.text", "hello "},
			typedText{"strong.prefix", "**"},
			typedText{"strong.text", "world"},
			typedText{"strong.suffix", "**"},
		)
	case <-time.After(3 * time.Second):
		t.Fatal("parser did not finish the stream")
	}
}

func TestParserLargeConcurrentStream(t *testing.T) {
	const repetitions = 2048
	const input = "正文 **bold** `code` [链接](https://example.test)\n"
	want := segments(
		typedText{"paragraph.text", "正文 "},
		typedText{"strong.prefix", "**"},
		typedText{"strong.text", "bold"},
		typedText{"strong.suffix", "**"},
		typedText{"paragraph.text", " "},
		typedText{"inline-code.prefix", "`"},
		typedText{"inline-code.text", "code"},
		typedText{"inline-code.suffix", "`"},
		typedText{"paragraph.text", " "},
		typedText{"link.[", "["},
		typedText{"link.name", "链接"},
		typedText{"link.]", "]"},
		typedText{"link.(", "("},
		typedText{"link.url", "https://example.test"},
		typedText{"link.)", ")"},
		typedText{"whitespace", "\n"},
	)
	p := NewParser()
	go p.Start(MarkdownDoc)
	go func() {
		for range repetitions {
			p.Put(input)
		}
		p.Close()
	}()
	finished := make(chan error, 1)
	go func() {
		count := 0
		var mismatch error
		for {
			seg, ok := p.Read()
			if !ok {
				break
			}
			if expected := want[count%len(want)]; seg != expected && mismatch == nil {
				mismatch = fmt.Errorf("segment %d: got %#v, want %#v", count, seg, expected)
			}
			count++
		}
		if expected := len(want) * repetitions; count != expected && mismatch == nil {
			mismatch = fmt.Errorf("got %d segments, want %d", count, expected)
		}
		finished <- mismatch
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent input and output stalled beyond the channel buffer sizes")
	}
}

func TestParserNonBlockingReadStates(t *testing.T) {
	p := NewParser()
	if seg, ok := p.ReadNonBlocking(); seg != nil || !ok {
		t.Fatalf("idle read: got (%#v, %t), want (nil, true)", seg, ok)
	}
	finished := make(chan struct{})
	go func() {
		p.Start(MarkdownDoc)
		close(finished)
	}()
	p.Put("X")
	p.Close()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("parser did not close its output")
	}
	seg, ok := p.ReadNonBlocking()
	if !ok || seg == nil || *seg != (MarkdownSegment{Type: "paragraph.text", Rune: 'X'}) {
		t.Fatalf("available read: got (%#v, %t)", seg, ok)
	}
	if _, ok := p.ReadNonBlocking(); ok {
		t.Fatal("exhausted output still reported an open stream")
	}
}

func TestParserUTF8AcrossChunks(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   []typedText
	}{
		{"three-byte-rune", []string{"\xe4", "", "\xbd", "\xa0"}, []typedText{{"paragraph.text", "你"}}},
		{"four-byte-rune", []string{"\xf0\x9f", "\x99", "\x82"}, []typedText{{"paragraph.text", "🙂"}}},
		{"invalid-continuation", []string{"\xe4", "\xff", "x"}, []typedText{{"paragraph.text", "��x"}}},
		{"incomplete-at-eof", []string{"\xe4", "\xbd"}, []typedText{{"paragraph.text", "��"}}},
		{"nul-and-delimiters", []string{"\xe4", "\xbd", "\xa0\x00*", "*好*", "*"}, []typedText{{"paragraph.text", "你\x00"}, {"strong.prefix", "**"}, {"strong.text", "好"}, {"strong.suffix", "**"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertSegments(t, parseChunks(t, MarkdownDoc, tc.chunks...), tc.want...)
		})
	}
}

func FuzzParserPreservesInput(f *testing.F) {
	for _, input := range []string{
		"", "plain 中文🙂", "before\x00after", "\\*literal\\*", "**a _b_**",
		"| h |\n| -x- |\n| v |\n", "| h |\n| --- |\n# next\n",
		"```go\nx := \"```\"\n```\n", "[a\\]b](url)", "a\r\nb\rc",
		"`a\\\nb`", "\xff\xe4\xbd",
	} {
		f.Add(input, uint8(1))
	}
	f.Fuzz(func(t *testing.T, input string, chunkSize uint8) {
		if len(input) > 4096 {
			t.Skip("bound fuzz input to keep each parser run small")
		}
		width := int(chunkSize)%64 + 1
		var chunks []string
		for start := 0; start < len(input); start += width {
			chunks = append(chunks, input[start:min(start+width, len(input))])
		}
		got := segmentText(parseChunks(t, MarkdownDoc, chunks...))
		want := string([]rune(input))
		if got != want {
			t.Fatalf("input changed: got %q, want %q", got, want)
		}
	})
}

func TestParserEOFIsDistinctFromNUL(t *testing.T) {
	p := NewParser()
	p.Put("A\x00B")
	p.Close()
	for _, want := range "A\x00B" {
		if got := p.Peek(); got != want {
			t.Fatalf("Peek() = %U, want %U", got, want)
		}
		if got := p.Next(); got != want {
			t.Fatalf("Next() = %U, want %U", got, want)
		}
	}
	for range 2 {
		if got := p.Peek(); got != EOF {
			t.Fatalf("Peek() after input = %U, want EOF", got)
		}
		if got := p.Next(); got != EOF {
			t.Fatalf("Next() after input = %U, want EOF", got)
		}
	}
}
