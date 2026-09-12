package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return New()
}

func post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/decode", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	newRouter().ServeHTTP(w, req)
	return w
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
