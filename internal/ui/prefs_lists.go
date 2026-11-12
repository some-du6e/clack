package ui

import (
	"context"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The lists on Connected accounts and Privacy & visibility, fetched the
// first time their page is drawn (Slack's useFetcher hooks run on mount).

type prefsLists struct {
	connStarted, connDone bool
	conn                  []core.WorkflowAppAuth

	blockStarted, verifyDone, verified, blockDone bool
	blocked                                       []string
}

// loadConnected is ConnectedWorkflowAccountsList's fetchUserAuth.
func (p *prefsModal) loadConnected() {
	l := &p.lists
	c := p.a.Core()
	if l.connStarted || c == nil {
		return
	}
	l.connStarted = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		apps, _ := c.WorkflowAppAuths(ctx) // an error shows "No accounts found", like an empty list
		p.a.Post(func() { l.conn, l.connDone = apps, true; p.a.Invalidate() })
	}()
}

// loadBlocked is useMemberEmailVerificationStatus, then (verified)
// fetchAndUpsertAllBlockedEmails.
func (p *prefsModal) loadBlocked() {
	l := &p.lists
	c := p.a.Core()
	if l.blockStarted || c == nil {
		return
	}
	l.blockStarted = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ok, _ := c.EmailVerified(ctx)
		var emails []string
		if ok {
			emails, _ = c.BlockedInviteEmails(ctx)
		}
		p.a.Post(func() {
			l.verifyDone, l.verified, l.blocked, l.blockDone = true, ok, emails, true
			p.a.Invalidate()
		})
	}()
}

// connectedList draws "Apps connected through workflows"' rows.
func (p *prefsModal) connectedList(f *prefsForm) {
	p.loadConnected()
	l := &p.lists
	if !l.connDone {
		return // Slack's spinner
	}
	if len(l.conn) == 0 {
		f.para(cwListNS.T("No accounts found"))
		return
	}
	for _, ap := range l.conn {
		p.connectedRow(f, ap) // ConnectedWorkflowAccount, with Manage (prefs_connected.go)
	}
}

// blockedSection is maybeRenderBlockedSection's body.
func (p *prefsModal) blockedSection(f *prefsForm) {
	p.loadBlocked()
	l := &p.lists
	if !l.blockDone {
		return // the spinner
	}
	if !l.verified {
		email := ""
		if c := p.a.Core(); c != nil {
			if u, ok := c.User(p.a.model().SelfID); ok {
				email = u.Email
			}
		}
		p.verifySection(f, email) // prefs_verify_email.go
		return
	}
	if len(l.blocked) == 0 {
		f.muted("You haven’t blocked any invitations from anyone yet.", false)
		return
	}
	for _, e := range l.blocked {
		e := e
		f.listRowIcon("blocked."+e, "", e, "Unblock email", func() { p.confirmUnblock(e) })
	}
}

// confirmUnblock is UnblockConfirmationModal: "Are you sure?", Cancel /
// Unblock Invitations (users.removeUserBlock).
func (p *prefsModal) confirmUnblock(email string) {
	a := p.a
	popups.Confirm(a, "Are you sure?", "You’ll start receiving Slack Connect invitations from "+email+" again.", "Unblock Invitations", true, nil, func() {
		c := a.Core()
		if c == nil {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err := c.UnblockInvites(ctx, email)
			a.Post(func() {
				if err != nil {
					a.Toast("Couldn't unblock " + email + ": " + err.Error())
					return
				}
				var keep []string
				for _, x := range p.lists.blocked {
					if x != email {
						keep = append(keep, x)
					}
				}
				p.lists.blocked = keep
				a.Invalidate()
			})
		}()
	})
}

// hiddenSection is maybeRenderHiddenUsersSection's list: users.prefs
// hidden_users by name; unhiding writes the list without them
// (unhideUserApi).
func (p *prefsModal) hiddenSection(f *prefsForm) {
	ids := core.SearchExcludedChannels(p.prefStr("hidden_users"))
	c := p.a.Core()
	var shown int
	for _, id := range ids {
		name, avatar := id, ""
		if c != nil {
			if u, ok := c.User(id); ok {
				if u.Deleted {
					continue // !isDeleted
				}
				name, avatar = u.Name, u.Avatar
			} else {
				ensureUserAsync(p.a, id)
			}
		}
		id := id
		shown++
		f.listRowIcon("hidden."+id, avatar, name, "Unhide user", func() {
			var keep []string
			for _, x := range ids {
				if x != id {
					keep = append(keep, x)
				}
			}
			p.setPref("hidden_users", strings.Join(keep, ","))
		})
	}
	if shown == 0 {
		f.muted("You haven’t hidden anyone yet.", false)
	}
}

// findWorkspacesURL is Rgd9's get-started page (#/find), where Slack's
// "Find my workspaces" sends you.
const findWorkspacesURL = "https://slack.com/ssb/get-started#/find"
