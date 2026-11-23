package jsrt

// withMedia gives the runtime a media host (TestMediaElementPlays). A red
// run proving the test fails on code without one swaps this file for a
// stand-in.
func withMedia(o *Options, h MediaHost) { o.Media = h }
