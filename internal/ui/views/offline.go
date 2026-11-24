package views

// viewsClientOnline is isClientOnline (client-boot Vdrr Pb, core
// ClientOnline): the views' offline states read it. A var for tests.
var viewsClientOnline = func(h Host) bool {
	c := h.Core()
	return c == nil || c.ClientOnline()
}
