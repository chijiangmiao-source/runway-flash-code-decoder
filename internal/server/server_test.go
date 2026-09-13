package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return New()
}

func postRaw(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/decode", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	return w
}

func post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postRaw(t, body)
}

func TestDecodeValidRecord(t *testing.T) {
	w := post(t, `{"durations": [100,100,100,100,100, 300, 300,100,300,100,300, 300, 100,100,100,100,100]}`)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Message    string `json:"message"`
		Characters []struct {
			Char    string `json:"char"`
			Pattern string `json:"pattern"`
			Start   int    `json:"start"`
			End     int    `json:"end"`
		} `json:"characters"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "SOS", body.Message)
	require.Len(t, body.Characters, 3)
	assert.Equal(t, "S", body.Characters[0].Char)
	assert.Equal(t, 0, body.Characters[0].Start)
	assert.Equal(t, 4, body.Characters[0].End)
	assert.Equal(t, "O", body.Characters[1].Char)
	assert.Equal(t, 6, body.Characters[1].Start)
	assert.Equal(t, 10, body.Characters[1].End)
	assert.Equal(t, "S", body.Characters[2].Char)
	assert.Equal(t, 12, body.Characters[2].Start)
	assert.Equal(t, 16, body.Characters[2].End)
}

func TestDecodeWith500MicrosecondTicks(t *testing.T) {
	w := post(t, `{"tick_micros": 500, "durations": [200,200,200,200,200, 600, 600,200,600,200,600, 600, 200,200,200,200,200]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Message    string `json:"message"`
		Characters []struct {
			Char  string `json:"char"`
			Start int    `json:"start"`
			End   int    `json:"end"`
		} `json:"characters"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "SOS", body.Message)
	require.Len(t, body.Characters, 3)
	assert.Equal(t, "S", body.Characters[0].Char)
	assert.Equal(t, 0, body.Characters[0].Start)
	assert.Equal(t, 4, body.Characters[0].End)
	assert.Equal(t, "O", body.Characters[1].Char)
	assert.Equal(t, 6, body.Characters[1].Start)
	assert.Equal(t, 10, body.Characters[1].End)
	assert.Equal(t, "S", body.Characters[2].Char)
	assert.Equal(t, 12, body.Characters[2].Start)
	assert.Equal(t, 16, body.Characters[2].End)
}

func TestDecodeInvalidTickReturns400WithField(t *testing.T) {
	for _, tickMicros := range []int{0, -1, 1_000_001} {
		body := `{"durations": [], "tick_micros": ` + strconv.Itoa(tickMicros) + `}`
		w := post(t, body)
		require.Equal(t, http.StatusBadRequest, w.Code, body)

		var response map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, "tick_micros", response["field"])
		assert.NotEmpty(t, response["error"])
		assert.NotContains(t, response, "message")
	}
}

func TestDecodeDefaultResponseHasNoTrace(t *testing.T) {
	// Clients that never send include_trace must receive a response shaped
	// exactly field-for-field as before the diagnostic was added.
	w := post(t, `{"durations": [100,100,100,100,100, 300, 300,100,300,100,300, 300, 100,100,100,100,100]}`)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "message")
	assert.Contains(t, body, "characters")
	assert.NotContains(t, body, "trace")
}

func TestDecodeIncludeTraceFalseOmitsTrace(t *testing.T) {
	w := post(t, `{"durations": [100, 100, 300], "include_trace": false}`)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "A", body["message"])
	assert.NotContains(t, body, "trace")
}

func TestDecodeIncludeTraceTrue(t *testing.T) {
	// "AN": a dot, an intra-character gap, a dash, then an inter-character
	// gap, a dash, an intra-character gap and a dot — all four readings.
	w := post(t, `{"durations": [100, 100, 300, 300, 300, 100, 100], "include_trace": true}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Message    string `json:"message"`
		Characters []struct {
			Char    string `json:"char"`
			Pattern string `json:"pattern"`
			Start   int    `json:"start"`
			End     int    `json:"end"`
		} `json:"characters"`
		Trace []struct {
			Index  int    `json:"index"`
			Micros int64  `json:"micros"`
			Role   string `json:"role"`
			Result string `json:"result"`
		} `json:"trace"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "AN", body.Message)
	require.Len(t, body.Characters, 2)
	assert.Equal(t, "A", body.Characters[0].Char)
	assert.Equal(t, ".-", body.Characters[0].Pattern)
	assert.Equal(t, 0, body.Characters[0].Start)
	assert.Equal(t, 2, body.Characters[0].End)
	assert.Equal(t, "N", body.Characters[1].Char)
	assert.Equal(t, 4, body.Characters[1].Start)
	assert.Equal(t, 6, body.Characters[1].End)

	want := []struct {
		index  int
		role   string
		micros int64
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
	require.Len(t, body.Trace, len(want))
	for i, e := range want {
		assert.Equal(t, e.index, body.Trace[i].Index, "trace %d index", i)
		assert.Equal(t, e.role, body.Trace[i].Role, "trace %d role", i)
		assert.Equal(t, e.micros, body.Trace[i].Micros, "trace %d micros", i)
		assert.Equal(t, e.result, body.Trace[i].Result, "trace %d result", i)
	}
}

func TestDecodeIncludeTraceTrueWithScaledTicks(t *testing.T) {
	// A at 500µs/tick: trace micros are the post-conversion values.
	w := post(t, `{"tick_micros": 500, "durations": [160, 160, 480], "include_trace": true}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Trace []struct {
			Micros int64  `json:"micros"`
			Result string `json:"result"`
		} `json:"trace"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Trace, 3)
	assert.Equal(t, int64(80_000), body.Trace[0].Micros)
	assert.Equal(t, "dot", body.Trace[0].Result)
	assert.Equal(t, int64(80_000), body.Trace[1].Micros)
	assert.Equal(t, "intra_gap", body.Trace[1].Result)
	assert.Equal(t, int64(240_000), body.Trace[2].Micros)
	assert.Equal(t, "dash", body.Trace[2].Result)
}

func TestDecodeIncludeTraceCorruptRecordStillReturns422(t *testing.T) {
	// A valid flag must not change failure semantics: the corrupt gap at
	// index 7 still gives the original first-error 422 with no trace.
	w := post(t, `{"durations": [100,100,100,100,100,300,100,50,100], "include_trace": true}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(7), body["index"])
	assert.NotEmpty(t, body["error"])
	assert.NotContains(t, body, "message")
	assert.NotContains(t, body, "characters")
	assert.NotContains(t, body, "trace")
}

