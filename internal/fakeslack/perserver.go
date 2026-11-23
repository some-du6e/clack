package fakeslack

import "sync"

// perServer is one of the package's maps keyed by *Server: a Server's
// state kept beside it (the package keeps about thirty). Each is made by
// newPerServer, so Close drops a closed Server from all of them. An entry
// keeps its Server reachable, histories and all: Close used to delete six
// of them by hand, and the rest kept every test's fake server in memory
// to the end of its test binary (ci-speed-3).
type perServer struct{ sync.Map }

var perServerMaps []*perServer

func newPerServer() *perServer {
	m := &perServer{}
	perServerMaps = append(perServerMaps, m)
	return m
}

// forget drops s from every perServer map and the plain ones beside them.
func (s *Server) forget() {
	for _, m := range perServerMaps {
		m.Delete(s)
	}
	cmdAppsMu.Lock()
	delete(cmdApps, s)
	cmdAppsMu.Unlock()
	surfAppsMu.Lock()
	delete(surfApps, s)
	surfAppsMu.Unlock()
}
