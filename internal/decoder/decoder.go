// Package decoder turns the alternating light-on / light-off pulse
// durations exported by the runway threshold-light controller into the
// Morse code message the equipment broadcast.
//
// A record is a slice of positive integer tick durations. It starts with a
// light-on pulse, alternates light-off / light-on, and must end with a
// light-on pulse (so its length is always odd). tickMicros gives the size
// of one tick in integer microseconds; DefaultTickMicros makes each tick
// one millisecond. All timing windows are closed intervals: both endpoints
// are valid.
package decoder

import (
	"fmt"
	"math"
)

const (
	// MicrosecondsPerMillisecond is used to express millisecond windows in
	// microseconds after tick scaling.
	MicrosecondsPerMillisecond int64 = 1000

	// DefaultTickMicros treats request durations as milliseconds.
	DefaultTickMicros = 1000
	// MinTickMicros and MaxTickMicros bound the optional request scale.
	MinTickMicros = 1
	MaxTickMicros = 1_000_000
)

// Timing windows in milliseconds. Every bound is inclusive.
const (
	DotMin  = 80  // shortest light-on pulse still read as a dot
	DotMax  = 120 // longest light-on pulse still read as a dot
	DashMin = 240 // shortest light-on pulse still read as a dash
	DashMax = 360 // longest light-on pulse still read as a dash

	IntraMin = 80  // shortest light-off gap inside one character
	IntraMax = 120 // longest light-off gap inside one character
	InterMin = 240 // shortest light-off gap between two characters
	InterMax = 360 // longest light-off gap between two characters
)

// Timing windows in microseconds. Every bound is inclusive.
const (
	dotMinMicros  = int64(DotMin) * MicrosecondsPerMillisecond
	dotMaxMicros  = int64(DotMax) * MicrosecondsPerMillisecond
	dashMinMicros = int64(DashMin) * MicrosecondsPerMillisecond
	dashMaxMicros = int64(DashMax) * MicrosecondsPerMillisecond

	intraMinMicros = int64(IntraMin) * MicrosecondsPerMillisecond
	intraMaxMicros = int64(IntraMax) * MicrosecondsPerMillisecond
	interMinMicros = int64(InterMin) * MicrosecondsPerMillisecond
	interMaxMicros = int64(InterMax) * MicrosecondsPerMillisecond
)

// patterns is the complete supported code table; anything else is an error.
var patterns = map[string]string{
	".-":  "A",
	"-.":  "N",
	".-.": "R",
	"-.-": "K",
	"...": "S",
	"---": "O",
}

// Pulse roles recorded in the optional trace, after the strict light-on /
// light-off alternation of the original durations array.
const (
	RoleLightOn  = "light_on"
	RoleLightOff = "light_off"
)

// Per-pulse readings recorded in the optional trace. Both gap readings
// describe light-off pulses: an intra-character gap keeps a character open,
// while an inter-character gap only splits two characters and never becomes
// part of a pattern.
const (
	ReadDot      = "dot"
	ReadDash     = "dash"
	ReadIntraGap = "intra_gap"
	ReadInterGap = "inter_gap"
)

// CharSpan describes one decoded character and the inclusive range of
// pulse indices (0-based, into the request's durations array) it covers.
type CharSpan struct {
	Char    string `json:"char"`
	Pattern string `json:"pattern"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

// TraceEntry is the on-site re-check record for one original duration: its
// index in the request array, its light-on/light-off role, its length after
// scaling to microseconds, and the reading that length was given.
type TraceEntry struct {
	Index  int    `json:"index"`
	Role   string `json:"role"`
	Micros int64  `json:"micros"`
	Result string `json:"result"`
}

// Result is a fully decoded record: the message plus, for every character,
// the pulse range it was decoded from, so the reading can be re-checked.
// Trace is only populated by DecodeWithTrace and is omitted from JSON
// otherwise, so responses without the diagnostic request field are
// byte-for-byte shaped as before.
type Result struct {
	Message    string       `json:"message"`
	Characters []CharSpan   `json:"characters"`
	Trace      []TraceEntry `json:"trace,omitempty"`
}

// Error pinpoints the first pulse that makes a record undecodable after
// tick durations have been scaled to microseconds.
type Error struct {
	Index  int    `json:"index"`
	Reason string `json:"error"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("pulse %d: %s", e.Index, e.Reason)
}

