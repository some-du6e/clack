package fakeslack

// The Files sidebar's file collections (custom_file_sections):
// files.collections.list / create / update / delete, and
// files.favorites.add / remove with a collection_id (moving a file into a
// section, or out of one). The starred collection (StarredCollectionID)
// is the same set files.favorites.* without a collection_id changes, so
// both experiment groups see one list. A file sits in at most one
// collection: adding it to one takes it out of the others. Every write is
// recorded with the Files view's (FilesViewCalls). Also lists.getMyItems,
// the list items assigned to the viewer (todo_assignee cells naming
// SelfID), for the sidebar's "Assigned to you".

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// StarredCollectionID is the starred file collection's id.
const StarredCollectionID = "FC0STARRED01"

type fakeCollection struct {
	ID, Name, Emoji, Type, Position, Sort string
	Collapsed                             bool
	Files                                 []fakeCollFile // newest first by position
}

type fakeCollFile struct{ ID, Position string }

// starPositions hands out the starred collection's positions in
// files.favorites.add order.
func (fv *filesViewStore) starPosLocked(id string) string {
	if fv.starPos == nil {
		fv.starPos = map[string]string{}
	}
	if p, ok := fv.starPos[id]; ok {
		return p
	}
	fv.nextPos++
	p := fmt.Sprintf("%010d", 5000000000+fv.nextPos)
	fv.starPos[id] = p
	return p
}

// AddFileCollection adds a custom collection (tests and shots) with
// files, the first at the top; it returns the collection's id.
func (s *Server) AddFileCollection(name, emoji string, fileIDs ...string) string {
	fv := s.filesView()
	fv.mu.Lock()
	defer fv.mu.Unlock()
	return fv.addCollectionLocked(name, emoji, fileIDs...)
}

func (fv *filesViewStore) addCollectionLocked(name, emoji string, fileIDs ...string) string {
	fv.nextColl++
	c := &fakeCollection{ID: fmt.Sprintf("FC0SECT%05d", fv.nextColl), Name: name, Emoji: emoji, Type: "custom",
		Position: fmt.Sprintf("%010d", 5000000000+fv.nextColl)}
	fv.colls = append(fv.colls, c)
	for i := len(fileIDs) - 1; i >= 0; i-- {
		fv.moveLocked(fileIDs[i], c.ID)
	}
	return c.ID
}

// FileCollections returns the custom collections' names and their files
// (tests), in the store's order.
func (s *Server) FileCollections() map[string][]string {
	fv := s.filesView()
	fv.mu.Lock()
	defer fv.mu.Unlock()
	out := map[string][]string{}
	for _, c := range fv.colls {
		var ids []string
		for _, f := range c.Files {
			ids = append(ids, f.ID)
		}
		out[c.Name] = ids
	}
	return out
}

