package monitor

import "time"

// Status is a site's last-known health state.
type Status int

const (
	// StatusUnknown means the site has never been checked yet this run.
	StatusUnknown Status = iota
	StatusUp
	StatusDown
)

// SiteState is the in-memory, per-site record mutated on each check cycle.
type SiteState struct {
	Name          string
	Status        Status
	LastCheckedAt time.Time
	LastError     string
}