// ScaleError identifies a request field that is invalid before or while
// durations are scaled to microseconds. The HTTP layer maps it to 400.
type ScaleError struct {
	Field  string `json:"field"`
	Reason string `json:"error"`
}

func (e *ScaleError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// Decode validates durations and decodes them into Morse characters.
//
// Each duration is multiplied by tickMicros using integer arithmetic to
// obtain its length in microseconds. Every duration is scaled before any
// pulse is classified, so an overflowing element is always reported as a
// *ScaleError — a request-level failure that outranks pulse-level decoding
// errors even when an earlier pulse would be rejected first. Pulses are
// then checked strictly left to right; a *Error always names the first
// offending original pulse index. A character that is not in the supported
// table is reported at the index of its first pulse. On error no partial
// message is produced.
//
// A *ScaleError is returned for an unsupported tick scale or integer
// multiplication overflow, before a pulse can be classified as 422.
func Decode(durations []int, tickMicros int) (*Result, error) {
	return decode(durations, tickMicros, false)
}

// DecodeWithTrace behaves exactly like Decode but additionally fills
// Result.Trace with one entry per original duration, in array order: the
// original index, the light-on/light-off role, the scaled microsecond length
// and the reading (dot, dash, intra-character gap or inter-character gap).
// The trace is diagnostic only — inter-character gaps merely split
// characters and are never appended to a pattern — so Message and
// Characters stay identical to Decode's. No trace is produced on error.
func DecodeWithTrace(durations []int, tickMicros int) (*Result, error) {
	return decode(durations, tickMicros, true)
}

func decode(durations []int, tickMicros int, includeTrace bool) (*Result, error) {
	if tickMicros < MinTickMicros || tickMicros > MaxTickMicros {
		return nil, &ScaleError{
			Field:  "tick_micros",
			Reason: fmt.Sprintf("tick_micros must be between %d and %d microseconds, got %d", MinTickMicros, MaxTickMicros, tickMicros),
		}
	}

	if len(durations) == 0 {
		return nil, &Error{Index: 0, Reason: "record is empty: at least one light-on pulse is required"}
	}

	// Scale the whole record up front: a multiplication overflow anywhere in
	// the request is a 400-level field error and takes priority over any
	// pulse-level decoding failure.
	scaled := make([]int64, len(durations))
	for i, d := range durations {
		micros, scaleErr := scaleDuration(i, d, tickMicros)
		if scaleErr != nil {
			return nil, scaleErr
		}
		scaled[i] = micros
	}

	var (
		chars     []CharSpan
		pattern   []byte
		charStart int
		trace     []TraceEntry
	)
	if includeTrace {
		trace = make([]TraceEntry, 0, len(durations))
	}

	// recordTrace only grows the diagnostic trace; on any failure the
	// partially built result is discarded, so it can never leak.
	recordTrace := func(i int, micros int64, reading string) {
		if !includeTrace {
			return
		}
		role := RoleLightOn
		if i%2 != 0 {
			role = RoleLightOff
		}
		trace = append(trace, TraceEntry{
			Index:  i,
			Role:   role,
			Micros: micros,
			Result: reading,
		})
	}

	closeChar := func(end int) *Error {
		if len(pattern) == 0 {
			return &Error{Index: charStart, Reason: "empty character: two character gaps with no pulse between them"}
		}
		p := string(pattern)
		char, ok := patterns[p]
		if !ok {
			return &Error{
				Index:  charStart,
				Reason: fmt.Sprintf("pattern %q is not in the supported code table (A=.- N=-. R=.-. K=-.- S=... O=---)", p),
			}
		}
		chars = append(chars, CharSpan{Char: char, Pattern: p, Start: charStart, End: end})
		pattern = pattern[:0]
		return nil
	}

	for i, micros := range scaled {
		switch {
		case micros <= 0:
			return nil, &Error{Index: i, Reason: fmt.Sprintf("duration %s is not a positive integer", valueText(micros, tickMicros))}
		case i%2 == 0: // light-on pulse
			switch {
			case micros >= dotMinMicros && micros <= dotMaxMicros:
				if len(pattern) == 0 {
					charStart = i
				}
				pattern = append(pattern, '.')
				recordTrace(i, micros, ReadDot)
			case micros >= dashMinMicros && micros <= dashMaxMicros:
				if len(pattern) == 0 {
					charStart = i
				}
				pattern = append(pattern, '-')
				recordTrace(i, micros, ReadDash)
			default:
				return nil, &Error{
					Index: i,
					Reason: fmt.Sprintf(
						"light-on duration %s is neither a dot (%s) nor a dash (%s)",
						valueText(micros, tickMicros),
						rangeText(dotMinMicros, dotMaxMicros, tickMicros),
						rangeText(dashMinMicros, dashMaxMicros, tickMicros),
					),
				}
			}
		default: // light-off pulse
			switch {
			case micros >= intraMinMicros && micros <= intraMaxMicros:
				// Gap inside the current character: keep collecting.
				recordTrace(i, micros, ReadIntraGap)
			case micros >= interMinMicros && micros <= interMaxMicros:
				recordTrace(i, micros, ReadInterGap)
				if err := closeChar(i - 1); err != nil {
					return nil, err
				}
			default:
				return nil, &Error{
					Index: i,
					Reason: fmt.Sprintf(
						"light-off duration %s is neither an intra-character gap (%s) nor an inter-character gap (%s)",
						valueText(micros, tickMicros),
						rangeText(intraMinMicros, intraMaxMicros, tickMicros),
						rangeText(interMinMicros, interMaxMicros, tickMicros),
					),
				}
			}
		}
	}

	if len(durations)%2 == 0 {
		return nil, &Error{
			Index:  len(durations) - 1,
			Reason: "record must end with a light-on pulse, not a light-off gap",
		}
	}
	if err := closeChar(len(durations) - 1); err != nil {
		return nil, err
	}

	message := make([]byte, 0, len(chars))
	for _, c := range chars {
		message = append(message, c.Char...)
	}
	return &Result{Message: string(message), Characters: chars, Trace: trace}, nil
}

func scaleDuration(index, duration, tickMicros int) (int64, *ScaleError) {
	d := int64(duration)
	tick := int64(tickMicros)

	overflow := false
	switch {
	case d > 0 && d > math.MaxInt64/tick:
		overflow = true
	case d < 0 && d < math.MinInt64/tick:
		overflow = true
	}
	if overflow {
		return 0, &ScaleError{
			Field: fmt.Sprintf("durations[%d]", index),
			Reason: fmt.Sprintf(
				"duration %d ticks × %d microseconds per tick overflows integer microseconds",
				duration, tickMicros,
			),
		}
	}

	return d * tick, nil
}

func valueText(micros int64, tickMicros int) string {
	if tickMicros == DefaultTickMicros {
		return fmt.Sprintf("%dms", micros/MicrosecondsPerMillisecond)
	}
	return fmt.Sprintf("%dµs", micros)
}

func rangeText(min, max int64, tickMicros int) string {
	if tickMicros == DefaultTickMicros {
		return fmt.Sprintf("%d-%dms", min/MicrosecondsPerMillisecond, max/MicrosecondsPerMillisecond)
	}
	return fmt.Sprintf("%d-%dµs", min, max)
}
