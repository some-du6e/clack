# The Redux mirror: Slack-shaped state for Taut plugins

Taut plugins read Slack's Redux store (`api.redux.getRawState()`, `useReduxState`, `getStore().getState()`) and patch it (`patchSlice`, `patchState`). Clack has no Redux store, so `internal/taut` keeps a mirror. Each slice is served by Go (`internal/taut/mirror.go`), built from `internal/core` and `internal/store`. JS caches a slice until a core event (or the mirror's change watcher) invalidates it. The shapes below were recorded from the live web client's store on 2026-11-07, keys and types only. No message content was saved.

Optional fields Slack leaves out when unset (avatar `image_*` URLs, bot icons, file thumbnails, …) are left out here too, never filled with `""` or 0, so plugins' `??` fallbacks work. Id-keyed slices are read one entry at a time (a `Proxy` in JS). Every other slice is read whole. A slice gets a new identity whenever something in it changes, so whole-slice selectors stay memoizable.

| slice | key | shape (what Clack fills in) | kept current by |
|---|---|---|---|
| `channels` | conversation id | the `conversations.info` object plus Slack's derived fields: `name_normalized`, `_name_lc`, `is_member`, `internalTeamIds`, `isNonExistent:false`, `isUnknown:false`, `previous_names`, `properties` (parsed) | `conv`, `conv_removed`, a fingerprint watch |
| `members` | user id | the `users.info` object plus `_name_lc`, `_real_name_lc`, `_display_name_lc`, `_real_name_normalized_lc`, `_display_name_normalized_lc`, `profile.*_normalized`, `isUnknown`/`isNonExistent:false`, `is_self` and `manual_presence` (self) | `user`, a fingerprint watch |
| `messages` | channel id → `{ts: message}` | each message as Slack stores it: the API JSON with Slack's own `blocks`, `attachments` (as sent), `bot_profile` and `metadata` (core loads history with `include_all_metadata`, so `getMessageMetadata` needs no request), plus `channel`, `_rxn_key: "message-<ts>-<channel>"`, `no_display:false`, `reply_count` (0 by default). The cached timeline and the open threads' replies are included | `message`/`update`/`delete`/`sent`, a fingerprint watch (history pages core loads without an event) |
| `channelHistory` | `C…` or `C…-<threadTs>` | `{reachedStart, reachedEnd, slices:[{timestamps, start, end}], prevReachedEnd}` | with `messages` |
| `membershipCounts` | channel id | `{counts:{member_count, current_team_member_count, restricted_member_count, member_count_by_team}}`, people only like the header (edge `users/counts`) | `conv` |
| `membership` | channel id → `{userId: {isKnown, isMember}}` | from the conversation's member list and `getChannelMembershipInfoForUsers` answers | that thunk, `conv` |
| `readOnlyChannels` | channel id | `{isReadOnly:true}` when the viewer can't post at the top level or in threads (`posting_restricted_to` and `threads_restricted_to`) | `conv` |
| `threadOnlyChannels` | channel id | `{isThreadOnly:true}` when only thread replies are allowed | `conv` |
| `reactions` | `message-<ts>-<channel>` (`_rxn_key`) | the message's `reactions` array (`[{name, users, count}]`), for every cached message that has reactions | with `messages` |
| `presence` | user id | `{id, presence}` | `presence` |
| `unreadChannelCounts` | channel id | `{unreadCnt, unreadHighlightCnt, unreadVIPMentionCnt}` | `message`, `marked`, `counts` |
| `channelCursors` | channel id | last read ts | `marked`, `counts` |
| `bots` | bot id | `{id, name, deleted, icons:{image_36,48,72}}` | — |
| `userGroups` | usergroup id | `{id, handle, name, description, user_count, is_usergroup, is_subteam, _name_lc}` | — |
| `teams` | team / enterprise id | `{id, name, domain, url, enterprise_id, enterprise_name, icon}` | — |
| `pendingFileUploads` | pendingFileId (`clack_pending_<n>`) | the composers' attached, unsent files: `{id, pendingFileId, title, name, size, mimetype, status: "pending"\|"uploaded", file:{name, title, size, type}, fileId?}`. `title` is what the chip shows and the file is shared under; `name` is what its bytes were uploaded under | the composer (`taut.PendingFiles`, `Taut.PendingFilesChanged`; files.go) |
| `files` | Slack file id | the pending files whose bytes are up: `{id, name, title, size, mimetype, is_external:false}` | same |
| `bootData` | whole | `{user_id, team_id, enterprise_id, page_needs_enterprise, version_ts, version_uid}` | — |
| `selfTeamIds` | whole | `{teamId (org on a grid), enterpriseId, internalWorkspaceIds, defaultWorkspaceId}`. `workspace.isHackClub()` reads this | — |
| `prefs` | whole | `{user:{…}, team:{}, channel:{}, enterprise:{}}`. `user` uses Slack's pref names (`emoji_mode`, `time24`, `display_real_names_override`, `messages_theme`, `muted_channels`, …) | `prefs`, `pref_change` |
| `userPrefs` | whole | the same user prefs (SlimMessageBox and ShowSendingBot read this slice) | same |
| `channelSections` | whole | `{channelSectionById, orderedChannelSectionList, channelIdsByChannelSectionId, channelSectionIdsByChannelId, autoSortedSectionIdByChannelId}` | `sidebar_changed`, `channel_sections_*` |
| `typing` | whole | `{key: {channelId, threadTs, memberId, memberIsSelf, timestamp}}`, expires after 6 s | `typing` |
| `drafts` | whole | `{unifiedDrafts:{id:{id, client_draft_id, destinations, blocks, ops, file_ids, date_created, date_scheduled, last_updated_ts}}, channelIds, …}` | `draft`, `Taut.SetDrafts` |
| `messageEditWindow` | whole | `{windowId: {channelId, ts}}` | `Taut.SetEditing` (ui-messages) |
| `searchResults` | whole | `{instance: {response: <search API JSON>}}` | `Taut.SetSearchResults` (ui-popups) |
| `slashCommand` | whole | `{commands: {name or appId-/name: {name, desc, usage, type, app, app_name, autocomplete}}}`, loaded on first read | — |
| `dndV2` | whole | `{currentMemberSnooze:{snoozeEnabled, snoozeEndTs}, currentStatuses, schedules, profileStatuses}` | `dnd_updated*` |
| `allCalls` | whole | `{byId: {roomId: {id, name, channels, createdBy, hasEnded, participants, dateStart, dateEnd}}}` | `huddle` |
| `experiments` | whole | `{}`. Clack runs no experiments, but `api.experiments.set` shows forced assignments here | — |
| `huddleEvents`, `peers`, `huddlesCanvas`, `alert`, `unreadCounts`, `mutedChannels`, `starredChannels` | whole | Slack's empty shapes, or the counts core has | — |

Plain actions plugins dispatch go to JS handlers (`OPEN_MODAL`, `SET_LOCAL_DRAFT`, `JUMP_TO_REPLY`, `UPDATE_PENDING_FILE` from the `updatePendingFile` export, which renames a pending file through `taut.RenamePendingFile`, the huddle log actions). Thunks are covered in [PLUGINS-taut.md](PLUGINS-taut.md).
