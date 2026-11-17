package slick

// What menu_test.go reaches of Slick #473's API in Go, in one place (a
// branch proving the tests fail on the code before it stands these in).

func panelOf(p PluginInfo) int { return p.Panel }

func renderPanel(s *Slick, root uint64, id string) { s.RenderPanel(root, id) }
