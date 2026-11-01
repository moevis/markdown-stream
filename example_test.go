package markdown_test

import (
	"fmt"

	markdown "github.com/moevis/markdown-stream"
)

func ExampleNewParser() {
	p := markdown.NewParser()
	go p.Start(markdown.MarkdownDoc)
	go func() {
		p.Put("**Hi**")
		p.Close()
	}()
	for {
		seg, ok := p.Read()
		if !ok {
			break
		}
		fmt.Printf("%s: %c\n", seg.Type, seg.Rune)
	}
	// Output:
	// strong.prefix: *
	// strong.prefix: *
	// strong.text: H
	// strong.text: i
	// strong.suffix: *
	// strong.suffix: *
}
