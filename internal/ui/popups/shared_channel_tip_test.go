package popups

import "testing"

// SharedChannelTooltipContent's sentences (boot-shared Un00), by how many
// external teams there are and whether the name was truncated.
func TestSharedChannelTipText(t *testing.T) {
	a, b, c := SharedTeam{Name: "Partner Co"}, SharedTeam{Name: "Freight Ltd"}, SharedTeam{Name: "Ink & Co"}
	for _, tc := range []struct {
		teams []SharedTeam
		count int
		trunc bool
		want  string
	}{
		{[]SharedTeam{a}, 1, false, "Partner Co is in this channel"},
		{[]SharedTeam{a}, 1, true, "Partner Co is in th-partner-co"},
		{[]SharedTeam{a, b}, 2, false, "Partner Co and Freight Ltd are in this channel"},
		{[]SharedTeam{a, b, c}, 3, false, "Partner Co, Freight Ltd, and Ink & Co are in this channel"},
		{[]SharedTeam{a, b, c}, 4, false, "Partner Co, Freight Ltd, Ink & Co, and 1 other external organization are in this channel"},
		{[]SharedTeam{a, b, c}, 7, true, "Partner Co, Freight Ltd, Ink & Co, and 4 other external organizations are in th-partner-co"},
		{[]SharedTeam{{}}, 1, false, "Unknown Workspace is in this channel"},
	} {
		if got := sharedChannelTipText(tc.teams, tc.count, "th-partner-co", tc.trunc); got != tc.want {
			t.Errorf("%d teams of %d (truncated %v): %q, want %q", len(tc.teams), tc.count, tc.trunc, got, tc.want)
		}
	}
}
