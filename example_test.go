package markdown_test

import (
	"fmt"
	"strings"

	markdown "github.com/moevis/markdown-stream"
)

func ExampleNewParser() {
	parser := markdown.NewParser()
	if err := parser.Put("**Hi"); err != nil {
		panic(err)
	}
	if err := parser.Put("**"); err != nil {
		panic(err)
	}
	if err := parser.Close(); err != nil {
		panic(err)
	}
	document, err := parser.Document()
	if err != nil {
		panic(err)
	}
	var html strings.Builder
	if err := markdown.RenderHTML(&html, document); err != nil {
		panic(err)
	}
	fmt.Println(html.String())
	// Output: <p><strong>Hi</strong></p>
}

// A reference syntax exists only when explicitly registered. The same
// combinators used by the native grammar record each source-derived token.
func ExampleSyntaxExtension() {
	identifier := markdown.Capture("docIdentifier", markdown.Repeat(
		markdown.Runes("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"), 1, 0,
	))
	reference := markdown.Capture("docReference", markdown.Sequence(
		markdown.String("<doc_"), identifier, markdown.String(">"),
	))
	syntax := markdown.SyntaxExtension{Text: map[markdown.Code][]markdown.Construct{
		'<': {{Name: "docReference", Rule: reference}},
	}}
	html := markdown.HTMLExtension{
		Enter: map[markdown.TokenType]markdown.HTMLHandler{
			"docReference": func(context *markdown.CompileContext, token *markdown.Token) error {
				id := token.Children[0].Source()
				if err := context.Tag(`<a href="/docs/` + context.Encode(id) + `">`); err != nil {
					return err
				}
				return context.Raw(context.Encode(id))
			},
		},
		Exit: map[markdown.TokenType]markdown.HTMLHandler{
			"docReference": func(context *markdown.CompileContext, _ *markdown.Token) error { return context.Tag("</a>") },
		},
	}
	ordinary, err := markdown.HTML("<doc_intro>")
	if err != nil {
		panic(err)
	}
	extended, err := markdown.HTML("<doc_intro>", markdown.Options{
		Extensions: []markdown.SyntaxExtension{syntax}, HTMLExtensions: []markdown.HTMLExtension{html},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(ordinary)
	fmt.Println(extended)
	// Output:
	// <p>&lt;doc_intro&gt;</p>
	// <p><a href="/docs/intro">intro</a></p>
}
