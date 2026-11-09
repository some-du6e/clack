package theme

// MilesDavisPresets are Slack's "Celebrate the music of Miles Davis" group
// (boot-shared cWw0 r, "Jazz themes"), shown first while the
// miles_davis_themes experiment is on (TU6L). All six are custom-hex
// themes.
var MilesDavisPresets = []ThemePreset{
	preset(MilesDavisGroup, "miles-kind-of-blue", "Kind of Blue", 4, true, true, "ocean", "ocean", "ocean", "ocean", "#4C64A6", "#71A3D1", "#14ADFF", "#1D3F5E"),
	preset(MilesDavisGroup, "miles-funky-fresh", "Funky Fresh", 3, false, true, "grass", "aubergine", "aubergine", "grass", "#BEB0FF", "#71D18F", "#00FF50", "#CDC8E8"),
	preset(MilesDavisGroup, "miles-jazz-club", "Jazz Club", 5, true, true, "tomato", "gray", "gray", "tomato", "#9C0000", "#070C52", "#000261", "#026B5A"),
	preset(MilesDavisGroup, "miles-electric-fusion", "Electric Fusion", 3, false, true, "mojito", "aubergine", "aubergine", "mojito", "#DCF095", "#ADFFBC", "#C674F2", "#C674F2"),
	preset(MilesDavisGroup, "miles-brassy", "Brassy", 3, true, true, "sunflower", "horchata", "horchata", "sunflower", "#FAD961", "#F0E6C2", "#EDCE34", "#ACDFE3"),
	preset(MilesDavisGroup, "miles-sunglasses-inside", "Sunglasses Inside", 4, true, true, "aubergine", "indigo", "indigo", "aubergine", "#2B102E", "#D1B4E0", "#41B584", "#C494D6"),
}

// MilesDavisGroup is the group's legend.
const MilesDavisGroup = "Celebrate the music of Miles Davis"
