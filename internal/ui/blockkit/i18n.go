package blockkit

import "github.com/some-du6e/clack/internal/i18n"

// Block Kit's modal chrome in the locale: the app view's own fallbacks
// (openConfirm's "Are you sure?", "OK", "Cancel"; renderBackButton's
// "Close": module 1r0Y, the app_view I18n) and the legacy dialog's
// "Submit" (app_dialog). Text an app sends is the app's and stays as sent.
var (
	appViewNS   = i18n.NS("app_view")
	appDialogNS = i18n.NS("app_dialog")
)

// bkSubmit is the submit button's fallback label.
func bkSubmit() string { return appDialogNS.T("Submit") }
