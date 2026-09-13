package decoder

import (
	"strconv"
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
			res, decErr := Decode(tc.durations, DefaultTickMicros)
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
			res, err := Decode(tc.durations, DefaultTickMicros)
			require.Nil(t, res, "no partial result may be returned on error")
			require.Error(t, err)
			var decErr *Error
			require.ErrorAs(t, err, &decErr)
			assert.Equal(t, tc.wantIndex, decErr.Index)
			assert.NotEmpty(t, decErr.Reason)
		})
	}
}

func TestDecodeWithTickScale(t *testing.T) {
	millisecondSOS := []int{
		80, 80, 80, 80, 80, // S
		240,
		240, 120, 240, 120, 240, // O
		360,
		120, 120, 120, 120, 120, // S
	}

	scaledSOS := make([]int, len(millisecondSOS))
	for i, d := range millisecondSOS {
		scaledSOS[i] = d * 2
	}

	defaultResult, err := Decode(millisecondSOS, DefaultTickMicros)
	require.NoError(t, err)
	scaledResult, err := Decode(scaledSOS, 500)
	require.NoError(t, err)
	assert.Equal(t, defaultResult, scaledResult)
}

func TestDecodeWithTrace(t *testing.T) {
	// SOS: dots and dashes, both intra-character and inter-character gaps.
	durations := []int{
		100, 100, 100, 100, 100, // S = ...
		300,                     // character gap
		300, 100, 300, 100, 300, // O = ---
		300,                     // character gap
		100, 100, 100, 100, 100, // S = ...
	}

	res, err := DecodeWithTrace(durations, DefaultTickMicros)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "SOS", res.Message)

	want := []TraceEntry{
		{Index: 0, Role: RoleLightOn, Micros: 100_000, Result: ReadDot},
		{Index: 1, Role: RoleLightOff, Micros: 100_000, Result: ReadIntraGap},
		{Index: 2, Role: RoleLightOn, Micros: 100_000, Result: ReadDot},
		{Index: 3, Role: RoleLightOff, Micros: 100_000, Result: ReadIntraGap},
		{Index: 4, Role: RoleLightOn, Micros: 100_000, Result: ReadDot},
		{Index: 5, Role: RoleLightOff, Micros: 300_000, Result: ReadInterGap},
		{Index: 6, Role: RoleLightOn, Micros: 300_000, Result: ReadDash},
		{Index: 7, Role: RoleLightOff, Micros: 100_000, Result: ReadIntraGap},
		{Index: 8, Role: RoleLightOn, Micros: 300_000, Result: ReadDash},
		{Index: 9, Role: RoleLightOff, Micros: 100_000, Result: ReadIntraGap},
		{Index: 10, Role: RoleLightOn, Micros: 300_000, Result: ReadDash},
		{Index: 11, Role: RoleLightOff, Micros: 300_000, Result: ReadInterGap},
		{Index: 12, Role: RoleLightOn, Micros: 100_000, Result: ReadDot},
		{Index: 13, Role: RoleLightOff, Micros: 100_000, Result: ReadIntraGap},
		{Index: 14, Role: RoleLightOn, Micros: 100_000, Result: ReadDot},
		{Index: 15, Role: RoleLightOff, Micros: 100_000, Result: ReadIntraGap},
		{Index: 16, Role: RoleLightOn, Micros: 100_000, Result: ReadDot},
	}
	require.Len(t, res.Trace, len(durations))
	for i, entry := range want {
		assert.Equal(t, entry, res.Trace[i], "trace entry %d", i)
	}
}

func TestDecodeWithoutTraceLeavesTraceNil(t *testing.T) {
	res, err := Decode([]int{100, 100, 300}, DefaultTickMicros)
	require.NoError(t, err)
	assert.Nil(t, res.Trace)
}

