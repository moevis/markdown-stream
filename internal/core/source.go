package core

import (
	"sort"
	"strings"
)

// Runs compress ordinary ASCII while retaining transformations as individual
// mappings. They map logical UTF-8 boundaries back to original input bytes.
type sourceRun struct {
	textStart int
	textEnd   int
	start     Position
	end       Position
	rawStep   int
	linear    bool
}

type sourceDocument struct {
	raw  string
	text string
	runs []sourceRun
	end  Position
}

func decodeSource(raw string, encoding Encoding) (*sourceDocument, error) {
	decoder, err := newSourceDecoder(encoding)
	if err != nil {
		return nil, err
	}
	decoder.WriteString(raw)
	decoder.Close()
	return decoder.Document(), nil
}

func (d *sourceDocument) position(index int) Position {
	if index >= len(d.text) {
		return d.end
	}
	i := sort.Search(len(d.runs), func(i int) bool { return d.runs[i].textEnd > index })
	if i == len(d.runs) {
		return d.end
	}
	run := &d.runs[i]
	point := run.start
	if run.linear {
		delta := index - run.textStart
		point.ByteOffset += delta * run.rawStep
		point.Offset += delta
		point.Column += delta
	}
	return point
}

// A part maps a logical substring to a contiguous normalized source substring.
// Padding retains the original tab enclosure instead of inventing source bytes.
type sourcePart struct {
	start         int
	end           int
	sourceStart   int
	sourceEnd     int
	padding       bool
	paddingColumn int
}

type sourceView struct {
	doc    *sourceDocument
	text   string
	parts  []sourcePart
	base   int
	anchor int
}

func (d *sourceDocument) view() sourceView {
	return sourceView{doc: d, text: d.text, parts: []sourcePart{{end: len(d.text), sourceEnd: len(d.text)}}}
}

func (v sourceView) slice(start, end int) sourceView {
	return sourceView{doc: v.doc, text: v.text[start:end], parts: v.parts, base: v.base + start, anchor: v.anchor}
}

func sameSourceView(a, b sourceView) bool {
	if a.doc != b.doc || a.base != b.base || len(a.text) != len(b.text) || a.anchor != b.anchor || len(a.parts) != len(b.parts) {
		return false
	}
	return len(a.parts) == 0 || &a.parts[0] == &b.parts[0]
}

func (v sourceView) position(index int, ending bool) Position {
	if v.doc == nil {
		return Position{Line: 1, Column: 1}
	}
	absolute := v.base + index
	if len(v.parts) == 0 {
		return v.doc.position(v.anchor)
	}
	i := sort.Search(len(v.parts), func(i int) bool {
		if ending && index != 0 {
			return v.parts[i].end >= absolute
		}
		return v.parts[i].end > absolute
	})
	if i == len(v.parts) {
		i--
	}
	part := &v.parts[i]
	if part.padding {
		if ending && absolute >= part.end {
			return v.doc.position(part.sourceEnd)
		}
		return v.doc.position(part.sourceStart)
	}
	delta := absolute - part.start
	if delta < 0 {
		delta = 0
	}
	if delta > part.end-part.start {
		delta = part.end - part.start
	}
	return v.doc.position(part.sourceStart + delta)
}

func (v sourceView) paddingAt(index int) bool {
	absolute := v.base + index
	i := sort.Search(len(v.parts), func(i int) bool { return v.parts[i].end > absolute })
	return i < len(v.parts) && v.parts[i].start <= absolute && v.parts[i].padding
}

func (v sourceView) span() SourceRange {
	return SourceRange{Start: v.position(0, false), End: v.position(len(v.text), true)}
}

func (v sourceView) ranges() []SourceRange {
	var result []SourceRange
	start, end := v.base, v.base+len(v.text)
	for _, part := range v.parts {
		a, b := max(start, part.start), min(end, part.end)
		if a >= b {
			continue
		}
		sourceStart, sourceEnd := part.sourceStart, part.sourceEnd
		if !part.padding {
			sourceStart += a - part.start
			sourceEnd = part.sourceStart + b - part.start
		}
		result = append(result, SourceRange{Start: v.doc.position(sourceStart), End: v.doc.position(sourceEnd)})
	}
	if len(result) == 0 {
		result = append(result, v.span())
	}
	return result
}

