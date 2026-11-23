package taut

// historyFP on the red tree (over the mirror before channelHistory was
// watched): TestChannelHistoryFollowsLoads compares two of them, and the
// code before had none.
func (m *mirror) historyFP(key string) uint64 { return 0 }
