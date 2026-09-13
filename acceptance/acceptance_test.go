// Package acceptance holds the black-box checks run by the one-shot
// "verify" docker-compose service against a live api container.
// The tests skip when API_URL is not set, so a plain `go test ./...`
// stays hermetic.
package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func apiURL(t *testing.T) string {
	t.Helper()
	base := os.Getenv("API_URL")
	if base == "" {
		t.Skip("API_URL not set; skipping live acceptance tests")
	}
	return base
}

func waitForAPI(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("api at %s did not become healthy within 30s", base)
}

func postDecode(t *testing.T, base string, payload map[string]any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	resp, err := http.Post(base+"/decode", "application/json", bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.Body.Close()
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return resp.StatusCode, body
}

func postDurations(t *testing.T, base string, durations []int) (int, map[string]any) {
	t.Helper()
	return postDecode(t, base, map[string]any{"durations": durations})
}

func TestAcceptanceValidRecordDecodesUniquely(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// SOS with every pulse on a window boundary: 80/120 dots and gaps,
	// 240/360 dashes and character gaps — the closed intervals must hold.
	durations := []int{
		80, 80, 80, 80, 80, // S
		240,
		240, 120, 240, 120, 240, // O
		360,
		120, 120, 120, 120, 120, // S
	}
	status, body := postDurations(t, base, durations)
	require.Equal(t, http.StatusOK, status, "body: %v", body)
	assert.Equal(t, "SOS", body["message"])

	chars, ok := body["characters"].([]any)
	require.True(t, ok, "characters must be an array: %v", body)
	require.Len(t, chars, 3)
	want := []struct {
		char  string
		start float64
		end   float64
	}{
		{"S", 0, 4},
		{"O", 6, 10},
		{"S", 12, 16},
	}
	for i, w := range want {
		c, ok := chars[i].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, w.char, c["char"], fmt.Sprintf("character %d", i))
		assert.Equal(t, w.start, c["start"], fmt.Sprintf("character %d start", i))
		assert.Equal(t, w.end, c["end"], fmt.Sprintf("character %d end", i))
	}
}

func TestAcceptance500MicrosecondTicksDecodeSameMessage(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	durations := []int{
		160, 160, 160, 160, 160, // S: 80ms each at 500µs/tick
		480,
		480, 240, 480, 240, 480, // O: 240ms/120ms at 500µs/tick
		720,
		240, 240, 240, 240, 240, // S: 120ms each at 500µs/tick
	}
	status, body := postDecode(t, base, map[string]any{
		"durations":   durations,
		"tick_micros": 500,
	})
	require.Equal(t, http.StatusOK, status, "body: %v", body)
	assert.Equal(t, "SOS", body["message"])

	chars, ok := body["characters"].([]any)
	require.True(t, ok, "characters must be an array: %v", body)
	require.Len(t, chars, 3)
	wantStarts := []float64{0, 6, 12}
	wantEnds := []float64{4, 10, 16}
	for i := range wantStarts {
		c := chars[i].(map[string]any)
		assert.Equal(t, wantStarts[i], c["start"])
		assert.Equal(t, wantEnds[i], c["end"])
	}
}

func TestAcceptanceDefaultResponseCarriesNoTrace(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// An old client that never sends include_trace must not gain any field.
	status, body := postDurations(t, base, []int{100, 100, 300, 300, 300, 100, 100})
	require.Equal(t, http.StatusOK, status, "body: %v", body)
	assert.Equal(t, "AN", body["message"])
	assert.Contains(t, body, "characters")
	assert.NotContains(t, body, "trace")
}

func TestAcceptanceIncludeTraceFalseOmitsTrace(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	status, body := postDecode(t, base, map[string]any{
		"durations":     []int{100, 100, 300},
		"include_trace": false,
	})
	require.Equal(t, http.StatusOK, status, "body: %v", body)
	assert.Equal(t, "A", body["message"])
	assert.NotContains(t, body, "trace")
}

