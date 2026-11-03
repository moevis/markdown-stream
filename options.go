package markdown

// Encoding selects decoding of the original input bytes.
type Encoding string

const (
	UTF8    Encoding = "utf-8"
	UTF16LE Encoding = "utf-16le"
	UTF16BE Encoding = "utf-16be"
	Latin1  Encoding = "latin1"
)

// Options configures syntax and HTML independently. Extension values and their
// maps must not be modified while a parse or compilation is in progress.
type Options struct {
	Encoding               Encoding
	AllowDangerousHTML     bool
	AllowDangerousProtocol bool
	DefaultLineEnding      string
	Extensions             []SyntaxExtension
	HTMLExtensions         []HTMLExtension
}

// Code includes Unicode scalar values and preprocessing sentinels.
type Code int32

const (
	VirtualSpace           Code = -2
	HorizontalTab          Code = -3
	LineFeed               Code = -4
	CarriageReturn         Code = -5
	CarriageReturnLineFeed Code = -6
	Fallback               Code = -7
)

func IsLineEnding(code Code) bool {
	return code == LineFeed || code == CarriageReturn || code == CarriageReturnLineFeed || code == '\n' || code == '\r'
}

func IsMarkdownSpace(code Code) bool {
	return code == ' ' || code == VirtualSpace || code == HorizontalTab || code == '\t'
}

// Phase identifies an extension dispatch layer.
type Phase uint8

const (
	PhaseDocument Phase = iota
	PhaseFlowInitial
	PhaseFlow
	PhaseContentInitial
	PhaseText
	PhaseString
)

// State consumes a preprocessed code and returns the next state. A nil state
// ends an attempt; ok/nok supplied to Tokenize distinguish its outcome.
type State func(Code) State

type Tokenizer func(*TokenizeContext, *Effects, State, State) State

// Resolver has stable identity, allowing a shared resolver to run once.
type Resolver struct {
	Resolve func([]Event, *TokenizeContext) ([]Event, error)
}

// Construct describes a transactional syntax attempt.
type Construct struct {
	Name         string
	Rule         Rule
	Tokenize     Tokenizer
	Previous     func(Code) bool
	AddAfter     bool
	Partial      bool
	Concrete     bool
	Continuation Tokenizer
	Exit         func(*TokenizeContext) error
	Resolve      *Resolver
	ResolveTo    *Resolver
	ResolveAll   *Resolver

	native bool
}

// SyntaxExtension extends the same grammar used by all input APIs.
type SyntaxExtension struct {
	Document         map[Code][]Construct
	FlowInitial      map[Code][]Construct
	Flow             map[Code][]Construct
	ContentInitial   map[Code][]Construct
	Text             map[Code][]Construct
	String           map[Code][]Construct
	InsideSpan       []*Resolver
	AttentionMarkers []Code
	Disable          []string

	features syntaxFeatures
}

type syntaxFeatures uint8

const (
	featureGFM syntaxFeatures = 1 << iota
	featureMath
)

// GFMOptions configures GFM syntax. Passing options selects whether single
// tildes may delimit strikethrough; GFM() enables both one and two tildes.
type GFMOptions struct {
	SingleTilde bool
}

// GFM enables tables, strikethrough, task lists, literal autolinks, and footnotes.
// Pair it with GFMHTML for GFM compilation and tag filtering.
func GFM(options ...GFMOptions) SyntaxExtension {
	single := true
	if len(options) != 0 {
		single = options[0].SingleTilde
	}
	return combineSyntax(gfmBlockSyntax(), gfmInlineSyntax(single))
}

// Math retains TeX expressions as explicit math nodes; it does not run KaTeX.
func Math() SyntaxExtension { return combineSyntax(mathBlockSyntax(), mathInlineSyntax()) }

// HTMLHandler runs at a concrete token boundary. Errors stop compilation.
type HTMLHandler func(*CompileContext, *Token) error

// HTMLExtension is independent of syntax registration. Later handlers replace
// earlier handlers for the same boundary; nil explicitly suppresses a handler.
type HTMLExtension struct {
	Enter         map[TokenType]HTMLHandler
	Exit          map[TokenType]HTMLHandler
	EnterDocument func(*CompileContext) error
	ExitDocument  func(*CompileContext) error

	gfm        bool
	filterTags bool
}

type parseState struct {
	doc            *Document
	options        Options
	gfm            bool
	math           bool
	disabled       map[string]bool
	insideSpan     []*Resolver
	attention      map[Code]bool
	contexts       []*TokenizeContext
	syntaxReady    bool
	constructs     [6]map[Code]constructBucket
	contextMap     map[syntaxKey]*TokenizeContext
	inlineOwner    *Token
	blockOwner     *Token
	blockParagraph bool
	checking       bool
}

func (s *parseState) enabled(name string) bool { return !s.disabled[name] }

func newParseState(doc *Document, options Options) *parseState {
	s := &parseState{doc: doc, options: options}
	for _, extension := range options.Extensions {
		s.gfm = s.gfm || extension.features&featureGFM != 0
		s.math = s.math || extension.features&featureMath != 0
		if len(extension.Disable) != 0 && s.disabled == nil {
			s.disabled = make(map[string]bool)
		}
		for _, name := range extension.Disable {
			s.disabled[name] = true
		}
		s.insideSpan = append(s.insideSpan, extension.InsideSpan...)
		if len(extension.AttentionMarkers) != 0 && s.attention == nil {
			s.attention = make(map[Code]bool)
		}
		for _, marker := range extension.AttentionMarkers {
			s.attention[marker] = true
		}
	}
	return s
}

func combineSyntax(extensions ...SyntaxExtension) SyntaxExtension {
	var combined SyntaxExtension
	var tables [6]map[Code][]Construct
	for _, extension := range extensions {
		for phase, table := range syntaxTables(extension) {
			if len(table) == 0 {
				continue
			}
			if tables[phase] == nil {
				tables[phase] = make(map[Code][]Construct)
			}
			for code, constructs := range table {
				tables[phase][code] = append(tables[phase][code], constructs...)
			}
		}
		combined.features |= extension.features
		combined.InsideSpan = append(combined.InsideSpan, extension.InsideSpan...)
		combined.AttentionMarkers = append(combined.AttentionMarkers, extension.AttentionMarkers...)
		combined.Disable = append(combined.Disable, extension.Disable...)
	}
	combined.Document, combined.FlowInitial, combined.Flow = tables[0], tables[1], tables[2]
	combined.ContentInitial, combined.Text, combined.String = tables[3], tables[4], tables[5]
	return combined
}
