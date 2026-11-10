package icons

// Slack SvgIcons the profile surfaces draw (icons.json of build
// a84662d2d135), on the centrelines of Slack's 1.5-unit outlines:
//
//   - user-hide: MemberProfileButtons' VIP button once the member is a VIP
//     (boot-shared Bp41 renderVipButton: icon user-hide, else user-add).
//     The head and the left shoulder of user-add with the head centred at
//     10, and the ring (r 4.75 outside, 3.25 inside) about 13.75,13.75
//     with its slash from top right to bottom left.
//   - shared-channel: ProfilePreviewMenu's "A contact from other
//     organizations" (boot-shared LEYP): the open chevron (vertex 2,10,
//     arms to 7.25,4.75 and 7.25,15.25 with their short returns) and the
//     diamond about 13,10 (half diagonal 5.25).
func init() {
	reg("user-hide",
		s("M13.6 7.4 C13.7 7.15 13.75 6.9 13.75 6.67 C13.75 4.1 11.9 2.75 10 2.75 C8.1 2.75 6.25 4.1 6.25 6.67 C6.25 8.25 7.05 9.65 8.25 10.5"),
		s("M7.9 11.6 C7.3 11.75 6.65 11.95 6.05 12.25 C4.65 12.9 3.4 13.95 2.85 15.35 C2.5 16.3 3.35 17.25 4.5 17.25 H8.65"),
		s(circle(13.75, 13.75, 4)+" M16.58 10.92 L10.92 16.58"))
	reg("shared-channel",
		s("M8 5.5 L7.25 4.75 L2 10 L7.25 15.25 L8 14.5"),
		s("M13 4.75 L18.25 10 L13 15.25 L7.75 10 Z"))
}
