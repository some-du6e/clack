//go:build perfharness && linux

package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The process's memory as Linux counts it, for the memory workflow's
// driver (perfdrive.go; perfharness builds only): /proc/self/status, and
// /proc/self/smaps summed by owner. Anonymous memory is split into the Go
// heap's arenas (the runtime's first arena hint, 0x00c0<<32, growing up),
// glibc's brk heap, and other anonymous mappings (malloc arenas and big
// mallocs, Mesa/llvmpipe's textures and buffers, Go's off-heap runtime
// structures, thread stacks); file mappings by file.

func perfGoArena(p uint64) bool { return p >= 0xc000000000 && p < 0xc100000000 }

func perfOSMem(w io.Writer) {
	if b, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			for _, k := range []string{"VmRSS", "VmHWM", "RssAnon", "RssFile", "RssShmem", "VmSwap", "VmData", "Threads"} {
				if strings.HasPrefix(l, k+":") {
					fmt.Fprintf(w, "linux_%s %s\n", k, strings.Join(strings.Fields(l[len(k)+1:]), " "))
				}
			}
		}
	}
	f, err := os.Open("/proc/self/smaps")
	if err != nil {
		fmt.Fprintf(w, "os_error smaps: %v\n", err)
		return
	}
	defer f.Close()
	type acc struct{ rss, priv, dirty, anon uint64 }
	cat := map[string]*acc{}
	files := map[string]*acc{}
	var cur, curFile *acc
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		fs := strings.Fields(l)
		if len(fs) == 0 {
			continue
		}
		if !strings.HasSuffix(fs[0], ":") { // a mapping's header line
			rng := strings.SplitN(fs[0], "-", 2)
			start, _ := strconv.ParseUint(rng[0], 16, 64)
			path := ""
			if len(fs) >= 6 {
				path = strings.Join(fs[5:], " ")
			}
			c := "anon_other"
			curFile = nil
			switch {
			case path == "" && perfGoArena(start):
				c = "anon_go_arenas"
			case path == "[heap]":
				c = "anon_brk_heap"
			case path == "[stack]" || strings.HasPrefix(path, "[anon:") || path == "":
				c = "anon_other"
			case strings.HasPrefix(path, "["):
				c = "special"
			case strings.HasPrefix(path, "/SYSV") || strings.HasPrefix(path, "/dev/shm") || strings.Contains(path, "(deleted)"):
				c = "shmem"
			default:
				c = "file"
				name := path
				if i := strings.LastIndex(name, "/"); i >= 0 {
					name = name[i+1:]
				}
				if files[name] == nil {
					files[name] = &acc{}
				}
				curFile = files[name]
			}
			if cat[c] == nil {
				cat[c] = &acc{}
			}
			cur = cat[c]
			continue
		}
		if cur == nil || len(fs) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fs[1], 10, 64)
		switch fs[0] {
		case "Rss:":
			cur.rss += v
			if curFile != nil {
				curFile.rss += v
			}
		case "Private_Clean:", "Private_Dirty:":
			cur.priv += v
			if curFile != nil {
				curFile.priv += v
			}
			if fs[0] == "Private_Dirty:" {
				cur.dirty += v
			}
		case "Anonymous:":
			cur.anon += v
		}
	}
	mb := func(kb uint64) string { return fmt.Sprintf("%.1f MB", float64(kb)/1024) }
	var names []string
	for k := range cat {
		names = append(names, k)
	}
	sort.Strings(names)
	var priv uint64
	for _, k := range names {
		a := cat[k]
		// the counterpart of Windows' private working set: memory only
		// this process holds that no file backs (anonymous and shared
		// memory, and file pages it wrote to); clean pages of the binary
		// and libraries are the file's, as Windows counts its images
		if k == "file" {
			priv += a.dirty
		} else {
			priv += a.priv
		}
		fmt.Fprintf(w, "linux_rss_%s %s\nlinux_private_%s %s\n", k, mb(a.rss), k, mb(a.priv))
	}
	fmt.Fprintf(w, "linux_private_total %s\n", mb(priv))
	if f := cat["file"]; f != nil {
		fmt.Fprintf(w, "linux_dirty_file %s\n", mb(f.dirty))
	}
	type kv struct {
		k string
		v *acc
	}
	var fl []kv
	for k, v := range files {
		fl = append(fl, kv{k, v})
	}
	sort.Slice(fl, func(i, j int) bool { return fl[i].v.rss > fl[j].v.rss })
	for _, x := range fl[:min(8, len(fl))] {
		fmt.Fprintf(w, "linux_file %s rss %s private %s\n", x.k, mb(x.v.rss), mb(x.v.priv))
	}
}
