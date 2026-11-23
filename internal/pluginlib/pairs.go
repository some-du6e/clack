package pluginlib

// Plugins Taut and Slick both ship under names of their own: the same
// plugin, written for each client. The Library lists each once, like a
// plugin both systems ship under one name (Clear URLs), and Install
// installs the plugin bench's pick. A plugin with one name in both pairs
// by it, and the bench pairs its own by id (bench.json), so this lists
// only the rest, each read from both sources at the Library's commits
// (taut 6fe5123, slick b582605) and paired only where both do the same
// thing with the same services. docs/PLUGINS.md's pair table is the
// record, with the related plugins that aren't paired and why
// (TestPairsMatchDocs keeps the two in step).
var pairs = []Pair{
	{Taut: "NoTrack", Slick: "NoTrack"},                       // No Tracking / No Track
	{Taut: "AnonymizeFilenames", Slick: "AnonymiseFileNames"}, // Anonymize Filenames / Anonymise File Names
	{Taut: "PrivateChannel", Slick: "PrivateChannelMapper"},   // Private Channel / Private Channel Mapper
	{Taut: "RealMarkdown", Slick: "NotShitMarkdown"},          // Real Markdown / Not Shit Markdown
	{Taut: "IdvStatus", Slick: "HcaStatus"},                   // IDV Status / HCA Status
	{Taut: "BringBackSlackbot", Slick: "CustomSlackbot"},      // Bring Back Slackbot / Custom Slackbot
}

// Pair is one plugin's id in each system.
type Pair struct{ Taut, Slick string }

// Pairs is the plugins both systems ship under names of their own.
func Pairs() []Pair { return append([]Pair(nil), pairs...) }