func TestDecodeWithTraceMatchesPlainDecode(t *testing.T) {
	durations := []int{
		100, 100, 300, // A
		300,
		300, 100, 100, // N
	}
	plain, err := Decode(durations, DefaultTickMicros)
	require.NoError(t, err)
	traced, err := DecodeWithTrace(durations, DefaultTickMicros)
	require.NoError(t, err)
	assert.Equal(t, plain.Message, traced.Message)
	assert.Equal(t, plain.Characters, traced.Characters)
}

func TestDecodeWithTraceUsesScaledMicroseconds(t *testing.T) {
	// A at 500µs/tick: doubled ticks still read as dot/intra/dash.
	res, err := DecodeWithTrace([]int{160, 160, 480}, 500)
	require.NoError(t, err)
	require.Len(t, res.Trace, 3)
	assert.Equal(t, int64(80_000), res.Trace[0].Micros)
	assert.Equal(t, ReadDot, res.Trace[0].Result)
	assert.Equal(t, int64(80_000), res.Trace[1].Micros)
	assert.Equal(t, ReadIntraGap, res.Trace[1].Result)
	assert.Equal(t, int64(240_000), res.Trace[2].Micros)
	assert.Equal(t, ReadDash, res.Trace[2].Result)
}

func TestDecodeWithTraceReturnsNothingOnError(t *testing.T) {
	// Index 7 is a 50ms light-off gap: the same 422 as plain Decode, with no
	// partial trace leaking the pulses that were classified before it.
	res, err := DecodeWithTrace([]int{100, 100, 100, 100, 100, 300, 100, 50, 100}, DefaultTickMicros)
	require.Nil(t, res)
	var decErr *Error
	require.ErrorAs(t, err, &decErr)
	assert.Equal(t, 7, decErr.Index)
}

func TestDecodeInvalidScale(t *testing.T) {
	for _, tickMicros := range []int{0, -1, MaxTickMicros + 1} {
		t.Run(strconv.Itoa(tickMicros), func(t *testing.T) {
			res, err := Decode(nil, tickMicros)
			require.Nil(t, res)
			var scaleErr *ScaleError
			require.ErrorAs(t, err, &scaleErr)
			assert.Equal(t, "tick_micros", scaleErr.Field)
		})
	}
}

func TestDecodeScaledInvalidPulseReportsOriginalIndex(t *testing.T) {
	// With 500µs ticks, [200,200,200] is a valid A; the unscaled 50 at
	// index 2 becomes 25000µs, which is not a dash.
	res, err := Decode([]int{200, 200, 50}, 500)
	require.Nil(t, res)
	var decErr *Error
	require.ErrorAs(t, err, &decErr)
	assert.Equal(t, 2, decErr.Index)
	assert.Contains(t, decErr.Reason, "25000µs")
}

func TestDecodeMultiplicationOverflow(t *testing.T) {
	overflowing, err := strconv.ParseInt("9223372036854775807", 10, 64)
	require.NoError(t, err)
	if strconv.IntSize < 64 {
		t.Skip("integer ticks on this platform cannot reach 64-bit microsecond overflow")
	}

	res, err := Decode([]int{100, 100, int(overflowing)}, DefaultTickMicros)
	require.Nil(t, res)
	var scaleErr *ScaleError
	require.ErrorAs(t, err, &scaleErr)
	assert.Equal(t, "durations[2]", scaleErr.Field)
}

func TestDecodeOverflowOutranksEarlierInvalidPulse(t *testing.T) {
	overflowing, err := strconv.ParseInt("9223372036854775807", 10, 64)
	require.NoError(t, err)
	if strconv.IntSize < 64 {
		t.Skip("integer ticks on this platform cannot reach 64-bit microsecond overflow")
	}

	// Index 0 is below the dot window, but the overflow at index 2 is a
	// request-level field error and takes priority over the pulse-level
	// failure that would be found first left to right.
	res, err := Decode([]int{50, 100, int(overflowing)}, DefaultTickMicros)
	require.Nil(t, res)
	var scaleErr *ScaleError
	require.ErrorAs(t, err, &scaleErr)
	assert.Equal(t, "durations[2]", scaleErr.Field)
}