func TestAcceptanceIncludeTraceItemizesEveryPulse(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// "AN": dot, intra gap, dash, inter gap, dash, intra gap, dot — every
	// pulse role and every one of the four readings appears.
	durations := []int{100, 100, 300, 300, 300, 100, 100}
	status, body := postDecode(t, base, map[string]any{
		"durations":     durations,
		"include_trace": true,
	})
	require.Equal(t, http.StatusOK, status, "body: %v", body)
	assert.Equal(t, "AN", body["message"])

	// characters keeps the exact same shape and values as without the flag.
	chars, ok := body["characters"].([]any)
	require.True(t, ok, "characters must be an array: %v", body)
	require.Len(t, chars, 2)
	first := chars[0].(map[string]any)
	assert.Equal(t, "A", first["char"])
	assert.Equal(t, ".-", first["pattern"])
	assert.Equal(t, float64(0), first["start"])
	assert.Equal(t, float64(2), first["end"])
	second := chars[1].(map[string]any)
	assert.Equal(t, "N", second["char"])
	assert.Equal(t, float64(4), second["start"])
	assert.Equal(t, float64(6), second["end"])

	trace, ok := body["trace"].([]any)
	require.True(t, ok, "trace must be an array: %v", body)
	require.Len(t, trace, len(durations), "one trace entry per original pulse")

	want := []struct {
		index  float64
		role   string
		micros float64
		result string
	}{
		{0, "light_on", 100_000, "dot"},
		{1, "light_off", 100_000, "intra_gap"},
		{2, "light_on", 300_000, "dash"},
		{3, "light_off", 300_000, "inter_gap"},
		{4, "light_on", 300_000, "dash"},
		{5, "light_off", 100_000, "intra_gap"},
		{6, "light_on", 100_000, "dot"},
	}
	for i, w := range want {
		entry, ok := trace[i].(map[string]any)
		require.True(t, ok, "trace entry %d: %v", i, trace[i])
		assert.Equal(t, w.index, entry["index"], "trace %d index", i)
		assert.Equal(t, w.role, entry["role"], "trace %d role", i)
		assert.Equal(t, w.micros, entry["micros"], "trace %d micros", i)
		assert.Equal(t, w.result, entry["result"], "trace %d result", i)
	}
}

func TestAcceptanceIncludeTraceScaledMicros(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// Same A with 500µs ticks: the trace reports the converted microseconds.
	status, body := postDecode(t, base, map[string]any{
		"durations":     []int{160, 160, 480},
		"tick_micros":   500,
		"include_trace": true,
	})
	require.Equal(t, http.StatusOK, status, "body: %v", body)
	assert.Equal(t, "A", body["message"])

	trace := body["trace"].([]any)
	require.Len(t, trace, 3)
	first := trace[0].(map[string]any)
	assert.Equal(t, float64(80_000), first["micros"])
	assert.Equal(t, "dot", first["result"])
	last := trace[2].(map[string]any)
	assert.Equal(t, float64(240_000), last["micros"])
	assert.Equal(t, "dash", last["result"])
}

func TestAcceptanceIncludeTraceRejectsNonBooleansWith400(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	for _, value := range []any{nil, "true", "false", 1, 0, 2.5, []any{}, map[string]any{}} {
		status, body := postDecode(t, base, map[string]any{
			"durations":     []int{100},
			"include_trace": value,
		})
		assert.Equal(t, http.StatusBadRequest, status, "include_trace=%v body: %v", value, body)
		assert.Equal(t, "include_trace", body["field"], "include_trace=%v", value)
		assert.NotContains(t, body, "message", "include_trace=%v", value)
		assert.NotContains(t, body, "trace", "include_trace=%v", value)
	}
}

func TestAcceptanceIncludeTraceWithCorruptRecordStillReturns422(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// A clean S, a clean character gap, then a 50ms light-off at index 7.
	// The valid diagnostic flag must not alter the first-error 422.
	status, body := postDecode(t, base, map[string]any{
		"durations":     []int{100, 100, 100, 100, 100, 300, 100, 50, 100},
		"include_trace": true,
	})
	require.Equal(t, http.StatusUnprocessableEntity, status, "body: %v", body)
	assert.Equal(t, float64(7), body["index"])
	assert.NotEmpty(t, body["error"])
	assert.NotContains(t, body, "message", "corrupt records must not leak a partial message")
	assert.NotContains(t, body, "characters", "corrupt records must not leak partial characters")
	assert.NotContains(t, body, "trace", "corrupt records must not leak a partial trace")
}

