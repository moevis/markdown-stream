package core

// Resolver rewrites inside a speculative Attempt must be reversible even when
// they replace slices or change the topology of already closed prefix tokens.
// Ordinary rules need no graph copy: Effects uses the small cursor snapshot.
type resolverTokenState struct {
	token    *Token
	value    Token
	node     *Node
	semantic Node
}

type resolverUndo struct {
	events    []Event
	eventTape []Event
	roots     []*Token
	rootTape  []*Token
	tokens    []resolverTokenState
}

func copyMetadata(original map[string]any) map[string]any {
	if original == nil {
		return nil
	}
	copy := make(map[string]any, len(original))
	for key, value := range original {
		copy[key] = value
	}
	return copy
}

func (c *TokenizeContext) saveResolverUndo() {
	if c.running == 0 {
		return
	}
	undo := resolverUndo{
		events: append([]Event(nil), c.Events...), eventTape: c.Events,
		roots: append([]*Token(nil), c.roots...), rootTape: c.roots,
		tokens: make([]resolverTokenState, 0, len(c.Events)/2),
	}
	for _, event := range c.Events {
		if event.Kind != Enter {
			continue
		}
		token := event.Token
		state := resolverTokenState{token: token, value: *token}
		state.value.Children = append([]*Token(nil), token.Children...)
		state.value.Data = copyMetadata(token.Data)
		if node := token.node; node != nil && node.token == token {
			state.node, state.semantic = node, *node
			state.semantic.Children = append([]*Node(nil), node.Children...)
			state.semantic.Alignments = append([]Alignment(nil), node.Alignments...)
			state.semantic.Data = copyMetadata(node.Data)
		}
		undo.tokens = append(undo.tokens, state)
	}
	c.undo = append(c.undo, undo)
}

func (c *TokenizeContext) restoreResolvers(from int) {
	for index := len(c.undo) - 1; index >= from; index-- {
		undo := &c.undo[index]
		copy(undo.eventTape, undo.events)
		copy(undo.rootTape, undo.roots)
		for _, state := range undo.tokens {
			*state.token = state.value
			if state.node != nil {
				*state.node = state.semantic
			}
		}
		*undo = resolverUndo{}
	}
	c.undo = c.undo[:from]
}

func (c *TokenizeContext) releaseResolverUndo() {
	if c.running == 0 {
		clear(c.undo)
		c.undo = c.undo[:0]
	}
}
