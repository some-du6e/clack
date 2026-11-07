package jsrt

import (
	"time"
	_ "time/tzdata" // Intl time zones must work on Windows too
)

type tzResult struct {
	Offset int    `json:"offset"` // minutes east of UTC at that instant
	Abbr   string `json:"abbr"`   // "PST", "CET", or "GMT+5:30" style when the zone has none
	Name   string `json:"name"`   // canonical IANA name
}

// tzInfo resolves an IANA zone (or "" for the local zone) at epoch ms.
func tzInfo(zone string, ms float64) (any, error) {
	loc := time.Local
	if zone != "" && zone != "Local" {
		l, err := time.LoadLocation(zone)
		if err != nil {
			return nil, &JSError{Name: "RangeError", Message: "Invalid time zone specified: " + zone}
		}
		loc = l
	}
	t := time.UnixMilli(int64(ms)).In(loc)
	abbr, off := t.Zone()
	name := loc.String()
	if name == "Local" {
		name = localZoneName()
	}
	return tzResult{Offset: off / 60, Abbr: abbr, Name: name}, nil
}

func localZoneName() string {
	if z := time.Local.String(); z != "Local" && z != "" {
		return z
	}
	return "UTC"
}

// Stats reports runtime figures (JS thread; use inside Do).
type Stats struct {
	Boot   time.Duration // New → environment and React ready
	Phases []Phase       // where Boot went
	Memory uint64        // JS heap bytes in use
}

// Phase is one step of the boot.
type Phase struct {
	Name string
	Time time.Duration
}

// Stats returns runtime figures.
func (r *Runtime) Stats() Stats {
	var s Stats
	_ = r.Do(func(js *Ctx) error {
		s.Boot = r.bootTime
		s.Phases = append([]Phase(nil), r.phases...)
		if r.eng != nil {
			s.Memory = r.eng.call("jr_memory")
		}
		return nil
	})
	return s
}
