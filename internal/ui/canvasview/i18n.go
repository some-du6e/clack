package canvasview

import "github.com/some-du6e/clack/internal/i18n"

// Slack's strings for the canvas chrome (internal/i18n, docs/I18N.md): the
// canvas components' namespaces, first one holding a string wins:
// slack_docs (the canvas header, access indicator, insert menu and
// placeholders), slack-docs (the share and open menus), quip_collab and
// quip (the editor and the edited time), then the file menu's and the
// Today view's strings the header shares.
var nsCanvas = i18n.NewChain("slack_docs", "slack-docs", "quip_collab", "quip", "share-dialog", "file", "today", "page-block", "agents")
