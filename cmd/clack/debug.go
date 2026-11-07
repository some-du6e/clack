package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

// startProfiler serves runtime profiles on a loopback address when
// CLACK_PPROF is set (e.g. CLACK_PPROF=127.0.0.1:6061):
//
//	/heap /allocs /goroutine   pprof profiles (?gc=1 runs a GC first)
//	/profile?seconds=10        CPU profile
//	/mem                       runtime.MemStats and /proc RSS, as text
//	/freeos                    debug.FreeOSMemory, then /mem
//
// CLACK_CPUPROFILE=<file> profiles the first CLACK_CPUPROFILE_SECS
// seconds (default 5) of the process: main to first frame and boot.
//
// It uses runtime/pprof directly (net/http/pprof would link html/template).
func startProfiler() {
	startupProfile()
	addr := os.Getenv("CLACK_PPROF")
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		log.Printf("pprof: %q is not a loopback address; not serving", addr)
		return
	}
	mux := http.NewServeMux()
	for _, name := range []string{"heap", "allocs", "goroutine", "block", "mutex", "threadcreate"} {
		name := name
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("gc") != "" {
				runtime.GC()
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_ = pprof.Lookup(name).WriteTo(w, 0)
		})
	}
	mux.HandleFunc("/profile", func(w http.ResponseWriter, r *http.Request) {
		secs, _ := strconv.Atoi(r.URL.Query().Get("seconds"))
		if secs <= 0 {
			secs = 10
		}
		if err := pprof.StartCPUProfile(w); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		time.Sleep(time.Duration(secs) * time.Second)
		pprof.StopCPUProfile()
	})
	mux.HandleFunc("/mem", func(w http.ResponseWriter, r *http.Request) { writeMem(w) })
	mux.HandleFunc("/freeos", func(w http.ResponseWriter, r *http.Request) {
		debug.FreeOSMemory()
		writeMem(w)
	})
	go func() {
		log.Printf("pprof: serving on http://%s", addr)
		log.Printf("pprof: %v", http.ListenAndServe(addr, mux))
	}()
}

func writeMem(w http.ResponseWriter) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mb := func(b uint64) string { return fmt.Sprintf("%.1f MB", float64(b)/(1<<20)) }
	fmt.Fprintf(w, "heap_alloc %s\nheap_inuse %s\nheap_idle %s\nheap_released %s\nheap_sys %s\nstack_inuse %s\nsys %s\nnum_gc %d\nheap_objects %d\ngoroutines %d\n",
		mb(m.HeapAlloc), mb(m.HeapInuse), mb(m.HeapIdle), mb(m.HeapReleased), mb(m.HeapSys), mb(m.StackInuse), mb(m.Sys),
		m.NumGC, m.HeapObjects, runtime.NumGoroutine())
	if b, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "Vm") || strings.HasPrefix(l, "Rss") {
				fmt.Fprintln(w, l)
			}
		}
	}
}

func startupProfile() {
	path := os.Getenv("CLACK_CPUPROFILE")
	if path == "" {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		log.Printf("cpuprofile: %v", err)
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		log.Printf("cpuprofile: %v", err)
		f.Close()
		return
	}
	secs, _ := strconv.Atoi(os.Getenv("CLACK_CPUPROFILE_SECS"))
	if secs <= 0 {
		secs = 5
	}
	time.AfterFunc(time.Duration(secs)*time.Second, func() {
		pprof.StopCPUProfile()
		f.Close()
		log.Printf("cpuprofile: wrote %s", path)
	})
}
