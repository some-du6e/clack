package fakeslack

// The timeline block (Slack's TimelineCondensed): an agent's tool calls
// as one line that opens into its points and their steps' sections. The
// running one draws its header still (Clack leaves out the shimmer), so
// every one of them idles. Synthetic.

func init() { RegisterGallery("17-timeline", galleryTimeline) }

func galleryTimeline(e GalleryEnv) []GalleryMessage {
	rt := func(s string) string {
		return `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}`
	}
	return []GalleryMessage{
		{Name: "timeline/used tools", Text: "Used 3 tools", Blocks: `[{"type":"timeline","block_id":"tl1","points":[
			{"id":"p1","status":"complete","text":"Searched messages","icon":"search","steps":[{"id":"sources","sections":[
				{"rich_text_title":` + rt("Looked through #general") + `,"sources":[{"type":"web","url":"https://example.com/release","title":"Release notes"},{"type":"message","channel_id":"` + GalleryChannelID + `","timestamp":"1700000000.000001"}]},
				{"rich_text_title":` + rt("Read the release notes") + `,"sources":[]}]}]},
			{"id":"p2","status":"complete","rich_text_title":` + rt("Checked the calendar") + `,"icon":"calendar"},
			{"id":"p3","status":"complete","text":"Summarized the thread","icon":"ai-sparkle","steps":[{"id":"s1","sections":[{"rich_text_title":` + rt("Wrote a summary") + `}]}]}]},
			{"type":"section","text":{"type":"mrkdwn","text":"Here's what changed in the release."}}]`},
		{Name: "timeline/one point", Text: "Looked something up", Blocks: `[{"type":"timeline","block_id":"tl2","points":[{"id":"p1","status":"complete","rich_text_title":` + rt("Looked up the on-call schedule") + `,"icon":"clock"}]}]`},
		{Name: "timeline/summary", Text: "Planned the trip", Blocks: `[{"type":"timeline","block_id":"tl3","summary":` + rt("Planned the offsite") + `,"points":[
			{"id":"p1","status":"complete","text":"Found flights","icon":"search"},{"id":"p2","status":"error","text":"Booked the hotel","icon":"error"}]}]`},
		{Name: "timeline/running", Text: "Working on it", Blocks: `[{"type":"timeline","block_id":"tl4","points":[
			{"id":"p1","status":"complete","text":"Read the doc","icon":"book"},{"id":"p2","status":"in_progress","text":"Drafting a reply","icon":"ai-sparkle"}]}]`},
		{Name: "timeline/task attribution", Text: "A task attribution timeline (nothing drawn)", Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"A task attribution timeline Slack draws only with its feature (nothing under this line):"}},{"type":"timeline","block_id":"task_attribution_1","points":[{"id":"p1","status":"complete","text":"Hidden"}]}]`},
	}
}
