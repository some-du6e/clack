package icons

// Glyphs of Slack's icon font ("Slack v2", slack-icons-v2-*.woff2 on the
// CDN, the c-icon--* classes), which a few places draw instead of an
// SvgIcon. Each is the glyph's outline as a fill, in the set's 20-unit
// view: the font's 2000-unit em at the 20px c-icon:before font size, the
// baseline at its 1700 ascent (x/100, (1700-y)/100). Slack's fonts are not
// in the repo.
func init() {
	// U+E125 icon_hash_small (c-icon--hash-small): the # before the channel
	// name in the composer's placeholder (MessagePaneInputPlaceholder). Its
	// strokes are one unit wide, the weight of Lato's own at 15px.
	reg("hash-small", f("M12.75 8 13.23 5.59C13.24 5.55 13.24 5.52 13.24 5.48C13.24 5.21 13.03 5 12.72 5C12.48 5 12.3 5.17 12.25 5.43L11.73 8H9.26L9.74 5.59C9.75 5.55 9.75 5.52 9.75 5.48C9.75 5.21 9.54 5 9.23 5C8.99 5 8.81 5.17 8.76 5.43L8.24 8H6.51C6.21 8 6 8.21 6 8.5C6 8.79 6.21 9 6.51 9H8.04L7.44 12H5.71C5.41 12 5.2 12.21 5.2 12.5C5.2 12.79 5.41 13 5.71 13H7.24L6.76 15.41C6.75 15.45 6.75 15.48 6.75 15.52C6.75 15.79 6.96 16 7.27 16C7.51 16 7.69 15.83 7.74 15.57L8.26 13H10.73L10.25 15.41C10.24 15.45 10.24 15.48 10.24 15.52C10.24 15.79 10.45 16 10.76 16C11 16 11.18 15.83 11.23 15.57L11.75 13H13.48C13.78 13 13.99 12.79 13.99 12.5C13.99 12.21 13.78 12 13.48 12H11.95L12.55 9H14.29C14.59 9 14.8 8.79 14.8 8.5C14.8 8.21 14.59 8 14.29 8ZM8.46 12 9.06 9H11.53L10.93 12Z"))
	// U+E294 icon_times_small (c-icon--times-small): AttachmentDeleteButton's
	// rail-style remove button left of classic and Block Kit unfurls.
	reg("times-small", f("M7.25 13.5C7.44 13.5 7.64 13.42 7.78 13.28L10 11.06L12.22 13.28C12.37 13.43 12.56 13.5 12.75 13.5C13.13 13.5 13.5 13.19 13.5 12.75C13.5 12.56 13.43 12.37 13.28 12.22L11.06 10L13.28 7.78C13.43 7.63 13.5 7.44 13.5 7.25C13.5 6.83 13.17 6.5 12.75 6.5C12.56 6.5 12.37 6.57 12.22 6.72L10 8.94L7.78 6.72C7.63 6.57 7.44 6.5 7.25 6.5C6.83 6.5 6.5 6.84 6.5 7.25C6.5 7.44 6.57 7.63 6.72 7.78L8.94 10L6.72 12.22C6.58 12.36 6.5 12.56 6.5 12.75C6.5 13.16 6.84 13.5 7.25 13.5Z"))

	// The Activity rows' Slack Connect Ignore flow (rPCk IgnoreForkedModal,
	// IgnoreForkedModalButton: the y8g2 font icons times-circle-medium,
	// disable and chevron-medium-right).
	// U+E229 icon_times_circle_medium
	reg("times-circle-medium", f("M10 18C14.42 18 18 14.42 18 10C18 5.58 14.42 2 10 2C5.58 2 2 5.58 2 10C2 14.42 5.58 18 10 18ZM10 3.5C13.59 3.5 16.5 6.41 16.5 10C16.5 13.59 13.59 16.5 10 16.5C6.41 16.5 3.5 13.59 3.5 10C3.5 6.41 6.41 3.5 10 3.5ZM13.01 7C12.72 6.71 12.24 6.71 11.95 7L10 8.94L8.06 7C7.77 6.71 7.29 6.71 7 7C6.71 7.29 6.71 7.77 7 8.06L8.94 10L7 11.95C6.71 12.24 6.71 12.72 7 13.01C7.29 13.3 7.77 13.3 8.06 13.01L10 11.06L11.95 13.01C12.24 13.3 12.72 13.3 13.01 13.01C13.3 12.72 13.3 12.24 13.01 11.95L11.06 10L13.01 8.06C13.3 7.77 13.3 7.29 13.01 7Z"))
	// U+E149 icon_disable
	reg("disable-font", f("M10 2C5.58 2 2 5.58 2 10C2 14.42 5.58 18 10 18C14.42 18 18 14.42 18 10C18 5.58 14.42 2 10 2ZM5.96 4.9C7.07 4.03 8.48 3.5 10 3.5C13.59 3.5 16.5 6.41 16.5 10C16.5 11.52 15.97 12.93 15.1 14.04ZM3.5 10C3.5 8.48 4.03 7.07 4.9 5.96L14.04 15.1C12.93 15.97 11.52 16.5 10 16.5C6.41 16.5 3.5 13.59 3.5 10Z"))
	// U+E254 icon_chevron_medium_right
	reg("chevron-medium-right", f("M8.53 4.72C8.39 4.58 8.19 4.5 8 4.5C7.81 4.5 7.62 4.58 7.47 4.72C7.32 4.86 7.25 5.06 7.25 5.25C7.25 5.44 7.33 5.64 7.47 5.78L11.69 10L7.47 14.22C7.32 14.37 7.25 14.56 7.25 14.75C7.25 14.94 7.33 15.14 7.47 15.28C7.62 15.43 7.81 15.5 8 15.5C8.19 15.5 8.39 15.43 8.53 15.28L13.25 10.56L13.28 10.53C13.42 10.38 13.5 10.19 13.5 10C13.5 9.81 13.43 9.62 13.28 9.47Z"))
	// Slack's cloud-offline SvgIcon (InlineOfflineIndicator: Activity's
	// "No connection. Go online to load more activity."), its own path:
	// three filled outlines, so nonzero and evenodd fill alike.
	reg("cloud-offline", f("m15.982 7.907 1.236-1.39c.212.647.31 1.34.273 2.05A5.25 5.25 0 0 1 13.75 17.5H7.455L8.788 16h4.962a3.75 3.75 0 0 0 2.469-6.573.75.75 0 0 1-.25-.655 4 4 0 0 0 .013-.865M1 13.75c0 1.12.49 2.125 1.27 2.812l.048-.057.947-1.065a2.25 2.25 0 0 1 .54-3.733.75.75 0 0 0 .383-.954c-.722-1.843.877-3.966 2.882-3.735a.75.75 0 0 0 .775-.448A4.25 4.25 0 0 1 11.75 4c.509 0 .997.094 1.45.264l1.047-1.179A5.6 5.6 0 0 0 11.75 2.5a5.75 5.75 0 0 0-5.051 3c-2.654.04-4.647 2.64-4.096 5.176A3.75 3.75 0 0 0 1 13.75M17.501 2.941a.75.75 0 0 0-1.06.058l-13 14.5a.75.75 0 0 0 1.118 1.002l13-14.5a.75.75 0 0 0-.058-1.06"))
}