func (fv *filesViewStore) collLocked(id string) *fakeCollection {
	for _, c := range fv.colls {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// unstarLocked drops id from the starred set.
func (fv *filesViewStore) unstarLocked(id string) bool {
	for i, x := range fv.starred {
		if x == id {
			fv.starred = append(fv.starred[:i:i], fv.starred[i+1:]...)
			return true
		}
	}
	return false
}

// moveLocked puts file id in collection to (the starred one or a custom
// one), out of every other.
func (fv *filesViewStore) moveLocked(id, to string) {
	if to != StarredCollectionID {
		fv.unstarLocked(id)
	}
	for _, c := range fv.colls {
		out := c.Files[:0:0]
		for _, f := range c.Files {
			if f.ID != id {
				out = append(out, f)
			}
		}
		c.Files = out
	}
	if to == StarredCollectionID {
		for _, x := range fv.starred {
			if x == id {
				return
			}
		}
		fv.starred = append(fv.starred, id)
		fv.starPosLocked(id)
		return
	}
	if c := fv.collLocked(to); c != nil {
		top := "5000000000"
		if len(c.Files) > 0 {
			n, _ := strconv.ParseInt(strings.SplitN(c.Files[0].Position, ".", 2)[0], 10, 64)
			top = fmt.Sprintf("%010d", n+1)
		}
		c.Files = append([]fakeCollFile{{ID: id, Position: top}}, c.Files...)
	}
}

func (fv *filesViewStore) collJSON(c *fakeCollection) map[string]any {
	files := []map[string]any{}
	for _, f := range c.Files {
		files = append(files, map[string]any{"id": f.ID, "position": f.Position})
	}
	m := map[string]any{"id": c.ID, "name": c.Name, "emoji": c.Emoji, "type": c.Type, "position": c.Position,
		"is_collapsed": c.Collapsed, "files": files}
	if c.Sort != "" {
		m["sort"] = c.Sort
	}
	return m
}

func (fv *filesViewStore) starredJSONLocked() map[string]any {
	files := []map[string]any{}
	for i := len(fv.starred) - 1; i >= 0; i-- {
		id := fv.starred[i]
		files = append(files, map[string]any{"id": id, "position": fv.starPosLocked(id)})
	}
	m := map[string]any{"id": StarredCollectionID, "name": "Starred", "emoji": "", "type": "starred",
		"position": "9000000000", "is_collapsed": false, "files": files}
	if fv.starSort != "" {
		m["sort"] = fv.starSort
	}
	return m
}

// handleFileSections answers files.collections.* and the favorites calls
// that name a collection; handled=false passes on. fv.mu is held.
func (s *Server) handleFileSections(fv *filesViewStore, method string, f url.Values) (any, bool) {
	switch method {
	case "files.collections.list":
		out := []map[string]any{fv.starredJSONLocked()}
		for _, c := range fv.colls {
			out = append(out, fv.collJSON(c))
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i]["position"].(string) > out[j]["position"].(string) })
		return ok("collections", out), true
	case "files.collections.create":
		name := strings.TrimSpace(f.Get("name"))
		if name == "" {
			return fail("invalid_name"), true
		}
		fv.calls = append(fv.calls, FilesViewCall{Method: method, Args: f})
		id := fv.addCollectionLocked(name, f.Get("emoji"))
		return ok("collections", []map[string]any{fv.collJSON(fv.collLocked(id))}), true
	case "files.collections.update":
		fv.calls = append(fv.calls, FilesViewCall{Method: method, Args: f})
		id := f.Get("collection")
		if id == StarredCollectionID {
			if v := f.Get("sort"); v != "" {
				fv.starSort = v
			}
			return ok("collection", fv.starredJSONLocked()), true
		}
		c := fv.collLocked(id)
		if c == nil {
			return fail("collection_not_found"), true
		}
		if _, set := f["name"]; set {
			if strings.TrimSpace(f.Get("name")) == "" {
				return fail("invalid_name"), true
			}
			c.Name = f.Get("name")
		}
		if _, set := f["emoji"]; set {
			c.Emoji = f.Get("emoji")
		}
		if v := f.Get("sort"); v != "" {
			c.Sort = v
		}
		if v := f.Get("is_collapsed"); v != "" {
			c.Collapsed = v == "true" || v == "1"
		}
		return ok("collection", fv.collJSON(c)), true
	case "files.collections.delete":
		fv.calls = append(fv.calls, FilesViewCall{Method: method, Args: f})
		id := f.Get("collection")
		for i, c := range fv.colls {
			if c.ID == id {
				fv.colls = append(fv.colls[:i:i], fv.colls[i+1:]...)
				return ok(), true
			}
		}
		return fail("collection_not_found"), true
	case "files.favorites.add", "files.favorites.remove":
		to := f.Get("collection_id")
		if to == "" {
			return nil, false
		}
		id := f.Get("file_id")
		if id == "" {
			return fail("invalid_arguments"), true
		}
		fv.calls = append(fv.calls, FilesViewCall{Method: method, Args: f})
		if to != StarredCollectionID && fv.collLocked(to) == nil {
			return fail("collection_not_found"), true
		}
		if method == "files.favorites.add" {
			fv.moveLocked(id, to)
			return ok(), true
		}
		if to == StarredCollectionID {
			if !fv.unstarLocked(id) {
				return fail("not_starred"), true
			}
			return ok(), true
		}
		c := fv.collLocked(to)
		for i, x := range c.Files {
			if x.ID == id {
				c.Files = append(c.Files[:i:i], c.Files[i+1:]...)
				return ok(), true
			}
		}
		return fail("not_in_collection"), true
	}
	return nil, false
}

// handleMyItems answers lists.getMyItems: every record with a
// todo_assignee cell naming the viewer, and those records' lists.
func (s *Server) handleMyItems(f url.Values) any {
	ls := s.listStore()
	ls.mu.Lock()
	defer ls.mu.Unlock()
	listsOut := []map[string]any{}
	records := []any{}
	for _, id := range ls.order {
		l := ls.byID[id]
		mine := false
		for _, r := range l.Records {
			if r.Archived {
				continue
			}
			for _, fl := range r.Fields {
				if fl.Key != "todo_assignee" {
					continue
				}
				for _, u := range fl.User {
					if u == SelfID {
						mine = true
						rr := r
						rr.ListID = l.ID
						records = append(records, rr)
					}
				}
			}
		}
		if mine {
			listsOut = append(listsOut, s.listFile(l))
		}
	}
	return ok("lists", listsOut, "records", records)
}
