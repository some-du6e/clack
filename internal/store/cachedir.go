package store

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The account's cache directory: per workspace and per user, versioned,
// under the OS's user cache folder (cmd/clack's cacheRoot):
//
//	<cache root>/v1/<team>-<user>/
//	  meta.json       the user directory, custom emoji, usergroups, prefs (SaveCache)
//	  boot_data.json  the sidebar a warm boot renders first (bootdata.go)
//	  headers.json    recently opened channels' tab bars and facepiles (bootdata.go)
//	  img/            avatars and emoji (imgcache)
//
// Every file is 0600 and every directory 0700; nothing in it is message
// content. The whole directory goes on sign-out (DropDisk). Older vN
// directories are purged (PurgeOldVersions), as Slack drops a store older
// than its minimum data version (isStoreOutOfDate, "store_out_of_date").
// alpha.28 also kept a boot snapshot here (boot.json, and the last
// conversation's messages in history.json); RemoveBootFiles deletes what
// it left. boot_data.json is its sidebar half without any message.

// CacheVersion is the disk layout version: the vN directory and every
// file's "v". Bump it when a change makes old snapshots wrong.
const CacheVersion = 1

// AccountDir is the cache directory of one user in one workspace under
// root. team and user are Slack ids; anything else is reduced to letters
// and digits so it can't leave root.
func AccountDir(root, team, user string) string {
	return filepath.Join(root, "v"+strconv.Itoa(CacheVersion), safeID(team)+"-"+safeID(user))
}

func safeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "none"
	}
	return b.String()
}

// PurgeOldVersions removes root's vN directories other than the current
// version's. Slow on a big image cache: call it off the UI goroutine.
func PurgeOldVersions(root string) {
	if root == "" {
		return
	}
	es, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cur := "v" + strconv.Itoa(CacheVersion)
	for _, e := range es {
		n := e.Name()
		if !e.IsDir() || n == cur || len(n) < 2 || n[0] != 'v' {
			continue
		}
		if _, err := strconv.Atoi(n[1:]); err != nil {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, n))
	}
}

// RemoveBootFiles deletes alpha.28's boot snapshot from dir (boot.json and
// history.json, the only files that held message text). Disk I/O: call it
// off the UI goroutine.
func RemoveBootFiles(dir string) {
	if dir == "" {
		return
	}
	for _, n := range []string{"boot.json", "history.json"} {
		_ = os.Remove(filepath.Join(dir, n))
	}
}

func writeAtomic(dir, name string, b []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+name+"-*.tmp") // 0600
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, name))
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// CacheDir is the account's cache directory ("" without a disk cache).
func (s *Store) CacheDir() string { return s.cacheDir }

// cacheFiles are the files DropDisk removes from the account directory.
var cacheFiles = []string{"meta.json", "meta.json.tmp", "sidebar.json", "boot.json", "history.json",
	BootDataFile, HeadersFile, // the warm boot's (bootdata.go)
	"code_channel_reopen.json", "code_channel_reopen.json.tmp", // core's codeChannelReopen (codechannel_close.go)
	"channel_managers.json"} // core's channel managers store (channel_managers.go)

// StopSaving makes every later save a no-op (sign-out), at once and
// without touching the disk; DropDisk then deletes the files.
func (s *Store) StopSaving() { s.dropped.Store(true) }

// DropDisk deletes the account's disk cache (sign-out) and stops every
// later save from writing it again. It removes only Clack's own files and
// the img directory, then the directory itself if that left it empty.
// It touches the disk: call it off the UI goroutine.
func (s *Store) DropDisk() {
	s.dropped.Store(true)
	s.saveMu.Lock() // waits out a save already writing
	defer s.saveMu.Unlock()
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	if s.cacheDir != "" {
		dropAccountDir(s.cacheDir)
	}
}

// dropAccountDir deletes Clack's files in an account directory, the img
// directory, and last the signed-out mark (so a start after an
// interrupted sign-out finishes it: SweepAccounts), then the directory
// itself when that left it empty.
func dropAccountDir(dir string) {
	removeAccountFiles(dir)
	_ = os.RemoveAll(filepath.Join(dir, "img"))
	_ = os.Remove(filepath.Join(dir, SignedOutMark))
	_ = os.Remove(dir) // only when empty
}

