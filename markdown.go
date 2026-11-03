package markdown

import (
	"io"

	core "github.com/moevis/markdown-stream/internal/core"
)

type (
	Alignment       = core.Alignment
	Code            = core.Code
	CompileContext  = core.CompileContext
	Construct       = core.Construct
	ContentType     = core.ContentType
	Definition      = core.Definition
	Document        = core.Document
	Effects         = core.Effects
	Encoding        = core.Encoding
	Event           = core.Event
	EventKind       = core.EventKind
	GFMHTMLOptions  = core.GFMHTMLOptions
	GFMOptions      = core.GFMOptions
	HTMLExtension   = core.HTMLExtension
	HTMLHandler     = core.HTMLHandler
	Node            = core.Node
	NodeKind        = core.NodeKind
	Options         = core.Options
	Parser          = core.Parser
	Phase           = core.Phase
	Position        = core.Position
	ReadStatus      = core.ReadStatus
	Resolver        = core.Resolver
	Rule            = core.Rule
	SourceRange     = core.SourceRange
	State           = core.State
	SyntaxExtension = core.SyntaxExtension
	Token           = core.Token
	TokenAction     = core.TokenAction
	TokenStream     = core.TokenStream
	TokenType       = core.TokenType
	TokenizeContext = core.TokenizeContext
	Tokenizer       = core.Tokenizer
)

const (
	AlignCenter            = core.AlignCenter
	AlignLeft              = core.AlignLeft
	AlignNone              = core.AlignNone
	AlignRight             = core.AlignRight
	CarriageReturn         = core.CarriageReturn
	CarriageReturnLineFeed = core.CarriageReturnLineFeed
	ContentDocument        = core.ContentDocument
	ContentFlow            = core.ContentFlow
	ContentNone            = core.ContentNone
	ContentString          = core.ContentString
	ContentText            = core.ContentText
	EOF                    = core.EOF
	Enter                  = core.Enter
	Exit                   = core.Exit
	Fallback               = core.Fallback
	HorizontalTab          = core.HorizontalTab
	KindBlockQuote         = core.KindBlockQuote
	KindCodeBlock          = core.KindCodeBlock
	KindCodeSpan           = core.KindCodeSpan
	KindCustom             = core.KindCustom
	KindDefinition         = core.KindDefinition
	KindDocument           = core.KindDocument
	KindEmphasis           = core.KindEmphasis
	KindFootnoteDefinition = core.KindFootnoteDefinition
	KindFootnoteReference  = core.KindFootnoteReference
	KindHTMLBlock          = core.KindHTMLBlock
	KindHTMLInline         = core.KindHTMLInline
	KindHardBreak          = core.KindHardBreak
	KindHeading            = core.KindHeading
	KindImage              = core.KindImage
	KindLink               = core.KindLink
	KindList               = core.KindList
	KindListItem           = core.KindListItem
	KindMathBlock          = core.KindMathBlock
	KindMathInline         = core.KindMathInline
	KindParagraph          = core.KindParagraph
	KindSoftBreak          = core.KindSoftBreak
	KindStrikethrough      = core.KindStrikethrough
	KindStrong             = core.KindStrong
	KindTable              = core.KindTable
	KindTableCell          = core.KindTableCell
	KindTableRow           = core.KindTableRow
	KindText               = core.KindText
	KindThematicBreak      = core.KindThematicBreak
	Latin1                 = core.Latin1
	LineFeed               = core.LineFeed
	PhaseContentInitial    = core.PhaseContentInitial
	PhaseDocument          = core.PhaseDocument
	PhaseFlow              = core.PhaseFlow
	PhaseFlowInitial       = core.PhaseFlowInitial
	PhaseString            = core.PhaseString
	PhaseText              = core.PhaseText
	ReadDone               = core.ReadDone
	ReadEvent              = core.ReadEvent
	ReadPending            = core.ReadPending
	UTF16BE                = core.UTF16BE
	UTF16LE                = core.UTF16LE
	UTF8                   = core.UTF8
	VirtualSpace           = core.VirtualSpace
)

var (
	ErrClosed = core.ErrClosed
)

func Any() Rule { return core.Any() }

func Bind(first Rule, next func(*TokenizeContext) Rule) Rule { return core.Bind(first, next) }

func BindCapture(kind TokenType, rule Rule, next func(*TokenizeContext, *Token) Rule, actions ...TokenAction) Rule {
	return core.BindCapture(kind, rule, next, actions...)
}

func Capture(kind TokenType, rule Rule, actions ...TokenAction) Rule {
	return core.Capture(kind, rule, actions...)
}

func Content(kind ContentType) TokenAction { return core.Content(kind) }

func End() Rule { return core.End() }

func FoldedString(text string) Rule { return core.FoldedString(text) }

func GFM(options ...GFMOptions) SyntaxExtension { return core.GFM(options...) }

func GFMHTML(options ...GFMHTMLOptions) HTMLExtension { return core.GFMHTML(options...) }

func Guard(predicate func(*TokenizeContext) bool) Rule { return core.Guard(predicate) }

func HTML(source string, options ...Options) (string, error) { return core.HTML(source, options...) }

func IsLineEnding(code Code) bool { return core.IsLineEnding(code) }

func IsMarkdownSpace(code Code) bool { return core.IsMarkdownSpace(code) }

func Lookahead(rule Rule) Rule { return core.Lookahead(rule) }

func Many(rule Rule) Rule { return core.Many(rule) }

func Math() SyntaxExtension { return core.Math() }

func MathHTML() HTMLExtension { return core.MathHTML() }

func NewParser(options ...Options) *Parser { return core.NewParser(options...) }

func NewTokenStream(options ...Options) *TokenStream { return core.NewTokenStream(options...) }

func Not(rule Rule) Rule { return core.Not(rule) }

func Optional(rule Rule) Rule { return core.Optional(rule) }

func OptionalCapture(kind TokenType, rule Rule, actions ...TokenAction) Rule {
	return core.OptionalCapture(kind, rule, actions...)
}

func Or(rules ...Rule) Rule { return core.Or(rules...) }

func Parse(source string, options ...Options) (*Document, error) {
	return core.Parse(source, options...)
}

func ParseBytes(source []byte, options ...Options) (*Document, error) {
	return core.ParseBytes(source, options...)
}

func ParseReader(reader io.Reader, options ...Options) (*Document, error) {
	return core.ParseReader(reader, options...)
}

func RenderAST(writer io.Writer, root *Node, options ...Options) error {
	return core.RenderAST(writer, root, options...)
}

func RenderHTML(writer io.Writer, document *Document, options ...Options) error {
	return core.RenderHTML(writer, document, options...)
}

func Repeat(rule Rule, min int, max int) Rule { return core.Repeat(rule, min, max) }

func Runes(set string) Rule { return core.Runes(set) }

func Satisfy(predicate func(Code) bool) Rule { return core.Satisfy(predicate) }

func SepBy(item Rule, separator Rule, min int, max int) Rule {
	return core.SepBy(item, separator, min, max)
}

func Sequence(rules ...Rule) Rule { return core.Sequence(rules...) }

func Some(rule Rule) Rule { return core.Some(rule) }

func Span(kind TokenType, open, close string) Rule { return core.Span(kind, open, close) }

func String(text string) Rule { return core.String(text) }

func Times(rule Rule, count int) Rule { return core.Times(rule, count) }

func Until(terminator Rule) Rule { return core.Until(terminator) }
