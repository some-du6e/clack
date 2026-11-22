# Directories › External and Invitations (the Slack Connect hub)

Ported from Slack's web client, build a09b625d90aa (2026-11-22, public CDN
chunks): ExternalOrganizationsView (SjEJ), SlackConnectPage (gmwg),
ExternalOrganizationView (zJKG). Reads were checked read-only against Hack
Club (a grid where Slack Connect is limited to top-level admins, owners and
selected people); every write is tested against `internal/fakeslack` only.

| Surface | Slack | Clack | Tests |
| --- | --- | --- | --- |
| External lists | externalTeams/list connected (60) and available (20, `next_marker`), then externalTeams/counts; an available team the viewer already shares with is left out | `sc_external.go`, `core/slack_connect_hub.go` | TestSlackConnectExternalLikeSlack |
| Load More | the next page by marker, gone after the last | same | same |
| Search | externalTeams/search; "Organizations", or "No results found" | same | same |
| Banner and cards | SlackConnectHeroBannerCta: the DM card off without the DM eligibility (sharedInvites.canGetLink), the channel card off without canUserInviteExternalMembers, "Contact an admin…" for a non-admin | same | TestSlackConnectExternalLikeSlack, TestSlackConnectHubRoles |
| Admin CTAs | an org admin with no connections: Customize settings, Manage requests, Manage connections | same | TestSlackConnectHubRoles |
| Organization page | a row opens ExternalOrganizationView in the secondary pane: users.slackConnect.orgInfo, Flannel channels/info for its channels, Your channels / Your conversations / Other channels with their users/counts by_team captions; with a query channels/search `xws AND shared` and users/search `NOT deactivated AND NOT org`, both scoped by `external_team` (no search for an empty query); "No results" | `sc_org.go`, `core/slack_connect_org.go`, `ui/external_org.go` | TestSlackConnectOrgPageLikeSlack |
| Invitations list | connectInvites.list {invite_types [channel, im], include_invite_requests}; Sent and Received, newest first; Show all / Sent / Received | `sc_invites.go` | TestSlackConnectInvitationsLikeSlack |
| Invite actions | Copy link, Revoke / Disable link (conversations.revokeSharedInvite, sharedInvites.revoke, then the outgoing invites read again), Ignore, Accept (shared with Activity) | `sc_invites.go`, `sc_invite_actions.go` | same |
| Empty and error | Track your invitations; "Something’s fishy!" with Try again (always sent, past the client's backoff) | `sc_invites.go` | TestSlackConnectInvitationsStates |
| No stub pages | — | TestNoNotYetAvailable (Emoji packs is the one known page left) | red run 45020153984 on the pre-hub code |

## Leftovers

- The org page's "Show all people" tile (more than 24 people): it opens People filtered to the org (`selectedOrgs`), and People has no organization filter yet, so the tile isn't drawn.
- The org page's banner is the team image scaled up from a tiny copy rather than Slack's 20px CSS blur.
- Not ported: the hub's channel and people search beyond organizations (contact lookup), the Start a DM invite modal, the guidelines alert, the Manage menu, the trial and upgrade banners, email verification.
