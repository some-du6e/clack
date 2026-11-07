package slick

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Plugins that keep message contents (Slick's MessageLogger keeps deleted
// and edited messages, with image previews, in api.storage). Hack Club's
// scraping policy (https://news.hackclub.com/news/scraping-use-policy/)
// doesn't allow storing other people's messages, so Clack keeps these
// plugins' storage in memory: it lasts until Clack quits. The user can turn
// on "Keep on disk" for one of them after reading StoresMessagesNote; then
// it is stored like every other plugin's data. Clack bundles none of them
// and enables none by default.
var storesMessages = map[string]bool{
	"MessageLogger": true,
}

// StoresMessagesNote is the warning Preferences > Slick shows for these
// plugins, before they are turned on and next to "Keep on disk".
const StoresMessagesNote = "This plugin saves message contents, including deleted and edited messages. Hack Club's scraping policy forbids storing other people's messages, so Clack keeps what it saves in memory only, and it's gone when Clack quits. Only keep it on disk if you understand that."

const keepOnDiskPref = "keepOnDisk:"

// memStore is the in-memory blob store for message-keeping plugins.
type memStore struct {
	mu sync.Mutex
	ns map[string]map[string]string
}

func pluginOfNamespace(ns string) string {
	return strings.TrimSuffix(strings.TrimPrefix(ns, "plugin:"), ":secrets")
}

// memoryOnly: ns belongs to a message-keeping plugin the user hasn't
// allowed to keep data on disk. Call with s.mem.mu held.
func (s *Slick) memoryOnlyLocked(ns string) bool {
	id := pluginOfNamespace(ns)
	if !storesMessages[id] {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prefs[keepOnDiskPref+id] != "on"
}

// blob runs one BlobStore operation on namespace ns (plugin:<id>[:secrets]).
func (s *Slick) blob(op, ns, key, value string) (any, error) {
	s.mem.mu.Lock()
	defer s.mem.mu.Unlock()
	if s.memoryOnlyLocked(ns) {
		return s.mem.op(op, ns, key, value)
	}
	dns := dataNS(ns)
	switch op {
	case "list":
		return s.t.DataList(dns), nil
	case "read":
		if v, ok := s.t.DataRead(dns, key); ok {
			return v, nil
		}
		return nil, nil
	case "readAll":
		out := map[string]string{}
		for _, k := range s.t.DataList(dns) {
			if strings.HasPrefix(k, key) {
				if v, ok := s.t.DataRead(dns, k); ok {
					out[k] = v
				}
			}
		}
		return out, nil
	case "write":
		s.t.DataWrite(dns, key, value)
		return true, nil
	case "delete":
		s.t.DataDelete(dns, key)
		return true, nil
	case "clear":
		s.t.DataClear(dns)
		return true, nil
	}
	return nil, fmt.Errorf("slick: blob %s", op)
}

// op is a BlobStore operation on the memory store. Call with m.mu held.
func (m *memStore) op(op, ns, key, value string) (any, error) {
	if m.ns == nil {
		m.ns = map[string]map[string]string{}
	}
	data := m.ns[ns]
	switch op {
	case "list":
		out := make([]string, 0, len(data))
		for k := range data {
			out = append(out, k)
		}
		return out, nil
	case "read":
		if v, ok := data[key]; ok {
			return v, nil
		}
		return nil, nil
	case "readAll":
		out := map[string]string{}
		for k, v := range data {
			if strings.HasPrefix(k, key) {
				out[k] = v
			}
		}
		return out, nil
	case "write":
		if data == nil {
			data = map[string]string{}
			m.ns[ns] = data
		}
		data[key] = value
		return true, nil
	case "delete":
		delete(data, key)
		return true, nil
	case "clear":
		delete(m.ns, ns)
		return true, nil
	}
	return nil, fmt.Errorf("slick: blob %s", op)
}

// clearData deletes a plugin's data namespace(s) wherever they are.
func (s *Slick) clearData(id string, secrets bool) {
	s.mem.mu.Lock()
	defer s.mem.mu.Unlock()
	nss := []string{"plugin:" + id}
	if secrets {
		nss = append(nss, "plugin:"+id+":secrets")
	}
	for _, ns := range nss {
		delete(s.mem.ns, ns)
		s.t.DataClear(dataNS(ns))
	}
}

// KeepOnDisk reports whether a message-keeping plugin may store its data
// on disk (false for every other plugin: they always do).
func (s *Slick) KeepOnDisk(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return storesMessages[id] && s.prefs[keepOnDiskPref+id] == "on"
}

// SetKeepOnDisk lets a plugin that keeps message contents store them on
// disk (on), or moves them back to memory and deletes them from disk
// (off). What it has now carries over either way.
func (s *Slick) SetKeepOnDisk(ctx context.Context, id string, on bool) error {
	if !storesMessages[id] {
		return fmt.Errorf("slick: %s doesn't keep messages", id)
	}
	s.mem.mu.Lock()
	for _, ns := range []string{"plugin:" + id, "plugin:" + id + ":secrets"} {
		dns := dataNS(ns)
		if on {
			for k, v := range s.mem.ns[ns] {
				s.t.DataWrite(dns, k, v)
			}
			delete(s.mem.ns, ns)
			continue
		}
		keys := s.t.DataList(dns)
		if len(keys) > 0 {
			if s.mem.ns == nil {
				s.mem.ns = map[string]map[string]string{}
			}
			if s.mem.ns[ns] == nil {
				s.mem.ns[ns] = map[string]string{}
			}
			for _, k := range keys {
				if v, ok := s.t.DataRead(dns, k); ok {
					s.mem.ns[ns][k] = v
				}
			}
		}
		s.t.DataClear(dns)
	}
	s.mu.Lock()
	if on {
		s.prefs[keepOnDiskPref+id] = "on"
	} else {
		delete(s.prefs, keepOnDiskPref+id)
	}
	err := writeJSON(s.path("plugins", "prefs.json"), s.prefs)
	s.mu.Unlock()
	s.mem.mu.Unlock()
	s.changed()
	return err
}
