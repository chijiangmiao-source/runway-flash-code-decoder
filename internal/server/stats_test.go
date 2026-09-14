package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statsBody mirrors the GET /stats response for typed assertions.
type statsBody struct {
	StartedAt   string `json:"started_at"`
	Total       int64  `json:"total"`
	Success     int64  `json:"success"`
	BadRequest  int64  `json:"bad_request"`
	Undecodable int64  `json:"undecodable"`
}

// doRequest serves one request against the given engine, so several
// requests can share the same process-level tally.
func doRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// getStats returns the decoded stats response both as a raw map (for key
// presence checks) and as a typed body (for value checks).
func getStats(t *testing.T, router *gin.Engine) (map[string]any, statsBody) {
	t.Helper()
	w := doRequest(router, http.MethodGet, "/stats", "")
	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	var body statsBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return raw, body
}

func TestStatsEmptyOnFreshService(t *testing.T) {
	before := time.Now().UTC().Truncate(time.Second)
	router := newRouter()
	after := time.Now().UTC().Add(time.Second).Truncate(time.Second)

	raw, body := getStats(t, router)

	// The complete zero-value structure is returned even before the first
	// decode request.
	assert.Len(t, raw, 5)
	for _, key := range []string{"started_at", "total", "success", "bad_request", "undecodable"} {
		assert.Contains(t, raw, key)
	}
	assert.Zero(t, body.Total)
	assert.Zero(t, body.Success)
	assert.Zero(t, body.BadRequest)
	assert.Zero(t, body.Undecodable)

	startedAt, err := time.Parse(time.RFC3339, body.StartedAt)
	require.NoError(t, err, "started_at must be an RFC 3339 timestamp")
	assert.False(t, startedAt.Before(before), "started_at must not precede service startup")
	assert.False(t, startedAt.After(after), "started_at must not be in the future")
}

func TestStatsCountEachDecodeOutcomeOnce(t *testing.T) {
	router := newRouter()

	// Two successes: a plain decode and one with the diagnostic trace.
	w := doRequest(router, http.MethodPost, "/decode", `{"durations": [100,100,100,100,100, 300, 300,100,300,100,300, 300, 100,100,100,100,100]}`)
	require.Equal(t, http.StatusOK, w.Code)
	w = doRequest(router, http.MethodPost, "/decode", `{"durations": [100,100,300], "include_trace": true}`)
	require.Equal(t, http.StatusOK, w.Code)

	// Three bad requests through distinct 400 paths: malformed JSON, a
	// field type error and an out-of-range tick scale.
	w = doRequest(router, http.MethodPost, "/decode", `not json`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = doRequest(router, http.MethodPost, "/decode", `{"durations": "100,100"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	w = doRequest(router, http.MethodPost, "/decode", `{"durations": [100], "tick_micros": 0}`)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// One well-formed request whose record cannot be decoded.
	w = doRequest(router, http.MethodPost, "/decode", `{"durations": [100,100,100]}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	_, body := getStats(t, router)
	assert.Equal(t, int64(6), body.Total)
	assert.Equal(t, int64(2), body.Success)
	assert.Equal(t, int64(3), body.BadRequest)
	assert.Equal(t, int64(1), body.Undecodable)
}

func TestStatsStartedAtIsStableAcrossReads(t *testing.T) {
	router := newRouter()

	_, first := getStats(t, router)
	doRequest(router, http.MethodPost, "/decode", `{"durations": [100,100,300]}`)
	_, second := getStats(t, router)

	assert.Equal(t, first.StartedAt, second.StartedAt,
		"started_at marks process start and never moves")
	assert.Equal(t, int64(1), second.Total)
}

func TestStatsIgnoreNonDecodeTraffic(t *testing.T) {
	router := newRouter()

	for i := 0; i < 3; i++ {
		w := doRequest(router, http.MethodGet, "/healthz", "")
		require.Equal(t, http.StatusOK, w.Code)
	}
	// The stats query itself and unknown routes must not be tallied either.
	_, before := getStats(t, router)
	w := doRequest(router, http.MethodGet, "/no-such-route", "")
	require.Equal(t, http.StatusNotFound, w.Code)
	w = doRequest(router, http.MethodPost, "/healthz", "")
	require.Equal(t, http.StatusNotFound, w.Code)

	_, after := getStats(t, router)
	assert.Zero(t, after.Total)
	assert.Zero(t, after.Success)
	assert.Zero(t, after.BadRequest)
	assert.Zero(t, after.Undecodable)
	assert.Equal(t, before, after)
}

func TestStatsConcurrentMixedRequests(t *testing.T) {
	router := newRouter()

	const workers = 32
	statuses := make(chan int, 3*workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, body := range []string{
				`{"durations": [100,100,300]}`, // success
				`{"durations": "100,100"}`,     // bad request
				`{"durations": [100,100,100]}`, // undecodable
			} {
				w := doRequest(router, http.MethodPost, "/decode", body)
				statuses <- w.Code
			}
		}()
	}
	wg.Wait()
	close(statuses)

	var got200, got400, got422, gotOther int
	for code := range statuses {
		switch code {
		case http.StatusOK:
			got200++
		case http.StatusBadRequest:
			got400++
		case http.StatusUnprocessableEntity:
			got422++
		default:
			gotOther++
		}
	}
	require.Equal(t, workers, got200)
	require.Equal(t, workers, got400)
	require.Equal(t, workers, got422)
	require.Zero(t, gotOther)

	// No missed or double counting: every request was tallied exactly once
	// and the total is exactly the sum of the three categories.
	_, body := getStats(t, router)
	assert.Equal(t, int64(workers), body.Success)
	assert.Equal(t, int64(workers), body.BadRequest)
	assert.Equal(t, int64(workers), body.Undecodable)
	assert.Equal(t, int64(3*workers), body.Total)
	assert.Equal(t, body.Success+body.BadRequest+body.Undecodable, body.Total)
}
