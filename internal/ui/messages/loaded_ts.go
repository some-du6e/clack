package messages

// LoadedTS is the ts of every message the list holds, oldest first, as of
// its last layout (its snapshot): what the list draws, for the tests.
func (v *ChannelView) LoadedTS() []string {
	out := make([]string, 0, len(v.s.snap.msgs))
	for i := range v.s.snap.msgs {
		out = append(out, v.s.snap.msgs[i].m.TS)
	}
	return out
}
