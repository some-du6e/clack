# Inviting people to the workspace

Slack's invite modal as Clack draws it, with the Slack modules each part
comes from. Build 4d0c78085232 of the public web client, from the CDN with
no cookie. Code: `internal/core/invite.go` (rules and API),
`internal/ui/popups/invite.go` (the dialog), `invite_tokens.go` (the email
field), `internal/fakeslack/invites.go` (the test server's recorder).

## Entry points

All three open `openInviteModal` (boot-shared `CJ8h` `nq`, exported as `A`).

| Where | Slack | Source (campaign) | Shown when |
|---|---|---|---|
| Team menu, "Invite people to {teamDisplayName}" | `/fB9` workspace menu, key `invite-people` | `team_menu` | `canUserSeeInviteModal` (`c1OI Lz`) |
| Create menu (rail +), "Invite people" | `nx8t` `onInviteClick` | `sidebar_create_new_invite_people` | `canUserSeeInviteModal` |
| Huddles page, "Add coworkers" | `Igs2` `HuddlesPageInviteTeammates` | `huddles_page` | the card's own rule (frontline teams without invite rights hide it) |

Before this, the team menu row toasted "Invites aren't supported in
yoloslack" and the Create menu row "Inviting people isn't available in
Clack yet". "Add coworkers" did nothing.

## Who may invite (`c1OI`)

- `canUserInviteMembersToWorkspace`: a workspace admin or owner, or a full
  member while the team pref `invites_only_admins` is off.
- `canUserRequestToInvite`: a full member while `invite_requests_enabled`
  is on.
- `canUserSeeInviteModal`: either of the two. Guests (`is_restricted`) and
  frontline members (`restricted_user_type` `frontline_worker`) never see it.
- `isRequestingInvitesForType`: a member invite from a non-admin while
  `invites_only_admins` is on, or a guest invite from someone who can't
  invite guests. The send then goes to an admin.
- `canUserSeeAccountTypeOptions`: Guest is offered under "Invite as" on paid
  teams to anyone who may request, or who may invite guests.

Team prefs are read the way Slack reads them, `!!getTeamPref(name)`. That is
JavaScript truthiness, so the string `"false"` counts as on.

Clack has no role grants (Slack's `QaSy` `INVITE_GUEST` and
`ADD_FULL_USER_TO_WORKSPACE`). A workspace admin or owner stands in for
them, which is also Slack's fallback (`7RIn` `canUserActAsWorkspaceAdmin`).

## The form (`CJ8h` `InviteForm`)

Hack Club is on Enterprise Grid, and Clack draws the form as Grid does. Off
Grid, Slack draws the simplified form (`wfeE` `m`: labels "Email addresses"
and "Invite as", the placeholder "Ex. {email1}, {email2}"). That variant
isn't ported. The order:

1. Title: "Invite people to {workspaceName}", or "Request invitations to
   {workspaceName}" for a request (`invite-to-workspace`).
2. "To:" and `TokenizerEmailInput` (`invite` namespace). The field is large:
   at least 44px, at most 200, 32px tokens with 18px text (`c-token--large`).
   The placeholder is "name@{domain}", where the domain comes from the
   viewer's email ("example.com" without one).
   - Text becomes tokens on a separator (`, ， 、 ; ； | ｜`, tab, newline),
     on a trailing space, on Enter and on blur (`m5BY tokenizeFilter`,
     `B7l2` `EmailInput`). A paste goes through `splitToEmails`: "Name
     <a@b.c>" lists give their addresses (`sdt0` `defaultSplitQuery` with
     `matchEmailField`), "a@b c@d" splits on spaces, `mailto:` is dropped.
   - Token errors (`1Glt` `deriveTokenState`): `invalid_email` (`itIg`
     `isEmail`), `duplicate` (case- and space-insensitive), `self_invite` (the
     viewer's email), `already_in_workspace` (a loaded member with that
     email). A bad token has the `surf-imp` background and a dashed
     `otl-imp` border. Hovering it shows the reason ("Invalid email
     address", "Duplicate email", "Already in this workspace", "You can’t
     invite yourself").
   - Under the field, one line per kind of error (`hSb/`
     `MultiSelectErrorText`): "N errors." with "Remove all items with
     errors", "N existing members." with "Remove existing members", "You
     can’t invite yourself." with "Remove your email address", "N duplicate
     emails." with "Remove duplicates", and "You can only add 200 people at a
     time." with "Remove the last N".
   - Send is off while any token has an error, while there is no valid
     address, or past 200 (`isBlocked`).
3. "Invite as:" Member / Guest (`InviteTypeSelect`, paid teams only).
4. "**Add to team channels** (optional)" for members, "(required)" for
   guests, under the line "Make sure your teammates are in the right
   conversations from the get go." (`ChannelSelect`). The search covers the
   channels Clack knows (not archived, not shared). One channel makes a
   single-channel guest. The box "Allow them to be added to more channels…"
   makes that a multi-channel guest, and so do two or more channels, with
   the billing note.
5. For a guest, "**Set an expiration date**": No limit, 7, 30 or 60 days,
   expiring at 11:59 PM that day in the viewer's time zone (`expiration_ts`,
   `6FNz getExpirationTs`).
