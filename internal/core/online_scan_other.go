//go:build !windows

package core

import "net"

// scanInterfaces is netScan: online while an interface that is up and
// running, not loopback, has an address countsOnline counts, and the
// fingerprint of every such address. One net.Interfaces and one Addrs per
// interface that is up (a netlink dump each on Linux, a routing sysctl
// each on macOS).
func scanInterfaces() (online bool, fingerprint string) {
	ifs, err := net.Interfaces()
	if err != nil {
		return true, "" // nothing to go on: don't claim offline
	}
	var out []string
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagRunning == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, ad := range addrs {
			var ip net.IP
			switch v := ad.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if countsOnline(ip) {
				out = append(out, ifc.Name+"="+ip.String())
			}
		}
	}
	return len(out) > 0, fingerprintOf(out)
}
