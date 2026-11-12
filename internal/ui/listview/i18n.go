package listview

import "github.com/some-du6e/clack/internal/i18n"

// Slack's strings for lists (internal/i18n, docs/I18N.md): the lists
// namespace, then the file and files menus', file-permissions' share
// levels, tabbed-channel's tab errors and the shared pickers' strings,
// first one holding a string wins.
var nsLists = i18n.NewChain("lists", "file", "files", "file-permissions", "tabbed-channel", "quip", "todos", "block_kit", "huddles", "search", "agents")
