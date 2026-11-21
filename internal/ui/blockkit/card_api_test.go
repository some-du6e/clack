package blockkit

import "github.com/some-du6e/clack/internal/richtext"

// cardButtons is a card block's buttons, its left group then its right
// (red branches stub it: nil).
func cardButtons(b richtext.Block) []richtext.Button {
	if b.Kind != richtext.CardBlock || b.Card == nil {
		return nil
	}
	return append(append([]richtext.Button(nil), b.Card.Left...), b.Card.Right...)
}
