package taut

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
)

// configFile is config.json, with Taut's semantics: {"plugins": {id: block}}
// where a block holds only the options someone set. Defaults are filled in
// when the plugin's config is resolved (in JS, with its checks), secrets
// never go here (secrets.json), and unknown root keys are kept as they are.
type configFile struct {
	path string
	mu   sync.Mutex
	root map[string]json.RawMessage
	// plugins is root["plugins"] decoded, kept in sync on every edit.
	plugins map[string]map[string]json.RawMessage
	err     error
}

func loadConfig(path string) (*configFile, error) {
	c := &configFile{path: path, root: map[string]json.RawMessage{}, plugins: map[string]map[string]json.RawMessage{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		c.err = err
		return c, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c.root); err != nil {
		c.err = fmt.Errorf("config.json isn't JSON: %w", err)
		c.root = map[string]json.RawMessage{}
		return c, c.err
	}
	if raw, ok := c.root["plugins"]; ok {
		if err := json.Unmarshal(raw, &c.plugins); err != nil {
			c.err = errors.New(`"plugins" must be an object`)
			c.plugins = map[string]map[string]json.RawMessage{}
			return c, c.err
		}
	}
	if c.plugins == nil {
		c.plugins = map[string]map[string]json.RawMessage{}
	}
	return c, nil
}

// block returns a copy of the saved block for id (nil when there is none).
func (c *configFile) block(id string) map[string]json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.plugins[id]
	if !ok {
		return nil
	}
	out := make(map[string]json.RawMessage, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}

// edit applies fn to id's block (created if missing) and writes the file
// when something changed. An emptied block is removed.
func (c *configFile) edit(id string, fn func(b map[string]json.RawMessage)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return fmt.Errorf("config.json can't be written until it's fixed: %w", c.err)
	}
	before, _ := json.Marshal(c.plugins[id])
	b := c.plugins[id]
	if b == nil {
		b = map[string]json.RawMessage{}
	}
	fn(b)
	if len(b) == 0 {
		delete(c.plugins, id)
	} else {
		c.plugins[id] = b
	}
	after, _ := json.Marshal(c.plugins[id])
	if bytes.Equal(before, after) {
		return nil
	}
	return c.save()
}

func (c *configFile) remove(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.plugins[id]; !ok {
		return nil
	}
	delete(c.plugins, id)
	return c.save()
}

// save writes the file the way Taut does: indented JSON plus a newline.
func (c *configFile) save() error {
	raw, err := json.Marshal(c.plugins)
	if err != nil {
		return err
	}
	c.root["plugins"] = raw
	out, err := json.MarshalIndent(c.root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return writeFileAtomic(c.path, out, 0o600)
}

// jsonEqual compares two JSON values semantically.
func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(x, y)
}

// ---- metadata cache: settings and boot work without starting JS ----

type metaCache struct {
	Hash string `json:"hash"`
	Meta *Meta  `json:"meta"`
}

func metaPath(dir, id string) string { return filepath.Join(dir, "plugins", id+".meta.json") }

func readMeta(dir, id, hash string) *Meta {
	b, err := os.ReadFile(metaPath(dir, id))
	if err != nil {
		return nil
	}
	var mc metaCache
	if json.Unmarshal(b, &mc) != nil || mc.Hash != hash || mc.Meta == nil || mc.Meta.ID != id {
		return nil
	}
	return mc.Meta
}

func writeMeta(dir, id, hash string, m *Meta) error {
	b, err := json.MarshalIndent(metaCache{Hash: hash, Meta: m}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(metaPath(dir, id), b, 0o600)
}
