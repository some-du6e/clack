package popups

// The GIF tile test's reads of the grid (gif_tile_click_test.go), apart
// so a proof run on the old grid can stand in its own.

// gifHovered is whether item k's tile reads hovered.
func gifHovered(g *gifPanel, k int) bool { return g.tile(g.items[k]).Hovered() }

// gifLanded is what the grid does when results land (gifSearch's answer).
func gifLanded(g *gifPanel) { g.pruneTiles() }
