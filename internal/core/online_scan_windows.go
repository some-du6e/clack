//go:build windows

package core

import (
	"errors"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// scanInterfaces is netScan: online while an adapter that is up, not
// loopback, has a unicast address countsOnline counts, and the
// fingerprint of every such address. Go's net.Interfaces and each
// Interface.Addrs call GetAdaptersAddresses for every adapter with their
// gateways and prefixes, so a walk cost one call per interface that is
// up, plus one; this is a single call, which skips what neither reads
// (anycast, multicast and DNS server addresses, friendly names).
func scanInterfaces() (online bool, fingerprint string) {
	const flags = windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_SKIP_DNS_SERVER | windows.GAA_FLAG_SKIP_FRIENDLY_NAME
	size := uint32(15000) // the recommended first size
	var buf []byte
	for tries := 1; ; tries++ {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if errors.Is(err, windows.ERROR_NO_DATA) {
			return false, "" // no adapter at all
		}
		// the list can grow between the size it asks for and the read
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) || size <= uint32(len(buf)) || tries == 4 {
			return true, "" // nothing to go on: don't claim offline
		}
	}
	if size == 0 {
		return false, ""
	}
	var out []string
	for aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); aa != nil; aa = aa.Next {
		if aa.OperStatus != windows.IfOperStatusUp || aa.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
			continue
		}
		idx := aa.IfIndex
		if idx == 0 {
			idx = aa.Ipv6IfIndex // Go's net: an IPv6-only adapter's index
		}
		for u := aa.FirstUnicastAddress; u != nil; u = u.Next {
			if ip := u.Address.IP(); countsOnline(ip) {
				out = append(out, strconv.FormatUint(uint64(idx), 10)+"="+ip.String())
			}
		}
	}
	return len(out) > 0, fingerprintOf(out)
}
