package icons

// Code channel glyphs (internal/ui/codechannel.go): the sidebar's agent
// status dots (getAgentStatusDisplay), the context bar's working chip and
// link icons. The ones Slack's SvgIcon set has (status-member, pr-open,
// pr-closed, branch, stop-filled, archive, terminal, hierarchy, life-ring)
// are redrawn on its centrelines in redrawn.go under the same names.
func init() {
	// suspended / PR blocked: a ring with an exclamation mark, on the
	// 8.25 ring Slack's info and help use (Slack has no such glyph; see
	// clackOwn)
	reg("needs-attention", s(circle(10, 10, 8.25)), s("M10 5.75 V10.75"), disc(10, 13.75, 1))
}
