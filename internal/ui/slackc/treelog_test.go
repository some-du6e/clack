package slackc

import "testing"

// The tree log carries the shape only: no text, no prop values.
func TestTreeShapeHasNoContent(t *testing.T) {
	nodes, err := ParseTree([]byte(`[{"t":"span","k":1,"p":{"className":"taut-reactors","title":"secret title"},"c":[{"t":"#","v":"secret text"},{"t":"@","n":"Emoji","k":2,"p":{"emoji":"eyes","size":16}}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	got := TreeShape(nodes)
	if got != "span.taut-reactors{title}>(# @Emoji{emoji,size})" {
		t.Errorf("TreeShape = %q", got)
	}
}
