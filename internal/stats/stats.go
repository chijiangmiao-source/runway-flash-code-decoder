// Package stats keeps the process-level tally of POST /decode outcomes.
// Only the start time and four counters are stored — never any pulse data —
// and everything resets when the process restarts.
package stats

import (
	"sync"
	"time"
)

// Category is the single bucket a completed POST /decode request is counted
// into, chosen by the final HTTP status the decode chain produced.
type Category int

const (
	// CategorySuccess is a fully decoded record (HTTP 200).
	CategorySuccess Category = iota
	// CategoryBadRequest is a malformed request (HTTP 400): invalid JSON, a
	// field of the wrong type, an out-of-range tick scale or an overflowing
	// duration.
	CategoryBadRequest
	// CategoryUndecodable is a well-formed request whose record cannot be
	// read (HTTP 422).
	CategoryUndecodable
)

// Snapshot is a consistent read of the tally: every field comes from the
// same locked instant, so Total always equals the sum of the three
// categories.
type Snapshot struct {
	StartedAt   time.Time `json:"started_at"`
	Total       int64     `json:"total"`
	Success     int64     `json:"success"`
	BadRequest  int64     `json:"bad_request"`
	Undecodable int64     `json:"undecodable"`
}

// Recorder counts completed decode requests. It is safe for concurrent use.
type Recorder struct {
	mu          sync.Mutex
	startedAt   time.Time
	success     int64
	badRequest  int64
	undecodable int64
}

// NewRecorder starts an empty tally stamped with now, truncated to whole
// seconds and normalized to UTC for a clean RFC 3339 rendering.
func NewRecorder(now time.Time) *Recorder {
	return &Recorder{startedAt: now.UTC().Truncate(time.Second)}
}

// Record adds exactly one completed decode request to its category.
func (r *Recorder) Record(c Category) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch c {
	case CategorySuccess:
		r.success++
	case CategoryBadRequest:
		r.badRequest++
	default:
		r.undecodable++
	}
}

// Snapshot reads every counter under a single lock, so the returned fields
// are consistent with each other no matter how many requests are in flight.
func (r *Recorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Snapshot{
		StartedAt:   r.startedAt,
		Total:       r.success + r.badRequest + r.undecodable,
		Success:     r.success,
		BadRequest:  r.badRequest,
		Undecodable: r.undecodable,
	}
}
