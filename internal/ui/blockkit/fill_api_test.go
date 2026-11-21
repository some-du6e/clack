package blockkit

import "github.com/some-du6e/clack/internal/ui/richview"

// viewFills is whether v lays out as a view's blocks (FillBlocks; red
// branches stub it: false).
func viewFills(v *richview.View) bool { return v.Options().FillBlocks }
