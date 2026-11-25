package views

import "time"

// viewsClientOnline is isClientOnline (client-boot Vdrr Pb, core
// ClientOnline): the views' offline states read it. A var for tests.
var viewsClientOnline = func(h Host) bool {
	c := h.Core()
	return c == nil || c.ClientOnline()
}

// viewsTrouble is core's ConnectionTroubleSince (troubleConnecting, with
// when the period began; zero while there's none), which the views'
// DegradedBanners read. A var for tests.
var viewsTrouble = func(h Host) time.Time {
	if c := h.Core(); c != nil {
		return c.ConnectionTroubleSince()
	}
	return time.Time{}
}
