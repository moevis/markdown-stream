package core

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// sourceDecoder incrementally builds a sourceDocument. It decodes only newly
// written bytes and retains an incomplete rune or a trailing CR for the next
// chunk, so the total work is linear in the input size.
type sourceDecoder struct {
	encoding Encoding
	doc      *sourceDocument

	raw    strings.Builder
	rawStr string
	text   strings.Builder

	changed bool
	base    int // raw offset after a leading BOM
	rawAt   int // next raw byte to decode
	textAt  int // number of decoded text bytes
	point   Position
	closed  bool
}

func newSourceDecoder(encoding Encoding) (*sourceDecoder, error) {
	if encoding == "" {
		encoding = UTF8
	}
	switch encoding {
	case UTF8, UTF16LE, UTF16BE, Latin1:
	default:
		return nil, fmt.Errorf("markdown: unsupported encoding %q", encoding)
	}
	point := Position{Line: 1, Column: 1}
	return &sourceDecoder{encoding: encoding, doc: &sourceDocument{end: point}, point: point, changed: encoding != UTF8}, nil
}

func (d *sourceDecoder) Write(raw []byte) {
	d.WriteString(string(raw))
}

func (d *sourceDecoder) WriteString(raw string) {
	if d.closed || raw == "" {
		return
	}
	d.raw.WriteString(raw)
	d.rawStr = d.raw.String()
	d.decode(false)
}

func (d *sourceDecoder) Close() {
	if d.closed {
		return
	}
	d.closed = true
	d.rawStr = d.raw.String()
	d.decode(true)
}

func (d *sourceDecoder) Document() *sourceDocument { return d.doc }

func (d *sourceDecoder) decode(closed bool) {
	for d.rawAt < len(d.rawStr) {
		r, size, ok := d.readRune(d.rawAt, closed)
		if !ok {
			break
		}
		if d.rawAt == 0 && r == '\ufeff' {
			d.rawAt += size
			d.base = d.rawAt
			d.point.ByteOffset = d.rawAt
			continue
		}
		start := d.point
		original := r
		if r == 0 {
			r = utf8.RuneError
		}
		if !d.changed && (original == 0 || original == utf8.RuneError && size == 1) {
			d.changed = true
			d.text.WriteString(d.rawStr[d.base:d.rawAt])
		}
		units := 1
		if r > 0xffff {
			units = 2
		}
		textSize := utf8.RuneLen(r)
		lineEnding := r == '\r' || r == '\n'
		crlf := false
		if r == '\r' {
			next, nextSize, nextOK := d.readRune(d.rawAt+size, closed)
			if !nextOK && !closed {
				break
			}
			if nextOK && next == '\n' {
				crlf = true
				size += nextSize
				units++
				textSize++
			}
		}
		if d.changed {
			d.text.WriteRune(r)
			if crlf {
				d.text.WriteByte('\n')
			}
		}
		d.point.ByteOffset += size
		d.point.Offset += units
		if lineEnding {
			d.point.Line++
			d.point.Column = 1
		} else if r == '\t' {
			d.point.Column += 4 - (d.point.Column-1)%4
		} else {
			d.point.Column += units
		}
		linear := r >= 0x20 && r < 0x7f
		run := sourceRun{textStart: d.textAt, textEnd: d.textAt + textSize, start: start, end: d.point, rawStep: size, linear: linear}
		if linear && len(d.doc.runs) != 0 {
			last := &d.doc.runs[len(d.doc.runs)-1]
			if last.linear && last.rawStep == run.rawStep && last.end == start {
				last.textEnd = run.textEnd
				last.end = d.point
			} else {
				d.doc.runs = append(d.doc.runs, run)
			}
		} else {
			d.doc.runs = append(d.doc.runs, run)
		}
		d.rawAt += size
		d.textAt += textSize
	}
	d.doc.raw = d.rawStr
	if d.changed {
		d.doc.text = d.text.String()
	} else {
		d.doc.text = d.rawStr[d.base:]
	}
	d.doc.end = d.point
}

func (d *sourceDecoder) readRune(index int, closed bool) (rune, int, bool) {
	text := d.rawStr
	if index >= len(text) {
		return 0, 0, false
	}
	if d.encoding == UTF8 {
		r, size := utf8.DecodeRuneInString(text[index:])
		if r == utf8.RuneError && size == 1 && !utf8.FullRuneInString(text[index:]) && !closed {
			return 0, 0, false
		}
		return r, size, true
	}
	if d.encoding == Latin1 {
		return rune(text[index]), 1, true
	}
	if index+1 >= len(text) {
		if !closed {
			return 0, 0, false
		}
		return utf8.RuneError, 1, true
	}
	unit := uint16(text[index]) | uint16(text[index+1])<<8
	if d.encoding == UTF16BE {
		unit = uint16(text[index])<<8 | uint16(text[index+1])
	}
	if unit >= 0xd800 && unit <= 0xdbff {
		if index+3 >= len(text) {
			if !closed {
				return 0, 0, false
			}
			return utf8.RuneError, 2, true
		}
		next := uint16(text[index+2]) | uint16(text[index+3])<<8
		if d.encoding == UTF16BE {
			next = uint16(text[index+2])<<8 | uint16(text[index+3])
		}
		if next >= 0xdc00 && next <= 0xdfff {
			return utf16Decode(unit, next), 4, true
		}
		return utf8.RuneError, 2, true
	}
	if unit >= 0xdc00 && unit <= 0xdfff {
		return utf8.RuneError, 2, true
	}
	return rune(unit), 2, true
}

func utf16Decode(high, low uint16) rune {
	return rune(high-0xd800)<<10 + rune(low-0xdc00) + 0x10000
}
