//go:build perfharness && windows

package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The process's memory as Windows counts it, for the memory workflow's
// driver (perfdrive.go; perfharness builds only). Task Manager's Memory
// column is the private working set: the resident pages no other process
// shares. It is split here by who owns the pages: the Go heap's arenas,
// other private allocations (the C heaps, Direct3D and its driver, Go's
// off-heap runtime structures, thread stacks), and copy-on-write pages of
// DLLs and mapped files. The biggest private allocations outside the Go
// heap and the DLLs with the most private pages are listed by name.

var (
	perfKernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procGetProcMemInfo = perfKernel32.NewProc("K32GetProcessMemoryInfo")
	procQueryWS        = perfKernel32.NewProc("K32QueryWorkingSet")
	procMappedName     = perfKernel32.NewProc("K32GetMappedFileNameW")
)

// processMemoryCountersEx2 is PROCESS_MEMORY_COUNTERS_EX2.
type processMemoryCountersEx2 struct {
	CB, PageFaultCount         uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
	PrivateWorkingSetSize      uintptr
	SharedCommitUsage          uint64
}

const (
	perfMemPrivate = 0x20000
	perfMemMapped  = 0x40000
	perfMemImage   = 0x1000000
	perfPage       = 4096
)

// perfGoArena is the address range of the Go heap's arenas on amd64 and
// arm64 (the runtime's first arena hint, 0x00c0<<32, growing up).
func perfGoArena(p uintptr) bool { return p >= 0xc000000000 && p < 0x1c000000000 }

type perfRegion struct {
	base, size, allocBase uintptr
	state, typ, protect   uint32
}

func perfRegions() []perfRegion {
	var out []perfRegion
	var mbi windows.MemoryBasicInformation
	for p := uintptr(0); p < 0x7ffffffeffff; {
		if windows.VirtualQuery(p, &mbi, unsafe.Sizeof(mbi)) != nil || mbi.RegionSize == 0 {
			break
		}
		if mbi.State == windows.MEM_COMMIT {
			out = append(out, perfRegion{mbi.BaseAddress, mbi.RegionSize, mbi.AllocationBase, mbi.State, mbi.Type, mbi.Protect})
		}
		p = mbi.BaseAddress + mbi.RegionSize
	}
	return out
}

func perfMappedName(p uintptr) string {
	buf := make([]uint16, 1024)
	r, _, _ := procMappedName.Call(uintptr(windows.CurrentProcess()), p, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return "?"
	}
	s := windows.UTF16ToString(buf[:r])
	if i := strings.LastIndexAny(s, `\/`); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// perfWorkingSet lists the working set's pages (PSAPI_WORKING_SET_BLOCK:
// bit 8 shared, bits 12+ the virtual page number).
func perfWorkingSet() ([]uintptr, error) {
	n := 1 << 18
	for tries := 0; tries < 6; tries++ {
		buf := make([]uintptr, n+1)
		r, _, err := procQueryWS.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))*unsafe.Sizeof(buf[0]))
		if r != 0 {
			cnt := int(buf[0])
			return buf[1 : 1+min(cnt, n)], nil
		}
		if err != syscall.Errno(24) { // ERROR_BAD_LENGTH
			return nil, err
		}
		n = int(buf[0]) + 1<<14
	}
	return nil, fmt.Errorf("QueryWorkingSet: the working set kept growing")
}

