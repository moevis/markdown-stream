// Package markdown incrementally parses a small Markdown dialect into typed
// rune segments. It does not build an AST or render HTML, and it is not a full
// CommonMark or GFM implementation. The dialect includes h1-h7 headings,
// emphasis, simple links and images, pipe tables, code, equations, and references.
//
// Create a Parser with NewParser, run Start with MarkdownDoc in a goroutine,
// and produce input with Put followed by one Close. Consume Read concurrently:
// bounded input and output buffers apply backpressure, so feeding all input
// before reading output can block. Close ends input; it does not cancel parsing
// or wait for the output to be drained. A custom root may end before input does.
//
// A Parser is single-use, with one ordered input producer and one output consumer.
// Cursor methods and OutputRune belong to the parsing goroutine. Elements and
// their slices must remain unchanged while a parser is using them.
//
// Input blocks may split UTF-8 at any byte. Decoding is equivalent to iterating
// over the concatenated input as Go runes, including RuneError for invalid UTF-8.
// With MarkdownDoc, segments preserve decoded characters, markup, and escapes.
// U+0000 is ordinary input; cursor methods use EOF, not zero, to mark the end.
//
// Already emitted opening markers are not retracted for unclosed streaming
// elements. The parser retains decoded input for rollback while running and
// releases its rune buffer when the root finishes; memory is not constant-space.
package markdown
