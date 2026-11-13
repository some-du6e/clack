package core

// The huddle window's experiment checks (ui/huddleui/hand.go, Slack's
// pSxm selectors), registered apart from experimentReads' literal like
// experiment_reads_more.go's.
func init() {
	for name, groups := range map[string][]string{
		"huddles_music": on, // pSxm lV: HuddleStickerGrid offers DJ’ing (headphones)
	} {
		experimentReads[name] = groups
	}
}
