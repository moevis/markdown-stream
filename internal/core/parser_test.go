package core

import (
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestParserConfirmationAndByteBoundaries(t *testing.T) {
	input := "😀 **字**\r\n\n[a](https://example.test)\n"
	whole, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	for cut := 0; cut <= len(input); cut++ {
		parser := NewParser()
		if err := parser.Put(input[:cut]); err != nil {
			t.Fatal(err)
		}
		if _, state, err := parser.ReadNonBlocking(); err != nil || state != ReadPending {
			t.Fatalf("unclosed input published: state %v, error %v", state, err)
		}
		if _, err := parser.Write([]byte(input[cut:])); err != nil {
			t.Fatal(err)
		}
		if err := parser.Close(); err != nil {
			t.Fatal(err)
		}
		document, err := parser.Document()
		if err != nil {
			t.Fatal(err)
		}
		if actual := rendered(t, document); actual != rendered(t, whole) {
			t.Fatalf("byte boundary %d changed HTML: %q", cut, actual)
		}
		var boundaries []struct {
			Kind   EventKind
			Type   TokenType
			Range  SourceRange
			Source string
		}
		for {
			event, available, err := parser.Read()
			if err != nil {
				t.Fatal(err)
			}
			if !available {
				break
			}
			boundaries = append(boundaries, struct {
				Kind   EventKind
				Type   TokenType
				Range  SourceRange
				Source string
			}{event.Kind, event.Token.Type, event.Token.Range, event.Token.Source()})
		}
		var expected []struct {
			Kind   EventKind
			Type   TokenType
			Range  SourceRange
			Source string
		}
		for _, event := range whole.Events {
			expected = append(expected, struct {
				Kind   EventKind
				Type   TokenType
				Range  SourceRange
				Source string
			}{event.Kind, event.Token.Type, event.Token.Range, event.Token.Source()})
		}
		if !reflect.DeepEqual(boundaries, expected) {
			t.Fatalf("byte boundary %d changed concrete source boundaries", cut)
		}
		if _, state, err := parser.ReadNonBlocking(); err != nil || state != ReadDone {
			t.Fatalf("exhaustion: state %v, error %v", state, err)
		}
		if err := parser.Put("later"); !errors.Is(err, ErrClosed) {
			t.Fatalf("closed write: %v", err)
		}
	}
}

func TestDecodedSourceKeepsOriginalBytes(t *testing.T) {
	document, err := Parse("\ufeffa\x00b\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if document.Source() != "\ufeffa\x00b\r\n" || document.NormalizedSource() != "a�b\r\n" {
		t.Fatalf("source decoding: raw %q, normalized %q", document.Source(), document.NormalizedSource())
	}
	assertHTML(t, document.Source(), "<p>a�b</p>\r\n")
	for _, event := range document.Events {
		if event.Kind == Enter && event.Token.Type == "paragraph" {
			if event.Token.Range.Start != (Position{ByteOffset: 3, Offset: 0, Line: 1, Column: 1}) || event.Token.Range.End != (Position{ByteOffset: 6, Offset: 3, Line: 1, Column: 4}) {
				t.Fatalf("BOM/NUL coordinates: %+v", event.Token.Range)
			}
		}
	}
	encoded, err := ParseBytes([]byte{'a', 0, 0x3d, 0xd8, 0, 0xde}, Options{Encoding: UTF16LE})
	if err != nil {
		t.Fatal(err)
	}
	if encoded.NormalizedSource() != "a😀" {
		t.Fatalf("UTF-16 decoding: %q", encoded.NormalizedSource())
	}
	assertHTML(t, "\xff\xe4\xbd", "<p>���</p>")
}

func TestLogicalSourceExcludesContainerMargins(t *testing.T) {
	document, err := Parse("> a\n> b\n")
	if err != nil {
		t.Fatal(err)
	}
	var paragraph *Token
	for _, event := range document.Events {
		if event.Kind == Enter && event.Token.Type == "paragraph" {
			paragraph = event.Token
			break
		}
	}
	if paragraph == nil {
		t.Fatal("missing paragraph")
	}
	if paragraph.Source() != "a\nb" || paragraph.RawSource() != "a\n> b" {
		t.Fatalf("logical %q; physical %q", paragraph.Source(), paragraph.RawSource())
	}
	ranges := paragraph.SourceRanges()
	if len(ranges) != 2 || ranges[0].Start.ByteOffset != 2 || ranges[0].End.ByteOffset != 4 || ranges[1].Start.ByteOffset != 6 || ranges[1].End.ByteOffset != 7 {
		t.Fatalf("source pieces: %+v", ranges)
	}
}

type failingReader struct{ err error }

func (reader failingReader) Read([]byte) (int, error) { return 0, reader.err }

type failingWriter struct{ err error }

func (writer failingWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestInputAndCompilerErrorsPropagate(t *testing.T) {
	failure := errors.New("consumer failure")
	if document, err := ParseReader(failingReader{failure}); document != nil || !errors.Is(err, failure) {
		t.Fatalf("reader failure: document %v, error %v", document, err)
	}
	if document, err := Parse("x", Options{Encoding: "unsupported"}); document != nil || err == nil {
		t.Fatalf("decoder failure: document %v, error %v", document, err)
	}
	parser := NewParser()
	if err := parser.Put("unfinished"); err != nil {
		t.Fatal(err)
	}
	if err := parser.CloseWithError(failure); err != nil {
		t.Fatal(err)
	}
	if document, err := parser.Document(); document != nil || !errors.Is(err, failure) {
		t.Fatalf("producer failure: document %v, error %v", document, err)
	}
	if _, available, err := parser.Read(); available || !errors.Is(err, failure) {
		t.Fatalf("event failure: available %v, error %v", available, err)
	}
	document, err := Parse("x")
	if err != nil {
		t.Fatal(err)
	}
	if err := RenderHTML(failingWriter{failure}, document); !errors.Is(err, failure) {
		t.Fatalf("writer failure: %v", err)
	}
	if err := RenderHTML(io.Discard, document, Options{HTMLExtensions: []HTMLExtension{{Enter: map[TokenType]HTMLHandler{"paragraph": func(*CompileContext, *Token) error { return failure }}}}}); !errors.Is(err, failure) {
		t.Fatalf("compiler failure: %v", err)
	}
}
