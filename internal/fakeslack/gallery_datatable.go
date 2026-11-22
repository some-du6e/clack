package fakeslack

import (
	"strconv"
	"strings"
)

// The data_table blocks (Slack's DataTableBlock), and a thread shaped like
// a bot's leaderboard: a bar chart of twelve people with preview_images
// (both modes' URLs and file ids), its reply a three-column data_table of
// 111 raw_text rows under a caption. Names and numbers are made up; only
// the shape is a real message's.

func init() { RegisterGallery("15-datatable", galleryDataTable) }

// GalleryDataTableBoard is the leaderboard table's block_id, and
// GalleryDataVizBoard its chart's.
const (
	GalleryDataTableBoard = "dtlb1"
	GalleryDataVizBoard   = "viz-board-leaderboard-001"
)

func galleryDataTable(e GalleryEnv) []GalleryMessage {
	board := []GalleryMessage{{Name: "datatable/leaderboard reply", Text: "Full points leaderboard",
		Blocks: `[{"type":"data_table","block_id":"` + GalleryDataTableBoard + `","caption":"Full points leaderboard","rows":[` + boardRows(110) + `]}]`}}
	return []GalleryMessage{
		{Name: "datatable/leaderboard", Text: "Points leaderboard", Replies: board, Blocks: strings.NewReplacer("{L}", e.Img(1200, 720, "board-light"), "{D}", e.Img(1200, 720, "board-dark")).Replace(`[{"type":"data_visualization","block_id":"` + GalleryDataVizBoard + `","title":"Points leaderboard",
			"chart":{"type":"bar","series":[{"name":"points","data":[` + boardPoints(12) + `]}],"axis_config":{"categories":[` + boardNames(12) + `],"x_label":"name","y_label":"points"}},
			"preview_images":{"preview_image_light_mode":"{L}","slack_file_id_light_mode":"F0BOARDLGT1","preview_image_dark_mode":"{D}","slack_file_id_dark_mode":"F0BOARDDRK1"}}]`)},
		{Name: "datatable/cells", Text: "Deploys", Blocks: `[{"type":"data_table","block_id":"dtcells","caption":"Deploys this week","rows":[
			[{"type":"raw_text","text":"Service"},{"type":"raw_text","text":"Owner"},{"type":"raw_text","text":"Builds"},{"type":"raw_text","text":"Status"},{"type":"raw_text","text":"Action"}],
			[{"type":"raw_text","text":"api"},{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"` + e.Users[0] + `"}]}]},{"type":"raw_number","value":42,"text":"42"},{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"emoji","name":"white_check_mark"},{"type":"text","text":" passing","style":{"bold":true}}]}]},{"type":"action_cell","element":{"type":"button","action_id":"dt_redeploy_api","text":{"type":"plain_text","text":"Redeploy"},"value":"api"}}],
			[{"type":"raw_text","text":"a service whose name is far too long to fit in its column"},{"type":"raw_text","text":"nobody"},{"type":"raw_number","value":7,"text":"7"},{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"link","url":"https://example.com/runs/7","text":"failing"}]}]},null],
			[{"type":"cell_text","text":"web"},{"type":"raw_text","text":""},{"type":"raw_number","value":1250,"text":"1,250"},{"type":"raw_text","text":"queued"},{"type":"action_cell","element":{"type":"button","action_id":"dt_redeploy_web","text":{"type":"plain_text","text":"Redeploy"},"value":"web","style":"primary"}}]]}]`},
		{Name: "datatable/short", Text: "a short table", Blocks: `[{"type":"data_table","rows":[[{"type":"raw_text","text":"Key"},{"type":"raw_text","text":"Value"}],[{"type":"raw_text","text":"region"},{"type":"raw_text","text":"us-east-1"}],[{"type":"raw_text","text":"replicas"},{"type":"raw_number","value":3,"text":"3"}]]}]`},
		{Name: "datatable/page size 3", Text: "Queue", Blocks: `[{"type":"data_table","block_id":"dtpage3","caption":"Queue","page_size":3,"rows":[` + boardRows(7) + `]}]`},
		{Name: "datatable/many columns", Text: "Quarterly numbers", Blocks: `[{"type":"data_table","block_id":"dtwide","caption":"Quarterly numbers","rows":[` + wideRows(9, 4) + `]}]`},
		{Name: "datatable/salesforce", Text: "Accounts", Blocks: `[{"type":"data_table","block_id":"dtsf","caption":"Accounts","is_salesforce":true,"rows":[[{"type":"raw_text","text":"Account"},{"type":"raw_text","text":"Stage"}],[{"type":"raw_text","text":"Acme"},{"type":"raw_text","text":"Closed won"}],[{"type":"raw_text","text":"Globex"},{"type":"raw_text","text":"Proposal"}]]}]`},
		{Name: "datatable/no rows", Text: "nothing yet", Blocks: `[{"type":"data_table","block_id":"dtnone","caption":"Nothing yet","rows":[]}]`},
		// what Slack's render throws on: the errorFallback
		{Name: "datatable/rows not an array", Text: "rows not an array", Blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"before the table"}},{"type":"data_table","block_id":"dtbad1","rows":{"a":1}},{"type":"section","text":{"type":"mrkdwn","text":"after the table"}}]`},
		{Name: "datatable/header string cell", Text: "a header cell that is a string", Blocks: `[{"type":"data_table","block_id":"dtbad2","rows":[["Name",{"type":"raw_text","text":"Value"}],[{"type":"raw_text","text":"a"},{"type":"raw_text","text":"b"}]]}]`},
		{Name: "datatable/caption object", Text: "a caption that is an object", Blocks: `[{"type":"data_table","block_id":"dtbad3","caption":{"text":"x"},"rows":[[{"type":"raw_text","text":"a"}]]}]`},
		// a mistyped field that Slack reads without throwing
		{Name: "datatable/mistyped page size", Text: "a page size that is a string", Blocks: `[{"type":"data_table","block_id":"dtodd","caption":7,"page_size":"2","row_header_column_index":"x","is_salesforce":"no","rows":[[{"type":"raw_text","text":"n"}],[{"type":"raw_number","value":"1","text":1}],[{"type":"raw_text","text":null}]]}]`},
	}
}

// boardNames are n made-up people ("member 1"…), JSON strings.
func boardNames(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"member ` + strconv.Itoa(i+1) + `"`)
	}
	return b.String()
}

