package composer

import (
	"context"
	"encoding/json"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// coreapi.go is the only file that calls into *core.App. The rest of the
// package goes through these helpers, so a core signature change is a
// one-file fix.

type coreAPI struct{ a *core.App }

func (c coreAPI) self() core.User                         { return c.a.Self() }
func (c coreAPI) user(id string) (core.User, bool)        { return c.a.User(id) }
func (c coreAPI) users(ids []string) map[string]core.User { return c.a.Users(ids) }
func (c coreAPI) conv(id string) (core.Conv, bool)        { return c.a.Conv(id) }
func (c coreAPI) snapshot() core.Snapshot                 { return c.a.Snapshot() }
func (c coreAPI) customEmoji() map[string]string          { return c.a.CustomEmoji() }
func (c coreAPI) emoji(name string) (url, text string, ok bool) {
	return c.a.Emoji(name)
}

func (c coreAPI) send(ctx context.Context, ch, threadTS, text string, blocks json.RawMessage, broadcast bool) (core.Message, error) {
	return c.a.Send(ctx, core.SendOpts{Channel: ch, ThreadTS: threadTS, Text: text, Blocks: blocks, Broadcast: broadcast})
}

func (c coreAPI) edit(ctx context.Context, ch, ts, text string, blocks json.RawMessage) error {
	return c.a.Edit(ctx, ch, ts, text, blocks)
}

func (c coreAPI) typing(ch, threadTS string) { _ = c.a.Typing(ch, threadTS) }

func (c coreAPI) upload(ctx context.Context, o core.UploadOpts) ([]core.UploadedFile, error) {
	return c.a.Upload(ctx, o)
}

func (c coreAPI) searchUsers(ctx context.Context, q string) ([]core.User, error) {
	return c.a.SearchUsers(ctx, q, 20)
}

func (c coreAPI) searchChannels(ctx context.Context, q string) ([]core.ChannelHit, error) {
	return c.a.SearchChannels(ctx, q, 20)
}

func (c coreAPI) usergroups(ctx context.Context) ([]core.Usergroup, error) {
	return c.a.Usergroups(ctx, "", 100)
}

func (c coreAPI) members(ctx context.Context, ch string) ([]string, error) {
	us, _, err := c.a.Members(ctx, ch, "", false)
	ids := make([]string, 0, len(us))
	for _, u := range us {
		ids = append(ids, u.ID)
	}
	return ids, err
}

func (c coreAPI) commands(ctx context.Context) ([]core.Command, error) {
	return c.a.Commands(ctx, "")
}

func (c coreAPI) command(ctx context.Context, o core.CommandOpts) (core.CommandResult, error) {
	return c.a.Command(ctx, o)
}
