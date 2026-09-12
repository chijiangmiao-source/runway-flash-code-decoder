// Package decoder turns the alternating light-on / light-off pulse
// durations exported by the runway threshold-light controller into the
// Morse code message the equipment broadcast.
//
// A record is a slice of positive millisecond durations. It starts with a
// light-on pulse, alternates light-off / light-on, and must end with a
// light-on pulse (so its length is always odd). All timing windows are
// closed intervals: both endpoints are valid.
package decoder

import "fmt"

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

// patterns is the complete supported code table; anything else is an error.
var patterns = map[string]string{
	".-":  "A",
	"-.":  "N",
	".-.": "R",
	"-.-": "K",
	"...": "S",
	"---": "O",
}

// CharSpan describes one decoded character and the inclusive range of
// pulse indices (0-based, into the request's durations array) it covers.
type CharSpan struct {
	Char    string `json:"char"`
	Pattern string `json:"pattern"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

// Result is a fully decoded record: the message plus, for every character,
// the pulse range it was decoded from, so the reading can be re-checked.
type Result struct {
	Message    string     `json:"message"`
	Characters []CharSpan `json:"characters"`
}

// Error pinpoints the first pulse that makes a record undecodable.
type Error struct {
	Index  int    `json:"index"`
	Reason string `json:"error"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("pulse %d: %s", e.Index, e.Reason)
}

// Decode validates durations and decodes it into Morse characters.
//
// Pulses are checked strictly left to right; the returned *Error always
// names the first offending pulse index. A character that is not in the
// supported table is reported at the index of its first pulse. On error no
// partial message is produced.
func Decode(durations []int) (*Result, *Error) {
	if len(durations) == 0 {
		return nil, &Error{Index: 0, Reason: "record is empty: at least one light-on pulse is required"}
	}

	var (
		chars     []CharSpan
		pattern   []byte
		charStart int
	)

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

	for i, d := range durations {
		switch {
		case d <= 0:
			return nil, &Error{Index: i, Reason: fmt.Sprintf("duration %dms is not a positive integer", d)}
		case i%2 == 0: // light-on pulse
			switch {
			case d >= DotMin && d <= DotMax:
				pattern = append(pattern, '.')
			case d >= DashMin && d <= DashMax:
				pattern = append(pattern, '-')
			default:
				return nil, &Error{
					Index:  i,
					Reason: fmt.Sprintf("light-on duration %dms is neither a dot (%d-%dms) nor a dash (%d-%dms)", d, DotMin, DotMax, DashMin, DashMax),
				}
			}
			if len(pattern) == 1 {
				charStart = i
			}
		default: // light-off pulse
			switch {
			case d >= IntraMin && d <= IntraMax:
				// Gap inside the current character: keep collecting.
			case d >= InterMin && d <= InterMax:
				if err := closeChar(i - 1); err != nil {
					return nil, err
				}
			default:
				return nil, &Error{
					Index:  i,
					Reason: fmt.Sprintf("light-off duration %dms is neither an intra-character gap (%d-%dms) nor an inter-character gap (%d-%dms)", d, IntraMin, IntraMax, InterMin, InterMax),
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
	return &Result{Message: string(message), Characters: chars}, nil
}
