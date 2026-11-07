package slack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReadCreds loads a credential file written by WriteCreds. A file (or its
// directory) that other users can read is tightened to owner-only first.
func ReadCreds(path string) (Creds, error) {
	var c Creds
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	restrictConfigDir(filepath.Dir(path))
	_ = RestrictFile(path)
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("%s: not valid JSON", path) // never echo the contents
	}
	if c.Token == "" {
		return c, fmt.Errorf("no token in %s", path)
	}
	if c.TeamURL == "" {
		c.TeamURL = "https://slack.com/"
	}
	if err := CheckCredsHosts(c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// CheckCredsHosts refuses credentials whose team or enterprise URL isn't
// https on slack.com: every API call sends the token and cookie there.
func CheckCredsHosts(c Creds) error {
	for _, u := range []string{c.TeamURL, c.EnterpriseURL} {
		if u != "" && !slackURL(u, "https") {
			return fmt.Errorf("refusing credentials for %q: not an https slack.com address", Redact(u))
		}
	}
	return nil
}

// WriteCreds saves c to path atomically, owner-only: mode 0600 in a 0700
// directory on Unix, and a protected owner-only ACL on Windows (where Go's
// mode bits mean nothing).
func WriteCreds(path string, c Creds) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return WriteSecretFile(path, b)
}

// WriteSecretFile writes b to path via a temp file in the same directory,
// with owner-only permissions set before any secret byte is written.
func WriteSecretFile(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	restrictConfigDir(dir)
	f, err := os.CreateTemp(dir, ".creds-*.tmp") // 0600
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)  // no-op after the rename
	_ = RestrictFile(tmp) // best effort: e.g. FAT volumes have no ACLs
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	_ = RestrictFile(path)
	return nil
}

// restrictConfigDir tightens Clack's own config directory (named clack or
// yoloslack, or CLACK_HOME/YOLOSLACK_HOME). A -creds path elsewhere (say
// ~/Desktop/creds.json) only gets the file itself restricted.
func restrictConfigDir(dir string) {
	dir = filepath.Clean(dir)
	switch {
	case filepath.Base(dir) == "clack", filepath.Base(dir) == "yoloslack",
		os.Getenv("CLACK_HOME") != "" && filepath.Clean(os.Getenv("CLACK_HOME")) == dir,
		os.Getenv("YOLOSLACK_HOME") != "" && filepath.Clean(os.Getenv("YOLOSLACK_HOME")) == dir:
		_ = RestrictDir(dir)
	}
}
