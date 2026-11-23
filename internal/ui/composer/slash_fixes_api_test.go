package composer

import (
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The fixes' API the tests call (slash_fixes_test.go): one place for the
// red tree to stand in for (red-evidence-branch).

func setEditInThread(c *Composer, v bool) { c.EditInThread = v }

func prepareWithStore(c *Composer, d *Doc) (*Doc, slugStore) { return c.prepareMessageStore(d) }

func processedOutgoing(doc *Doc, st slugStore) *outgoing {
	return &outgoing{doc: doc, text: doc.Mrkdwn(), blocks: doc.RichTextBlocks(), store: &st, processed: true}
}

func reminderDialog(c *Composer) (*popups.Modal, *TextyInput) { return c.reminderModal(c.ch, c.ts) }

// barredUsers are a profile-only user and one an information barrier
// keeps away (is_profile_only_user, interact false).
func barredUsers(prof, walled slack.Profile) []slack.User {
	no := false
	return []slack.User{{ID: tProf, Name: "pat", IsProfileOnlyUser: true, Profile: prof},
		{ID: tWalled, Name: "walled", Interact: &no, Profile: walled}}
}
