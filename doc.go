// Package markdown parses CommonMark with a native declarative grammar,
// recording concrete syntax tokens, source ranges, and enter/exit events alongside
// an editable semantic AST. GFM and math are explicitly registered extensions.
//
// Parse, ParseBytes, and ParseReader synchronously confirm complete documents.
// HTML parses and compiles a string. RenderHTML compiles a Document's recorded
// concrete event stream and retains the original parsed concrete semantics;
// RenderAST renders the current semantic Node tree, including edits.
// Document.Root is separate from the concrete tokens and events. Tokens retain
// logical Source, physical RawSource, and original SourceRanges; Range is
// end-exclusive. Positions include original byte offsets and preprocessed UTF-16
// coordinates, with one-based lines and columns and zero-based offsets.
//
// NewParser collects ordered input through Put or the io.Writer method Write.
// Close ends input; CloseWithError with a non-nil error aborts confirmation and
// propagates the producer's error. Chunks use the same synchronous grammar as
// Parse. Confirmed documents and events are published only after input is closed
// and parsing succeeds, not while input is being produced. Close does not wait
// for parsing or event consumption. Document waits without consuming the event
// cursor. Read returns (Event, bool, error); ReadNonBlocking returns
// (Event, ReadStatus, error), distinguishing ReadPending, ReadEvent, and ReadDone.
// Each Parser is single-use. Writes and close are serialized; multiple event
// readers share one cursor. Writes or repeated close after closure return ErrClosed.
//
// Options registers syntax and HTML independently: pair GFM() with GFMHTML(),
// and Math() with MathHTML(). GFM includes tables, strikethrough, task lists,
// literal autolinks, and footnotes; GFMHTML also supplies forbidden-tag filtering.
// MathHTML preserves TeX as HTML-escaped text with math classes, without KaTeX
// evaluation or typesetting. Raw HTML and dangerous protocols require explicit
// opt-in. Default CommonMark and GFM do not recognize doc/web/url business
// references. ExampleSyntaxExtension explicitly registers <doc_intro> syntax
// and HTML to produce a /docs/intro link; without registration it is escaped text.
//
// Shared matchers, named constructs, and phase assembly are declared together
// in grammar.go; synchronous and chunked parsing use that same entry point.
// Rule combinators are the core grammar and extension entry point. Register a
// Construct with Rule in a SyntaxExtension dispatch table. Sequence and Or
// transact cursor and token effects. Repeat uses max=0 for unbounded repetition,
// panics on invalid bounds, and reports zero-width success as a parsing error.
// Optional commits successful matches, including zero-width assertions, and
// restores absent branches. Lookahead and Not do not commit cursor or token
// effects. Until consumes up to but not including its terminator, can cross line
// endings, and rolls back when no terminator matches. Capture records concrete
// tokens, then runs TokenAction callbacks. Their errors stop parsing rather
// than falling through Or. Content returns a TokenAction that delegates the
// captured body to the native grammar.
//
// Complex constructs may use Rule.Tokenizer or State and Effects directly.
// Effects owns cursor movement, token boundaries, transactional Attempt, and
// Check/Interrupt lookahead. Construct Continuation, Exit, and resolver hooks
// support container continuation, exit, and event resolution. The core retains
// state and effects for container continuation, delimiter matching, references,
// and indentation. Transactions do not roll back arbitrary callback side effects.
// ResolveTo replaces the current scope tape, including the committed prefix.
// In document continuations that prefix includes committed child flow without
// duplicating the current container. Failed attempts restore parser-managed
// events, token topology, semantic nodes, and shallow metadata changes.
// Block classification and interruption checks neither execute nor register
// resolvers. Non-concrete Flow checks and committed matches use the same
// Document-container boundary. ContentInitial may consume part of a physical
// line; its remaining text and trivia retain their source ranges.
// Metadata Data maps are only shallow copied: nested values and external callback
// state are not deeply isolated. Extension values and their maps must remain
// unchanged during parsing or compilation.
package markdown
