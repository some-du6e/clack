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
}