// removeAccountFiles deletes Clack's own files in an account directory
// (cacheFiles and their temp files), not the images or the mark.
func removeAccountFiles(dir string) {
	for _, n := range cacheFiles {
		_ = os.Remove(filepath.Join(dir, n))
	}
	if es, err := os.ReadDir(dir); err == nil {
		for _, e := range es {
			if n := e.Name(); strings.HasSuffix(n, ".tmp") && strings.HasPrefix(n, ".") {
				_ = os.Remove(filepath.Join(dir, n))
			}
		}
	}
}

// SignedOutMark is the empty file sign-out leaves in the account
// directory before it deletes anything else, removed last (dropAccountDir):
// a start that finds it finishes the sign-out (SweepAccounts).
const SignedOutMark = ".signed-out"

// SignOutNow is what a sign-out does to the disk before anything else, at
// once: no save writes again (StopSaving), the warm boot's files go (they
// are small; meta.json and the images follow in DropDisk, which waits for
// a save still writing), and the signed-out mark is left so the next
// start finishes the job if this process ends first. Disk I/O, but only a
// few small files: the UI calls it as it signs out.
func (s *Store) SignOutNow() {
	s.dropped.Store(true)
	if s.cacheDir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(s.cacheDir, SignedOutMark), nil, 0o600)
	s.RemoveWarmFiles()
}

// RemoveWarmFiles deletes the warm boot's files (boot_data.json,
// headers.json), after any write of them still going, and no write of
// them follows (a save already past its own checks included). A session
// that ended (auth_failed) drops them: they only serve this session's
// next warm start.
func (s *Store) RemoveWarmFiles() {
	if s.cacheDir == "" {
		return
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	s.warmGone = true
	for _, n := range []string{BootDataFile, HeadersFile} {
		_ = os.Remove(filepath.Join(s.cacheDir, n))
	}
}

// ClearSignedOut takes the signed-out mark off the directory: a signed-in
// session opened it again (the same account signed back in), so it is
// that session's now, whatever an earlier sign-out left unfinished.
// cmd/clack calls it as the session's store opens.
func (s *Store) ClearSignedOut() {
	if s.cacheDir == "" {
		return
	}
	_ = os.Remove(filepath.Join(s.cacheDir, SignedOutMark))
}

// SweepAccounts runs at start, before any account's store opens. An
// account directory with the signed-out mark is deleted: a sign-out the
// process didn't live to finish. keep (the signed-in account's directory,
// "" when none is) is never deleted: its mark only means a sign-out was
// followed by a sign-in to the same account, so the mark goes instead.
// Every other directory stays whole, warm boot files included, as Slack
// keeps a persisted store per signed-in team (the account switcher's
// saved accounts sign in again warm). The images of a deleted directory
// are removed in the background. A few stats and removes: call it before
// the window opens, off the UI goroutine.
func SweepAccounts(root, keep string) {
	if root == "" {
		return
	}
	base := filepath.Join(root, "v"+strconv.Itoa(CacheVersion))
	es, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range es {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if strings.HasSuffix(e.Name(), ".trash") { // an earlier sweep's images, cut short
			go func(p string) { _ = os.RemoveAll(p) }(dir)
			continue
		}
		mark := filepath.Join(dir, SignedOutMark)
		if _, err := os.Stat(mark); err != nil {
			continue
		}
		if dir == keep {
			_ = os.Remove(mark)
			continue
		}
		// the images can be hundreds of MB: out of the way at once and
		// deleted in the background; when the rename fails (a file in
		// them held open), deleted in the background where they are,
		// the mark and the directory after them
		img := filepath.Join(dir, "img")
		trash := filepath.Join(base, "."+e.Name()+"-img-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".trash")
		if err := os.Rename(img, trash); err == nil || os.IsNotExist(err) {
			if err == nil {
				go func() { _ = os.RemoveAll(trash) }()
			}
			dropAccountDir(dir)
			continue
		}
		removeAccountFiles(dir)
		go func() {
			_ = os.RemoveAll(img)
			_ = os.Remove(mark)
			_ = os.Remove(dir) // only when empty
		}()
	}
}

// Dropped reports whether DropDisk ran.
func (s *Store) Dropped() bool { return s.dropped.Load() }
