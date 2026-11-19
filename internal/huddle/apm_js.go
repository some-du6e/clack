//go:build js

package huddle

// The browser demo has no huddles (rtc: no connection opens), so no
// microphone audio is ever processed, and the RNNoise model apm.go embeds
// stays out of its build. The settings are kept so Preferences reads back
// what it set.
type micProcessor struct{ agc, ns bool }

func newMicProcessor(agc, ns bool) *micProcessor { return &micProcessor{agc: agc, ns: ns} }

func (p *micProcessor) set(agc, ns bool) { p.agc, p.ns = agc, ns }

func (p *micProcessor) enabled() (agc, ns bool) { return p.agc, p.ns }

func (p *micProcessor) process([]int16) {}