func joinViews(views []sourceView) sourceView {
	if len(views) == 0 {
		return sourceView{}
	}
	if len(views) == 1 {
		return views[0]
	}
	var size int
	var doc *sourceDocument
	var first, last int
	contiguous := true
	seen := false
	for _, view := range views {
		if doc == nil {
			doc = view.doc
		}
		size += len(view.text)
		if len(view.text) == 0 {
			continue
		}
		start, end := view.base, view.base+len(view.text)
		for _, part := range view.parts {
			a, b := max(start, part.start), min(end, part.end)
			if a >= b {
				continue
			}
			sourceStart := part.sourceStart + a - part.start
			sourceEnd := part.sourceStart + b - part.start
			if part.padding || view.doc != doc || seen && sourceStart != last {
				contiguous = false
			}
			if !seen {
				first = sourceStart
				seen = true
			}
			last = sourceEnd
		}
	}
	if contiguous && seen {
		return doc.view().slice(first, last)
	}
	var builder strings.Builder
	builder.Grow(size)
	result := sourceView{doc: doc}
	for _, view := range views {
		base := builder.Len()
		builder.WriteString(view.text)
		start, end := view.base, view.base+len(view.text)
		for _, part := range view.parts {
			a, b := max(start, part.start), min(end, part.end)
			if a >= b {
				continue
			}
			mapped := sourcePart{start: base + a - start, end: base + b - start, sourceStart: part.sourceStart, sourceEnd: part.sourceEnd, padding: part.padding}
			if !part.padding {
				mapped.sourceStart += a - part.start
				mapped.sourceEnd = part.sourceStart + b - part.start
			} else {
				mapped.paddingColumn = part.paddingColumn + a - part.start
			}
			result.parts = append(result.parts, mapped)
		}
	}
	result.text = builder.String()
	if !seen {
		result.anchor = views[0].base
	}
	return result
}

func paddingView(count int, origin sourceView) sourceView {
	if count == 0 {
		return origin.slice(0, 0)
	}
	ranges := origin.parts
	start, end := origin.base, origin.base+len(origin.text)
	var sourceStart, sourceEnd int
	for _, part := range ranges {
		if part.end > start && part.start < end {
			sourceStart = part.sourceStart + max(start, part.start) - part.start
			sourceEnd = part.sourceStart + min(end, part.end) - part.start
			break
		}
	}
	return sourceView{doc: origin.doc, text: strings.Repeat(" ", count), parts: []sourcePart{{end: count, sourceStart: sourceStart, sourceEnd: sourceEnd, padding: true, paddingColumn: sourceVisualColumn(origin, len(origin.text)) - count}}}
}

func sourceVisualColumn(view sourceView, index int) int {
	if view.doc == nil {
		return 1
	}
	absolute := view.base + index
	i := sort.Search(len(view.parts), func(i int) bool { return view.parts[i].end > absolute })
	if i == len(view.parts) && i > 0 {
		i--
	}
	mapped := view.anchor
	if i < len(view.parts) {
		part := view.parts[i]
		delta := max(0, min(absolute-part.start, part.end-part.start))
		if part.padding {
			return part.paddingColumn + delta
		}
		mapped = part.sourceStart + delta
	}
	start := mapped
	for start > 0 && view.doc.text[start-1] != '\r' && view.doc.text[start-1] != '\n' {
		start--
	}
	column := 1
	for _, code := range view.doc.text[start:mapped] {
		if code == '\t' {
			column += 4 - (column-1)%4
		} else if code > 0xffff {
			column += 2
		} else {
			column++
		}
	}
	return column
}

type sourceLine struct {
	body   sourceView
	ending sourceView
}

func (line sourceLine) full() sourceView { return joinViews([]sourceView{line.body, line.ending}) }

func sourceLines(view sourceView) []sourceLine {
	var lines []sourceLine
	start := 0
	for i := 0; i < len(view.text); {
		if view.text[i] != '\n' && view.text[i] != '\r' {
			i++
			continue
		}
		end := i + 1
		if view.text[i] == '\r' && end < len(view.text) && view.text[end] == '\n' {
			end++
		}
		lines = append(lines, sourceLine{body: view.slice(start, i), ending: view.slice(i, end)})
		start, i = end, end
	}
	if start < len(view.text) {
		lines = append(lines, sourceLine{body: view.slice(start, len(view.text)), ending: view.slice(len(view.text), len(view.text))})
	}
	return lines
}

func linesView(lines []sourceLine) sourceView {
	views := make([]sourceView, 0, len(lines)*2)
	for _, line := range lines {
		views = append(views, line.body, line.ending)
	}
	return joinViews(views)
}
