package richview

import rt "github.com/some-du6e/clack/internal/richtext"

// The email tests' reads of new_email_rendering (a red run replaces this
// file with stubs: no flag, V2 everywhere).

func setEmailRendering(st *Style, on bool) { st.SetNewEmailRendering(on) }

func emailActions(f *rt.File, v1 bool) []barBtn { return fileActionsFor(f, false, false, v1) }

func emailMeta(f *rt.File, v1 bool) string { return fileMetaTextFor([]*rt.File{f}, true, v1) }

// openRecipients opens (or closes) the card's recipient row label.
func openRecipients(f *rt.File, label string, open bool) {
	setKitExpanded(emailRcptKey(f, label), open)
}