func TestAcceptanceInvalidTickRejectedWith400(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	for _, tickMicros := range []int{0, 1_000_001} {
		status, body := postDecode(t, base, map[string]any{
			"durations":   []int{100},
			"tick_micros": tickMicros,
		})
		assert.Equal(t, http.StatusBadRequest, status, "tick_micros=%d body: %v", tickMicros, body)
		assert.Equal(t, "tick_micros", body["field"])
		assert.NotContains(t, body, "message")
	}
}

func TestAcceptanceNullTickRejectedWith400(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// A valid record with an explicit null scale: null is not an integer
	// tick and must not silently fall back to the default scale.
	status, body := postDecode(t, base, map[string]any{
		"durations":   []int{80, 80, 240},
		"tick_micros": nil,
	})
	require.Equal(t, http.StatusBadRequest, status, "body: %v", body)
	assert.Equal(t, "tick_micros", body["field"])
	assert.NotContains(t, body, "message")
}

func TestAcceptanceNullDurationsRejectedWith400(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// An explicit null pulse array is a field type error, not an empty
	// record: 400 with the field named, never a 422 decode failure.
	status, body := postDecode(t, base, map[string]any{"durations": nil})
	require.Equal(t, http.StatusBadRequest, status, "body: %v", body)
	assert.Equal(t, "durations", body["field"])
	assert.NotContains(t, body, "message")
	assert.NotContains(t, body, "index")
}

func TestAcceptanceScaledOverflowRejectedWith400(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	status, body := postDecode(t, base, map[string]any{
		"durations":   []int64{100, math.MaxInt64},
		"tick_micros": 1000,
	})
	require.Equal(t, http.StatusBadRequest, status, "body: %v", body)
	assert.Equal(t, "durations[1]", body["field"])
	assert.NotContains(t, body, "message")
}

func TestAcceptanceOverflowOutranksEarlierInvalidPulse(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// Index 0 is a 50ms light-on pulse (outside every window), but the
	// request-level overflow at index 2 must be reported instead of the
	// earlier pulse-level failure.
	status, body := postDecode(t, base, map[string]any{
		"durations": []int64{50, 100, math.MaxInt64},
	})
	require.Equal(t, http.StatusBadRequest, status, "body: %v", body)
	assert.Equal(t, "durations[2]", body["field"])
	assert.NotContains(t, body, "message")
	assert.NotContains(t, body, "index")
}

func TestAcceptanceCorruptRecordStopsAtFirstAnomaly(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// A clean S, a clean character gap, then a 50ms light-off at index 7.
	status, body := postDurations(t, base, []int{100, 100, 100, 100, 100, 300, 100, 50, 100})
	require.Equal(t, http.StatusUnprocessableEntity, status, "body: %v", body)
	assert.Equal(t, float64(7), body["index"])
	assert.NotEmpty(t, body["error"])
	assert.NotContains(t, body, "message", "corrupt records must not leak a partial message")
	assert.NotContains(t, body, "characters", "corrupt records must not leak partial characters")
}

func TestAcceptanceScaledOutOfRangePulseStopsAtOriginalIndex(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// A doubled clean S and character gap, with the light-off tick at index 7
	// left undoubled: 100 × 500µs = 50,000µs, below the 80,000µs window.
	status, body := postDecode(t, base, map[string]any{
		"durations":   []int{200, 200, 200, 200, 200, 600, 200, 100, 200},
		"tick_micros": 500,
	})
	require.Equal(t, http.StatusUnprocessableEntity, status, "body: %v", body)
	assert.Equal(t, float64(7), body["index"])
	assert.NotEmpty(t, body["error"])
	assert.NotContains(t, body, "message", "corrupt records must not leak a partial message")
	assert.NotContains(t, body, "characters", "corrupt records must not leak partial characters")
}

func TestAcceptanceUnmappedCharacterRejected(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// ".."-like record: two dots are not in the supported table.
	status, body := postDurations(t, base, []int{100, 100, 100})
	require.Equal(t, http.StatusUnprocessableEntity, status, "body: %v", body)
	assert.Equal(t, float64(0), body["index"])
	assert.NotContains(t, body, "message")
}
