package slick

// What menu_test.go reaches of Slick #473's API in Go, in one place (a
// branch proving the tests fail on the code before it stands these in).

func panelOf(p PluginInfo) bool { return p.Panel }

func renderPanel(s *Slick, root uint64, id string) { s.RenderPanel(root, id) }

// mainErrorOf is why a plugin's privileged half didn't start ("" when it
// did, or has none).
func mainErrorOf(p PluginInfo) string { return p.MainError }
