// Package acceptance holds the black-box checks run by the one-shot
// "verify" docker-compose service against a live api container.
// The tests skip when API_URL is not set, so a plain `go test ./...`
// stays hermetic.
package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func postDurations(t *testing.T, base string, durations []int) (int, map[string]any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"durations": durations})
	require.NoError(t, err)
	resp, err := http.Post(base+"/decode", "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	defer resp.Body.Close()
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return resp.StatusCode, body
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

func TestAcceptanceUnmappedCharacterRejected(t *testing.T) {
	base := apiURL(t)
	waitForAPI(t, base)

	// ".."-like record: two dots are not in the supported table.
	status, body := postDurations(t, base, []int{100, 100, 100})
	require.Equal(t, http.StatusUnprocessableEntity, status, "body: %v", body)
	assert.Equal(t, float64(0), body["index"])
	assert.NotContains(t, body, "message")
}
