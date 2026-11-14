package icons

// The emoji picker's Emoji | GIFs tab icons (boot-shared Y5Hl EmojiIcon and
// qS+L GifIcon, p-emoji_and_gif_picker_custom_icons): inline SVGs, not
// SvgIcons. The inactive Emoji tab's path is the emoji SvgIcon's outline,
// so it draws emoji; the active one is the disc (the skin tone, filled by
// the picker) under emoji-tab-active: the ring, and the face turned 18°
// (its eyes, and the grin 0.72 inside its edge). gif-tab is the inactive
// GIFs tab: the tile with its corner folded down (its 1.5 stroke), the
// eyes, the smile and the flap (a 1.73 wall, on its centreline);
// gif-tab-active is the ink over the active tab's #9ed6fa tile (its
// 1.5 stroke, the eye, the smile and the wink's 1-wide stroke).
// TestIconsMatchSlack holds them to those SVGs (inlineRef).
func init() {
	reg("emoji-tab-active", s(circle(10, 10, 8.25)), disc(8.58, 6.88, 1.5), disc(13.34, 8.4, 1.5),
		fl("M8.9 14.1 L9.56 14.26 L10.14 14.31 L10.67 14.28 L11.37 14.1 L11.96 13.79 L12.46 13.41 L12.94 12.85 L13.16 12.48 L13.23 12.27 L6.43 10.11 L6.37 10.33 L6.33 10.61 L6.37 11.31 L6.52 11.9 L6.79 12.49 L7.2 13.06 L7.57 13.4 L8.02 13.72 L8.29 13.86 Z"))
	reg("gif-tab", s("M4.5 1.63 H12.42 C13.32 1.63 14.18 2.03 14.75 2.73 L17.61 6.23 C18.05 6.77 18.29 7.44 18.29 8.13 V15.42 C18.29 17.07 16.95 18.42 15.29 18.42 H4.5 C2.84 18.42 1.5 17.07 1.5 15.42 V4.63 C1.5 2.97 2.84 1.63 4.5 1.63 Z"),
		disc(7.43, 8.54, 1.48), disc(12.36, 8.54, 1.48),
		s("M6.84 12.49 C7.32 13.97 8.62 14.71 9.92 14.71 C11.22 14.71 12.52 13.97 13.01 12.49"),
		sw(1.73, "M16.46 8.05 L16.71 8.02 L17.01 7.92 L17.26 7.74 L17.46 7.5 L17.59 7.22 L17.63 6.91 L17.6 6.61 L17.47 6.3 L14.95 2.93 L14.74 2.71 L14.44 2.52 L14.11 2.45 L13.76 2.48 L13.43 2.61 L13.16 2.82 L13.02 3 L12.92 3.21 L12.87 3.46 L12.86 6.97 L12.9 7.2 L13.02 7.48 L13.15 7.66 L13.37 7.85 L13.63 7.98 L13.85 8.04 Z"))
	reg("gif-tab-active", s(rrect(1.5, 1.5, 17, 17, 3)), disc(12.5, 8.5, 1.5),
		s("M6.9 12.5 C7.4 14 8.71 14.75 10.03 14.75 C11.34 14.75 12.66 14 13.15 12.5"),
		sw(1, "M7.04 7.54 L9.04 8.68 L7.04 9.54"))
}
