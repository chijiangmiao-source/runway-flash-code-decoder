package decoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeValidRecords(t *testing.T) {
	cases := []struct {
		name      string
		durations []int
		message   string
		spans     []CharSpan
	}{
		{
			name: "SOS",
			durations: []int{
				100, 100, 100, 100, 100, // S = ...
				300,                     // character gap
				300, 100, 300, 100, 300, // O = ---
				300,                     // character gap
				100, 100, 100, 100, 100, // S = ...
			},
			message: "SOS",
			spans: []CharSpan{
				{Char: "S", Pattern: "...", Start: 0, End: 4},
				{Char: "O", Pattern: "---", Start: 6, End: 10},
				{Char: "S", Pattern: "...", Start: 12, End: 16},
			},
		},
		{
			name:      "A at lower bounds",
			durations: []int{80, 80, 240}, // dot min, intra gap min, dash min
			message:   "A",
			spans:     []CharSpan{{Char: "A", Pattern: ".-", Start: 0, End: 2}},
		},
		{
			name:      "N at upper bounds",
			durations: []int{360, 120, 120}, // dash max, intra gap max, dot max
			message:   "N",
			spans:     []CharSpan{{Char: "N", Pattern: "-.", Start: 0, End: 2}},
		},
		{
			name:      "R",
			durations: []int{120, 80, 240, 80, 80},
			message:   "R",
			spans:     []CharSpan{{Char: "R", Pattern: ".-.", Start: 0, End: 4}},
		},
		{
			name:      "K",
			durations: []int{240, 120, 120, 120, 360},
			message:   "K",
			spans:     []CharSpan{{Char: "K", Pattern: "-.-", Start: 0, End: 4}},
		},
		{
			name:      "S at lower bounds",
			durations: []int{80, 80, 80, 80, 80},
			message:   "S",
			spans:     []CharSpan{{Char: "S", Pattern: "...", Start: 0, End: 4}},
		},
		{
			name:      "O at upper bounds",
			durations: []int{360, 120, 360, 120, 360},
			message:   "O",
			spans:     []CharSpan{{Char: "O", Pattern: "---", Start: 0, End: 4}},
		},
		{
			name:      "character gap at lower bound 240",
			durations: []int{100, 100, 100, 100, 100, 240, 100, 100, 100, 100, 100},
			message:   "SS",
			spans: []CharSpan{
				{Char: "S", Pattern: "...", Start: 0, End: 4},
				{Char: "S", Pattern: "...", Start: 6, End: 10},
			},
		},
		{
			name:      "character gap at upper bound 360",
			durations: []int{300, 100, 100, 360, 100, 100, 300},
			message:   "NA",
			spans: []CharSpan{
				{Char: "N", Pattern: "-.", Start: 0, End: 2},
				{Char: "A", Pattern: ".-", Start: 4, End: 6},
			},
		},
		{
			name: "full table ANRKSO",
			durations: []int{
				100, 100, 300, // A
				300,
				300, 100, 100, // N
				300,
				100, 100, 300, 100, 100, // R
				300,
				300, 100, 100, 100, 300, // K
				300,
				100, 100, 100, 100, 100, // S
				300,
				300, 100, 300, 100, 300, // O
			},
			message: "ANRKSO",
			spans: []CharSpan{
				{Char: "A", Pattern: ".-", Start: 0, End: 2},
				{Char: "N", Pattern: "-.", Start: 4, End: 6},
				{Char: "R", Pattern: ".-.", Start: 8, End: 12},
				{Char: "K", Pattern: "-.-", Start: 14, End: 18},
				{Char: "S", Pattern: "...", Start: 20, End: 24},
				{Char: "O", Pattern: "---", Start: 26, End: 30},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, decErr := Decode(tc.durations)
			require.Nil(t, decErr)
			require.NotNil(t, res)
			assert.Equal(t, tc.message, res.Message)
			assert.Equal(t, tc.spans, res.Characters)
		})
	}
}

func TestDecodeInvalidRecords(t *testing.T) {
	cases := []struct {
		name      string
		durations []int
		wantIndex int
	}{
		{"empty record", []int{}, 0},
		{"zero duration", []int{0}, 0},
		{"negative duration", []int{100, -50, 100}, 1},

		{"light-on below dot window", []int{79}, 0},
		{"light-on between dot and dash", []int{121}, 0},
		{"light-on between dot and dash upper", []int{239}, 0},
		{"light-on above dash window", []int{361}, 0},
		{"light-on invalid at later index", []int{100, 100, 500}, 2},

		{"light-off below intra window", []int{100, 79, 100}, 1},
		{"light-off between windows", []int{100, 121, 100}, 1},
		{"light-off between windows upper", []int{100, 239, 100}, 1},
		{"light-off above inter window", []int{100, 361, 100}, 1},

		{"record ends on light-off after valid char", []int{100, 100, 100, 100, 100, 300}, 5},
		{"record ends on intra-character gap", []int{100, 100}, 1},

		{"single dot not in table", []int{100}, 0},
		{"two dots not in table", []int{100, 100, 100}, 0},
		{"two dashes not in table", []int{300, 100, 300}, 0},
		{"four dots not in table", []int{100, 100, 100, 100, 100, 100, 100}, 0},
		{"unmapped second char reports its own start", []int{100, 100, 100, 100, 100, 300, 100}, 6},
		{"unmapped first char beats later pulses", []int{100, 300, 300}, 0},
		{"invalid gap beats later unmapped char", []int{100, 500, 100}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, decErr := Decode(tc.durations)
			require.Nil(t, res, "no partial result may be returned on error")
			require.NotNil(t, decErr)
			assert.Equal(t, tc.wantIndex, decErr.Index)
			assert.NotEmpty(t, decErr.Reason)
		})
	}
}