// boardPoints are n points, falling, as chart data.
func boardPoints(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"label":"member ` + strconv.Itoa(i+1) + `","value":` + strconv.Itoa(480-i*37+(i%3)*5) + `}`)
	}
	return b.String()
}

// boardRows is a header row and n rows of raw_text cells: rank, name,
// points.
func boardRows(n int) string {
	var b strings.Builder
	b.WriteString(`[{"type":"raw_text","text":"Rank"},{"type":"raw_text","text":"Name"},{"type":"raw_text","text":"Points"}]`)
	for i := 0; i < n; i++ {
		b.WriteString(`,[{"type":"raw_text","text":"` + strconv.Itoa(i+1) + `"},{"type":"raw_text","text":"member ` + strconv.Itoa(i+1) +
			`"},{"type":"raw_text","text":"` + strconv.Itoa(max(0, 900-i*8)) + `"}]`)
	}
	return b.String()
}

// wideRows is a cols-wide header row and n rows of raw_number cells.
func wideRows(cols, n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for c := 0; c < cols; c++ {
		if c > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"type":"raw_text","text":"Q` + strconv.Itoa(c+1) + `"}`)
	}
	b.WriteByte(']')
	for r := 0; r < n; r++ {
		b.WriteString(",[")
		for c := 0; c < cols; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			v := strconv.Itoa((r*31 + c*17) % 200)
			b.WriteString(`{"type":"raw_number","value":` + v + `,"text":"` + v + `"}`)
		}
		b.WriteByte(']')
	}
	return b.String()
}
