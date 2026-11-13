package core

import (
	"context"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// channelFilesWant is fetchSearchModule's args for the tab's file
// browsers plus getSearchAPIParams' files arguments and the sort.
func channelFilesWant(query, page, sortBy, dir string) map[string]string {
	return map[string]string{"module": "files", "query": query, "page": page, "client_req_id": "*", "browse_session_id": "*",
		"extracts": "1", "highlight": "1", "max_extract_len": "200", "extra_message_data": "1", "no_user_profile": "1",
		"count": "50", "file_title_only": "false", "query_rewrite_disabled": "false", "include_files_shares": "1",
		"search_context": "desktop_files_channel_tab", "max_filter_suggestions": "10", "sort": sortBy, "sort_dir": dir,
		"search_exclude_bots": "false", "search_only_my_channels": "false", "shared_with_you": "0"}
}

// The tab's two file browsers send the filters as Slack's
// querySuggestionFromQueryAndFilter writes them: the words, in:<#C|name>,
// then type: for media or -type: for the rest.
func TestChannelFilesParams(t *testing.T) {
	wantForm(t, "files", channelFilesParams(ChannelFilesOpts{Channel: "C1"}, "general", false),
		channelFilesWant("in:<#C1|general> -type:images -type:videos -type:folder", "1", "timestamp", "desc"))
	wantForm(t, "media oldest", channelFilesParams(ChannelFilesOpts{Channel: "C1", Media: true, Sort: ChannelFilesOldest, Page: 2}, "gen>eral", false),
		channelFilesWant("in:<#C1|general> type:images type:videos", "2", "timestamp", "asc"))
	wantForm(t, "search", channelFilesParams(ChannelFilesOpts{Channel: "G1", Query: " plan ", Sort: ChannelFilesRelevant}, "mpdm-a--b-1", true),
		channelFilesWant("plan in:<#G1> -type:images -type:videos -type:folder", "1", "score", "desc"))
}

func channelFilesFake(t *testing.T) (*App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return New(slack.New(fs.Creds()), store.New("")), fs
}

// Against fakeslack: media is the gallery's images and videos, files the
// rest with the channel's canvas and list, newest share first; links
// page by ten and their args are fetchChannelLinks'.
func TestChannelFilesFake(t *testing.T) {
	a, fs := channelFilesFake(t)
	ctx := context.Background()
	media, err := a.ChannelFiles(ctx, ChannelFilesOpts{Channel: fakeslack.FilesChannelID, Media: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(media.Files) == 0 {
		t.Fatal("no media")
	}
	for _, f := range media.Files {
		if !strings.HasPrefix(f.Mime, "image/") && !strings.HasPrefix(f.Mime, "video/") {
			t.Errorf("media has %s (%s)", f.ID, f.Mime)
		}
	}
	files, err := a.ChannelFiles(ctx, ChannelFilesOpts{Channel: fakeslack.FilesChannelID})
	if err != nil {
		t.Fatal(err)
	}
	var canvas, list bool
	for i, f := range files.Files {
		if strings.HasPrefix(f.Mime, "image/") || strings.HasPrefix(f.Mime, "video/") {
			t.Errorf("files has media %s", f.ID)
		}
		canvas = canvas || f.IsCanvas
		list = list || f.IsList
		if i > 0 && f.Timestamp > files.Files[i-1].Timestamp {
			t.Errorf("files not newest first at %d", i)
		}
	}
	if !canvas || !list {
		t.Errorf("files: canvas %v list %v, want both", canvas, list)
	}
	forms := fs.SearchForms("search.modules.files")
	if len(forms) != 2 || !strings.HasSuffix(forms[0].Get("query"), "type:images type:videos") ||
		!strings.HasSuffix(forms[1].Get("query"), "-type:folder") {
		t.Fatalf("search forms %v", forms)
	}

	p1, err := a.ChannelLinks(ctx, fakeslack.FilesChannelID, "", ChannelFilesNewest, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.Links) != fakeslack.ChannelLinksPerPage || p1.Pages != 2 || p1.Page != 1 {
		t.Fatalf("links page 1: %d links, page %d/%d", len(p1.Links), p1.Page, p1.Pages)
	}
	if p1.Links[0].Title == "" || p1.Links[0].IconURL == "" || p1.Links[0].Timestamp == 0 {
		t.Errorf("first link %+v", p1.Links[0])
	}
	p2, err := a.ChannelLinks(ctx, fakeslack.FilesChannelID, "", ChannelFilesOldest, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Links) != fakeslack.ChannelLinksCount-fakeslack.ChannelLinksPerPage {
		t.Errorf("links page 2: %d", len(p2.Links))
	}
	if _, err := a.ChannelLinks(ctx, fakeslack.FilesChannelID, " roadmap ", ChannelFilesRelevant, 1); err != nil {
		t.Fatal(err)
	}
	lf := fs.ChannelLinksForms()
	if len(lf) != 3 {
		t.Fatalf("%d searchLinks calls", len(lf))
	}
	for _, f := range lf {
		f.Del("token") // the client's credential, not one of the fetcher's args
	}
	wantForm(t, "links", lf[0], map[string]string{"channel_id": fakeslack.FilesChannelID, "page": "1", "sort": "timestamp", "sort_dir": "desc"})
	wantForm(t, "links oldest", lf[1], map[string]string{"channel_id": fakeslack.FilesChannelID, "page": "2", "sort": "timestamp", "sort_dir": "asc"})
	wantForm(t, "links search", lf[2], map[string]string{"channel_id": fakeslack.FilesChannelID, "page": "1", "query": "roadmap",
		"sort": "timestamp", "sort_dir": "desc"}) // Most relevant ("score") isn't in fetchChannelLinks' switch
}

// #files-gallery's tab bar has the Files & links tab.
func TestChannelFilesTabListed(t *testing.T) {
	a, _ := channelFilesFake(t)
	tabs, err := a.Tabs(context.Background(), fakeslack.FilesChannelID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].Type != "files" || tabs[0].Title != "Files & links" {
		t.Fatalf("tabs %+v", tabs)
	}
}
