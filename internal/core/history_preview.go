package core

// "Include conversation history?" when adding people to a DM
// (MpdmAddPeopleModal, boot-shared TWss; SelectHistoryOptionForm and
// HistoryPreview, boot-shared hLnn; gantry-v2 build d3de3a487b1c,
// 2026-11-12):
//
//   - when the modal opens it loads the conversation's first message:
//     fetchPreviewHistory (boot-shared GLNO) {channelId, oldest:
//     "0000000000.000001", limit: 1}; its ts is oldestTs, which labels
//     "From the beginning ({date})" and bounds the date picker. A first
//     message from today hides "From today" and "Custom".
//   - HistoryPreview loads the messages from the chosen start:
//     fetchPreviewHistory {oldest: start, limit: 200} (the "Fetch
//     conversation history" thunk: conversations.history with oldest,
//     inclusive).
//   - getRowsAndKeys (hLnn) builds the preview's rows: messages that
//     aren't hidden (p6HR dK) with ts ≥ start, oldest first; a thread
//     broadcast only when its root is in the range (R+b2 SM), other
//     replies never (azCc n); a day divider whenever the day changes
//     (hJ57: local midnight); past 200 rows the list is cut and ends with
//     "And all other messages in this conversation".

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// HistoryFromBeginning is the start "From the beginning" sends (TWss em).
const HistoryFromBeginning = "0000000000.000001"

// HistoryPreviewLimit is getRowsAndKeys' row cap and the fetch's limit.
const HistoryPreviewLimit = 200

// HistoryPreview is fetchPreviewHistory from oldest: up to limit messages,
// oldest first.
func (a *App) HistoryPreview(ctx context.Context, ch, oldest string, limit int) ([]Message, error) {
	p, err := a.C.History(ctx, ch, slack.HistoryOpts{Oldest: oldest, Limit: limit, Inclusive: true})
	if err != nil {
		return nil, err
	}
	a.EnsureUsers(ctx, referencedUsers(p.Messages))
	var msgs []slack.Message
	for _, m := range p.Messages {
		if !m.Hidden { // getRowsAndKeys drops hidden messages (p6HR isHidden)
			msgs = append(msgs, m)
		}
	}
	sort.SliceStable(msgs, func(i, j int) bool { return tsLess(msgs[i].TS, msgs[j].TS) })
	return a.messages(ch, msgs), nil
}

// HistoryOldest is the modal's first fetch: the conversation's first
// message's ts ("" when it has none).
func (a *App) HistoryOldest(ctx context.Context, ch string) (string, error) {
	p, err := a.C.History(ctx, ch, slack.HistoryOpts{Oldest: HistoryFromBeginning, Limit: 1})
	if err != nil {
		return "", err
	}
	oldest := ""
	for _, m := range p.Messages {
		if oldest == "" || tsLess(m.TS, oldest) {
			oldest = m.TS
		}
	}
	return oldest, nil
}

func tsLess(a, b string) bool {
	x, _ := strconv.ParseFloat(a, 64)
	y, _ := strconv.ParseFloat(b, 64)
	return x < y
}

// HistoryRow is one getRowsAndKeys row.
type HistoryRow struct {
	Kind string    // message | day_divider | more_messages
	Msg  *Message  // message
	Day  time.Time // day_divider: local midnight
}

// HistoryPreviewRows is getRowsAndKeys over msgs (oldest first, hidden
// ones already dropped by HistoryPreview) from start (a unix-seconds
// string; "" for none).
func HistoryPreviewRows(msgs []Message, start string, loc *time.Location) []HistoryRow {
	if loc == nil {
		loc = time.Local
	}
	var in []int
	keys := map[string]bool{}
	for i, m := range msgs {
		if start != "" && tsLess(m.TS, start) {
			continue
		}
		in = append(in, i)
		keys[m.TS] = true
	}
	var rows []HistoryRow
	for n, i := range in {
		m := &msgs[i]
		broadcast := m.Broadcast
		rootIn := broadcast && m.ThreadTS != "" && keys[m.ThreadTS]
		reply := m.ThreadTS != "" && m.ThreadTS != m.TS
		if !(broadcast && rootIn || !broadcast && !reply) {
			continue
		}
		day := historyDay(m.TS, loc)
		prev := ""
		if n > 0 {
			prev = historyDayKey(msgs[in[n-1]].TS, loc)
		}
		if historyDayKey(m.TS, loc) != prev {
			rows = append(rows, HistoryRow{Kind: "day_divider", Day: day})
		}
		rows = append(rows, HistoryRow{Kind: "message", Msg: m})
	}
	if len(rows) > HistoryPreviewLimit {
		rows = append(rows[:HistoryPreviewLimit:HistoryPreviewLimit], HistoryRow{Kind: "more_messages"})
	}
	return rows
}

func historyDay(ts string, loc *time.Location) time.Time {
	f, _ := strconv.ParseFloat(ts, 64)
	t := time.Unix(int64(f), 0).In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func historyDayKey(ts string, loc *time.Location) string {
	return strconv.FormatInt(historyDay(ts, loc).Unix(), 10)
}