func perfOSMem(w io.Writer) {
	var pmc processMemoryCountersEx2
	pmc.CB = uint32(unsafe.Sizeof(pmc))
	r, _, err := procGetProcMemInfo.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.CB))
	if r == 0 { // before Windows 10 2004: no PrivateWorkingSetSize
		pmc.CB = uint32(unsafe.Offsetof(pmc.PrivateWorkingSetSize))
		r, _, err = procGetProcMemInfo.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.CB))
	}
	if r == 0 {
		fmt.Fprintf(w, "os_error GetProcessMemoryInfo: %v\n", err)
		return
	}
	mb := func(b uintptr) string { return fmt.Sprintf("%.1f MB", float64(b)/(1<<20)) }
	fmt.Fprintf(w, "win_working_set %s\nwin_private_working_set %s\nwin_peak_working_set %s\nwin_commit %s\nwin_peak_commit %s\n",
		mb(pmc.WorkingSetSize), mb(pmc.PrivateWorkingSetSize), mb(pmc.PeakWorkingSetSize), mb(pmc.PrivateUsage), mb(pmc.PeakPagefileUsage))

	regs := perfRegions()
	sort.Slice(regs, func(i, j int) bool { return regs[i].base < regs[j].base })
	// commit by owner (committed MEM_PRIVATE is what counts against the
	// commit charge; image and mapped pages only when copied on write)
	var commitGo, commitOther uintptr
	for _, g := range regs {
		if g.typ != perfMemPrivate {
			continue
		}
		if perfGoArena(g.base) {
			commitGo += g.size
		} else {
			commitOther += g.size
		}
	}
	fmt.Fprintf(w, "win_commit_go_arenas %s\nwin_commit_other_private %s\n", mb(commitGo), mb(commitOther))

	pages, err := perfWorkingSet()
	if err != nil {
		fmt.Fprintf(w, "os_error QueryWorkingSet: %v\n", err)
		return
	}
	find := func(p uintptr) *perfRegion {
		i := sort.Search(len(regs), func(i int) bool { return regs[i].base+regs[i].size > p })
		if i < len(regs) && regs[i].base <= p {
			return &regs[i]
		}
		return nil
	}
	var goPriv, otherPriv, imagePriv, mappedPriv, unknownPriv, shared uintptr
	byAlloc := map[uintptr]uintptr{}
	byImage := map[uintptr]uintptr{}
	for _, b := range pages {
		p := b &^ (perfPage - 1)
		if b&(1<<8) != 0 {
			shared += perfPage
			continue
		}
		g := find(p)
		switch {
		case g == nil:
			unknownPriv += perfPage
		case g.typ == perfMemImage:
			imagePriv += perfPage
			byImage[g.allocBase] += perfPage
		case g.typ == perfMemMapped:
			mappedPriv += perfPage
		case perfGoArena(p):
			goPriv += perfPage
		default:
			otherPriv += perfPage
			byAlloc[g.allocBase] += perfPage
		}
	}
	fmt.Fprintf(w, "win_ws_private_go_arenas %s\nwin_ws_private_other %s\nwin_ws_private_images %s\nwin_ws_private_mapped %s\nwin_ws_private_unknown %s\nwin_ws_shared %s\n",
		mb(goPriv), mb(otherPriv), mb(imagePriv), mb(mappedPriv), mb(unknownPriv), mb(shared))
	type kv struct {
		k, v uintptr
	}
	top := func(m map[uintptr]uintptr, n int) []kv {
		var s []kv
		for k, v := range m {
			s = append(s, kv{k, v})
		}
		sort.Slice(s, func(i, j int) bool { return s[i].v > s[j].v })
		return s[:min(n, len(s))]
	}
	// protections tell code (wazero's compiled wasm, shader JIT:
	// PAGE_EXECUTE_*) from data
	prot := func(p uint32) string {
		switch p &^ (windows.PAGE_GUARD | windows.PAGE_NOCACHE | windows.PAGE_WRITECOMBINE) {
		case windows.PAGE_READONLY:
			return "r"
		case windows.PAGE_READWRITE:
			return "rw"
		case windows.PAGE_EXECUTE_READ:
			return "rx"
		case windows.PAGE_EXECUTE_READWRITE:
			return "rwx"
		case windows.PAGE_EXECUTE:
			return "x"
		}
		return fmt.Sprintf("0x%x", p)
	}
	var execRes uintptr
	for k, v := range byAlloc {
		for _, g := range regs {
			if g.allocBase == k && g.protect&(windows.PAGE_EXECUTE|windows.PAGE_EXECUTE_READ|windows.PAGE_EXECUTE_READWRITE) != 0 {
				execRes += v
				break
			}
		}
	}
	fmt.Fprintf(w, "win_ws_private_other_exec %s\n", mb(execRes))
	for _, x := range top(byAlloc, 15) {
		var size uintptr
		ps := map[string]uintptr{}
		for _, g := range regs {
			if g.allocBase == x.k {
				size += g.size
				ps[prot(g.protect)] += g.size
			}
		}
		var parts []string
		for p, n := range ps {
			parts = append(parts, p+" "+mb(n))
		}
		sort.Strings(parts)
		fmt.Fprintf(w, "win_private_alloc 0x%x resident %s committed %s (%s)\n", x.k, mb(x.v), mb(size), strings.Join(parts, ", "))
	}
	for _, x := range top(byImage, 10) {
		fmt.Fprintf(w, "win_private_image %s %s\n", perfMappedName(x.k), mb(x.v))
	}
}
