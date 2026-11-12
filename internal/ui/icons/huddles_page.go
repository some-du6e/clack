package icons

// Slack SvgIcons the Huddles page draws (Slack's icon set, the public web
// client's, via slack-datamine-plus icons.json; TestIconsMatchSlack holds
// them to it).
func init() {
	// ai-summary: SlackAiIcon {type: "summary"} (mHNz), the history row's
	// AI notes button (ZvF8 useHuddleArtifactActions). Slack's glyph is a
	// four-point sparkle band (its hollow centre evenodd) at (0.5-10.5,
	// 2.25-12.25), three 1.5-wide round-ended bars (12-17.25 at y 4.5 and
	// 10, 10-17.25 at y 15.5) and a dot (r 1 at 5.5, 15.5). The bars are
	// drawn on their centrelines; the sparkle and the dot as flw(1, …)
	// fills of the shapes eroded 0.5 (shapely; IoU 0.993 with Slack's),
	// so their edges carry Chrome's ink in both modes.
	reg("ai-summary",
		flw(1, "M1.13 7.25 L3.19 6.22 L3.54 5.98 L3.92 5.64 L4.2 5.32 L4.45 4.96 L5.5 2.88 L6.5 4.88 L6.77 5.29 L7.09 5.66 L7.46 5.98 L7.87 6.25 L9.86 7.25 L7.91 8.23 L7.54 8.47 L7.31 8.65 L7.09 8.84 L6.9 9.06 L6.72 9.29 L6.48 9.66 L5.5 11.6 L4.52 9.66 L4.28 9.29 L4.1 9.06 L3.91 8.84 L3.69 8.65 L3.46 8.47 L3.09 8.23 Z M4.98 5.04 L4.75 5.41 L4.55 5.68 L4.34 5.93 L4.01 6.24 L3.75 6.44 L3.47 6.62 L2.29 7.27 L3.19 7.72 L3.48 7.88 L3.93 8.2 L4.26 8.49 L4.56 8.82 L4.82 9.18 L4.98 9.47 L5.49 10.5 L6.02 9.46 L6.19 9.18 L6.45 8.82 L6.74 8.49 L7.07 8.19 L7.43 7.93 L7.72 7.77 L8.71 7.25 L7.71 6.73 L7.43 6.56 L7.07 6.3 L6.74 6.01 L6.44 5.68 L6.18 5.32 L6.02 5.03 L5.49 4.02 L5.05 4.91 L4.98 5.01 Z"),
		s("M12 4.5 H17.25"),
		s("M12 10 H17.25"),
		s("M10 15.5 H17.25"),
		flw(1, "M6 15.5 A0.5 0.5 0 1 1 5 15.5 A0.5 0.5 0 1 1 6 15.5 Z"))
}