6. "Add a message" (`CustomizeButton`, magic-wand). It opens "**Custom
   message:**" with the placeholder "Add a personal note to your
   invitation", at most 560 characters. Paid teams only, not for requests.
7. For a request: "**Reason for request** (optional)" with the placeholder
   "Add a note for your admin", and the admin-approval note "Your request
   will be sent to your admins, and you’ll be notified when it’s approved or
   denied." (`ReasonForRequest`). For a member request, the channel select
   comes after it, as in Slack.
8. A failed send's banner, from invite-errors' `getInviteErrorText` (`kqqh`,
   the codes an invite to the workspace gets).
9. Footer: "Copy invite link" at the left when the team has links (not
   Grid, not a request, not a guest invite: `shouldShowInviteLinkComponents`),
   "Learn more about guests" for a guest invite, and Send / Send Request.

### The invite link (`SharedInviteLink`, `G3qv`, `3Lll`)

When the modal opens, `users.sharedInvites.list` (`mode`
`last_active_for_user`) finds the viewer's newest link that hasn't been
deleted and hasn't expired. "Copy invite link" copies it, or first mints one
with `users.admin.createSharedInvite` (`expiration` 30, `max_signups` 400).
The tooltip says "Invite your coworkers with this link". After a copy it
says "**Link copied** - expires in N days" ("never expires" past 365, "in
over 30 days", "today"). The tooltip's onHide resets it.

## The API (`6FNz` `sendInvites`)

Slack's `createApiMethodFetcher` (`WH55`) snake-cases the arguments. The
invites are `InviteBulkApiInvite` records (`Smwf tw`), sent as JSON.

- Direct: `users.admin.inviteBulk` with `team_id`, `invites`
  (`[{"email":…,"mode":"manual","type":"regular"}]`), `restricted`,
  `ultra_restricted`, `campaign` (the source), `channels` (ids, comma
  separated), `extra_message`, and `expiration_ts` for guests. The answer
  lists each invite's `ok` and `error`.
- Request: `users.inviteRequests.create` with the same arguments, `requests`
  in place of `invites` and `requester_message` in place of
  `extra_message`. Success means every request reached the admins. A failure
  may carry `requests` with each one's error. An error that has text in
  `kqqh` stays in the form. Any other error goes to the summary.

The tests and the shots only ever talk to `internal/fakeslack`, which
records the calls (`InviteCalls`) and answers them. A local part with
"already" gets `already_in_team`, one with "fail" gets `invite_failed`.
`SetInviteError` fails the whole call.

## The summary (`/oFV` `ConfirmationScreen`, `kWXe` `InvitesSummaryList`)

The form closes, and the summary opens with no header: a 60px circle (a
check on green when everything went out, otherwise a warning on orange),
then "Sent", "Unable to send all" or "Unable to send" (22/900). Below that
comes one row per address, failures first, grouped by error: a 34px tile
(paper-plane, or warning on raspberry), the address in bold, and the status
at the right ("Invited as a coworker", "Invited as a guest", "Couldn’t
invite") with an info icon. The row's tooltip gives the detail ("Your admin
has to review this first." for requests, kqqh's text or "Hmm, something went
wrong. Try again?" for failures). Admins get "See past invites" at the
footer's left. The buttons are Invite More People (a new form that keeps the
channels) and Done (`add-to-slack` namespace).

## Not done

- Google Workspace directory search and its banner (they need Google
  sign-in).
- The recommended channels above the channel select, and "Working with
  another company? Add people using Slack Connect" under "Invite as".
- The custom expiration date, the email preview, and the invite link's
  settings form (`SHARED_INVITE_LINK_FORM`, "Edit link settings" for
  admins).
- The off-Grid simplified form, the workspace picker for multi-workspace
  orgs (`WORKSPACE_SELECT`), the close speed bump, and the
  verified-email prompt.
- "Already in this workspace" only knows the members Clack has loaded with
  an email. Hack Club hides everyone's email but the viewer's, so in
  practice it catches only the viewer. Slack asks its member searcher.
