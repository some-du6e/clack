package popups

import (
	"image"
	"net/url"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/workobject"
)

// nsWorkObjects is Slack's work object components' I18n namespace.
var nsWorkObjects = i18n.NS("work-objects")

// WorkObjectMenuOpts are a work object card's menu inputs.
type WorkObjectMenuOpts struct {
	Entity *workobject.Entity
	// OpenURL is the item's link: the attachment's app_unfurl_url when it
	// has one (pFiZ B), else external_url.
	OpenURL string
	// Preview opens the details flexpane ("Preview in Slack").
	Preview func()
	// Share opens the share dialog; nil leaves "Share …" out.
	Share func()
	// Open opens a URL in the browser.
	Open func(u string)
	// Action sends one of the app's overflow actions.
	Action func(b workobject.Button)
	// AppName is the app's name, the actions' heading (UDTh jH);
	// "" falls back to the entity's app_name.
	AppName string
}

// NewWorkObjectMenu is WorkObjectContextMenu (boot-shared sqU2, build
// 4d0c78085232) for a resolved work object:
//
//	Preview in Slack (eye-open) — "View latest details" (info) for a file
//	  with no preview
//	──
//	Share {displayType} (share-message), Copy link to {displayType} (link;
//	  "Copy link" when the shared URL isn't the item's own)
//	──  the app's name, then its overflow actions (contextMenuAppActions)
//	──  Open in {productName} (new-window)
//
// Slack's "Open in new window" (a work object window) and the click
// preference submenu aren't here: Clack has no second window and no
// writer for work_objects_click_opens_url_prefs yet; Add to folder/
// section and Add as tab need the file-tab experiments the account
// doesn't have.
func NewWorkObjectMenu(h Host, anchor image.Point, o WorkObjectMenuOpts) *Menu {
	e := o.Entity
	if e == nil {
		return NewMenu(h, anchor, nil)
	}
	var items []MenuItem
	fileNoPreview := e.EntityType == workobject.EntityFile && !e.IsFullSizePreviewEnabled()
	if fileNoPreview {
		items = append(items, MenuItem{Key: "info", Label: nsWorkObjects.T("View latest details"), Icon: "info", Do: o.Preview})
	} else {
		items = append(items, MenuItem{Key: "view", Label: nsWorkObjects.T("Preview in Slack"), Icon: "eye", Do: o.Preview})
	}
	items = append(items, Sep())
	dt := e.DisplayType
	if o.Share != nil {
		items = append(items, MenuItem{Key: "share", Label: nsWorkObjects.T("Share {workObjectDisplayType}", "workObjectDisplayType", dt),
			Icon: "share-message", Do: o.Share})
	}
	copyLabel := nsWorkObjects.T("Copy link")
	if dt != "" && !woURLDiffers(e.ExternalURL, o.OpenURL) {
		copyLabel = nsWorkObjects.T("Copy link to {workObjectDisplayType}", "workObjectDisplayType", dt)
	}
	link := o.OpenURL
	items = append(items, MenuItem{Key: "copy", Label: copyLabel, Icon: "link", DoCtx: func(gtx layout.Context) { copyText(gtx, link) }})
	if acts := e.ContextMenuAppActions(); len(acts) > 0 {
		items = append(items, Sep(), MenuItem{Header: firstNonEmptyWO(o.AppName, e.AppName)})
		for i := range acts {
			b := acts[i]
			label := b.Label()
			items = append(items, MenuItem{Key: "app_action_" + b.ActionID, Label: label, Do: func() {
				if o.Action != nil {
					o.Action(b)
				}
			}})
		}
	}
	if link != "" && o.Open != nil {
		items = append(items, Sep(), MenuItem{Key: "open", Label: nsWorkObjects.T("Open in {productName}", "productName",
			firstNonEmptyWO(e.ProductName, o.AppName, e.AppName)), Icon: "new-window", Do: func() { o.Open(link) }})
	}
	return NewMenuOpts(h, items, MenuOpts{Anchor: pointRect(anchor), Within: "WorkObjectContextMenu"})
}

// woURLDiffers is SYVT's eQ: the shared URL (app_unfurl_url) points
// somewhere else than the item (another origin, a deeper path, a hash or
// more query parameters), so "Copy link" isn't the item's link.
func woURLDiffers(external, shared string) bool {
	if shared == "" || external == shared {
		return false
	}
	if external == "" {
		return true
	}
	a, err1 := url.Parse(external)
	n, err2 := url.Parse(shared)
	if err1 != nil || err2 != nil {
		return true
	}
	if a.Scheme+"://"+a.Host != n.Scheme+"://"+n.Host {
		return true
	}
	ap, np := strings.TrimSuffix(a.Path, "/"), strings.TrimSuffix(n.Path, "/")
	if ap == np {
		if n.Fragment != "" && n.Fragment != a.Fragment {
			return true
		}
		aq, nq := a.Query(), n.Query()
		for k, v := range aq {
			if nq.Get(k) != v[0] {
				return false
			}
		}
		return len(nq) > len(aq)
	}
	return strings.HasPrefix(np, ap+"/")
}

func firstNonEmptyWO(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
