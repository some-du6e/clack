package ui

import "testing"

// TestSlackTabViews: the primary view Slack has for a channel showing one
// of its header tabs, as plugins read it (Slick's ChatTabs comes back to
// it): a canvas or list tab's Canvas or List view in the channel's tab
// (toCanvas or toList, then inChannelTab), any other tab the channel's view
// with the tab's id (toChannelTab), Messages too once a tab was picked; ""
// (the channel as a plain navigation shows it) before. A tab link for
// "channel" (navigate's, for Messages) takes the Messages tab.
func TestSlackTabViews(t *testing.T) {
	const ch = "C0GENERAL1"
	for _, c := range []struct {
		tab    chTab
		picked bool
		want   string
	}{
		{chTab{key: "messages"}, false, ""},
		{chTab{key: "messages"}, true, `{"id":"C0GENERAL1","params":{"parentTab":"C0GENERAL1","tabId":"channel"},"viewType":"Channel"}`},
		{chTab{key: "tab:Ct0CANVAS1", typ: "canvas", fileID: "F0CANVAS1"}, true,
			`{"id":"Qp:F0CANVAS1","params":{"entityId":"Qp:F0CANVAS1","parentTab":"C0GENERAL1","tabId":"Ct0CANVAS1"},"viewType":"Canvas"}`},
		{chTab{key: "tab:Ct0LIST01", typ: "list", fileID: "F0LIST001", defaultView: "View0ALL"}, true,
			`{"id":"F0LIST001","params":{"listId":"F0LIST001","listViewId":"View0ALL","parentTab":"C0GENERAL1","tabId":"Ct0LIST01"},"viewType":"List"}`},
		{chTab{key: "tab:files", typ: "files"}, false, `{"id":"C0GENERAL1","params":{"parentTab":"C0GENERAL1","tabId":"files"},"viewType":"Channel"}`},
		{chTab{key: "tab:pins", typ: "pins"}, true, `{"id":"C0GENERAL1","params":{"parentTab":"C0GENERAL1","tabId":"pins"},"viewType":"Channel"}`},
	} {
		if got := slackTabView(ch, c.tab, c.picked); got != c.want {
			t.Errorf("%s (picked %v): %s, want %s", c.tab.key, c.picked, got, c.want)
		}
	}
	a := &App{}
	tabs := []chTab{{key: "messages"}, {key: "tab:Ct0CANVAS1"}, {key: "tab:files"}}
	for id, want := range map[string]int{"channel": 0, "Ct0CANVAS1": 1, "files": 2} {
		a.wantTabLink(ch, id)
		if i := a.takeTabLink(ch, tabs); i != want {
			t.Errorf("a tab link for %q took tab %d, want %d", id, i, want)
		}
	}
}