func TestDecodeIncludeTraceUnmappedCharacterStillReturns422(t *testing.T) {
	w := post(t, `{"durations": [100], "include_trace": true}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(0), body["index"])
	assert.NotContains(t, body, "trace")
}

func TestDecodeInvalidIncludeTraceTypeReturns400WithField(t *testing.T) {
	for _, raw := range []string{
		`null`,
		`"true"`,
		`"false"`,
		`1`,
		`0`,
		`2.5`,
		`[]`,
		`{}`,
	} {
		body := `{"durations": [100], "include_trace": ` + raw + `}`
		w := post(t, body)
		require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", body)

		var response map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "body: %s", body)
		assert.Equal(t, "include_trace", response["field"], "body: %s", body)
		assert.NotEmpty(t, response["error"], "body: %s", body)
		assert.NotContains(t, response, "message", "body: %s", body)
		assert.NotContains(t, response, "trace", "body: %s", body)
	}
}

func TestDecodeInvalidTickTypeReturns400WithField(t *testing.T) {
	w := post(t, `{"durations": [100], "tick_micros": 500.5}`)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "tick_micros", body["field"])
}

func TestDecodeNullTickReturns400WithField(t *testing.T) {
	// An explicit null scale is not an integer and must not fall back to the
	// default; only an omitted tick_micros member defaults.
	w := post(t, `{"durations": [80, 80, 240], "tick_micros": null}`)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "tick_micros", body["field"])
	assert.NotEmpty(t, body["error"])
	assert.NotContains(t, body, "message")
}

func TestDecodeNullDurationsReturns400WithField(t *testing.T) {
	// An explicit null array is a type error, not an empty record.
	w := post(t, `{"durations": null}`)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "durations", body["field"])
	assert.NotEmpty(t, body["error"])
	assert.NotContains(t, body, "message")
	assert.NotContains(t, body, "index")
}

func TestDecodeScaledOutOfRangePulseReturns422WithOriginalIndex(t *testing.T) {
	// Doubled clean values with the light-off gap at index 7 left at 100.
	// At 500µs/tick it is 50,000µs, below the first 80,000µs gap window.
	w := post(t, `{"tick_micros": 500, "durations": [200,200,200,200,200,600,200,100,200]}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(7), body["index"])
	assert.NotContains(t, body, "message")
}

func TestDecodeMultiplicationOverflowReturns400(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("integer JSON values on this platform cannot reach 64-bit microsecond overflow")
	}

	body := `{"durations": [100, 9223372036854775807], "tick_micros": 1000}`
	w := postRaw(t, body)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, "durations[1]", response["field"])
}

func TestDecodeOverflowOutranksEarlierInvalidPulse(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("integer JSON values on this platform cannot reach 64-bit microsecond overflow")
	}

	// Index 0 is a 50ms light-on pulse (outside every window), but the
	// overflow at index 2 is a request-level error and must be reported
	// instead of the earlier pulse-level failure.
	w := post(t, `{"durations": [50, 100, 9223372036854775807]}`)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "durations[2]", body["field"])
	assert.NotContains(t, body, "message")
	assert.NotContains(t, body, "index")
}

func TestDecodeInvalidPulseReturns422WithFirstIndex(t *testing.T) {
	// Index 7 is a 50ms light-off gap: outside every window.
	w := post(t, `{"durations": [100,100,100,100,100,300,100,50,100]}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(7), body["index"])
	assert.NotEmpty(t, body["error"])
	assert.NotContains(t, body, "message", "no partial message on failure")
	assert.NotContains(t, body, "characters", "no partial characters on failure")
}

func TestDecodeUnmappedCharacterReturns422(t *testing.T) {
	// Valid S, then a lone dot starting at index 6: not in the code table.
	w := post(t, `{"durations": [100,100,100,100,100,300,100]}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(6), body["index"])
	assert.NotContains(t, body, "message")
}

func TestDecodeTrailingGapReturns422(t *testing.T) {
	w := post(t, `{"durations": [100,100,100,100,100,300]}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(5), body["index"])
}

func TestDecodeEmptyRecordReturns422(t *testing.T) {
	w := post(t, `{"durations": []}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, float64(0), body["index"])
}

func TestDecodeMalformedBodyReturns400(t *testing.T) {
	for _, body := range []string{
		`not json`,
		`{"durations": "100,100"}`,
		`{"durations": [100, 100.5, 100]}`,
	} {
		w := post(t, body)
		assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", body)
	}
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
