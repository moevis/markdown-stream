package markdown

import (
	"fmt"
	"reflect"
	"testing"
	"unicode/utf16"
)

func utf16Bytes(text string, bigEndian bool) string {
	units := utf16.Encode([]rune(text))
	raw := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		if bigEndian {
			raw = append(raw, byte(unit>>8), byte(unit))
		} else {
			raw = append(raw, byte(unit), byte(unit>>8))
		}
	}
	return string(raw)
}

func TestSourceDecoderChunkBoundaries(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		encoding Encoding
	}{
		{"utf8-rune", "a😀b\r\nc", UTF8},
		{"utf8-bom-nul", "\ufeffa\x00b", UTF8},
		{"utf8-invalid", "x\xffy", UTF8},
		{"utf8-trailing-cr", "a\r", UTF8},
		{"utf8-empty", "", UTF8},
		{"latin1", string([]byte{'a', 0x0a, 0xe9}), Latin1},
		{"utf16le", utf16Bytes("a😀\r\nb", false), UTF16LE},
		{"utf16be", utf16Bytes("a😀\r\nb", true), UTF16BE},
		{"utf16le-odd", string([]byte{'a', 0, 0x3d}), UTF16LE},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			want, err := decodeSource(test.raw, test.encoding)
			if err != nil {
				t.Fatal(err)
			}
			decoder, err := newSourceDecoder(test.encoding)
			if err != nil {
				t.Fatal(err)
			}
			for index := range len(test.raw) {
				decoder.WriteString(test.raw[index : index+1])
			}
			decoder.Close()
			got := decoder.Document()
			if got.raw != want.raw || got.text != want.text || !reflect.DeepEqual(got.runs, want.runs) || got.end != want.end {
				t.Fatalf("document mismatch\ngot: %#v\nwant: %#v", got, want)
			}
			for offset := 0; offset <= len(got.text); offset++ {
				if got.position(offset) != want.position(offset) {
					t.Fatalf("position %d: got %+v, want %+v", offset, got.position(offset), want.position(offset))
				}
			}
		})
	}
}

func TestTokenStreamSplitUnicodeAndCRLF(t *testing.T) {
	for _, source := range []string{
		"# 标题\r\n\r\n正文 😀\r\n",
		"a\r\nb\r\n\r\nc\n",
		"one 😀\n\ntwo\n",
	} {
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
			assertTokenStreamPrefix(t, got, full.Events, fmt.Sprintf("%q@%d", source, cut+1))
		}
		if err := stream.Close(); err != nil {
			t.Fatal(err)
		}
		got = append(got, drainTokenStream(t, stream)...)
		if len(got) != len(full.Events) {
			t.Fatalf("%q: events %d, want %d", source, len(got), len(full.Events))
		}
	}
}

func TestTokenStreamUTF16Capture(t *testing.T) {
	source := "a😀\r\n\r\nbody\n"
	var raw []byte
	for _, r := range source {
		if r > 0xffff {
			r -= 0x10000
			high := uint16(0xd800 + (r >> 10))
			low := uint16(0xdc00 + (r & 0x3ff))
			raw = append(raw, byte(high), byte(high>>8), byte(low), byte(low>>8))
			continue
		}
		raw = append(raw, byte(r), byte(r>>8))
	}
	options := Options{Encoding: UTF16LE}
	full, err := ParseBytes(raw, options)
	if err != nil {
		t.Fatal(err)
	}
	stream := NewTokenStream(options)
	var got []Event
	for index := range raw {
		if _, err := stream.Write(raw[index : index+1]); err != nil {
			t.Fatal(err)
		}
		got = append(got, drainTokenStream(t, stream)...)
		assertTokenStreamPrefix(t, got, full.Events, fmt.Sprintf("byte %d", index+1))
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	got = append(got, drainTokenStream(t, stream)...)
	if len(got) != len(full.Events) {
		t.Fatalf("events %d, want %d", len(got), len(full.Events))
	}
}
