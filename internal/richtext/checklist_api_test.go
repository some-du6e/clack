package richtext

// checkOf is a List block's check list state (red branches stub it).
func checkOf(b Block) (check bool, checked []bool) { return b.Check, b.Checked }
