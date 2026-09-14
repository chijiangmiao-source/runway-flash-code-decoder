// Package server wires the HTTP API for the Morse record decoder.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"morse-api/internal/decoder"
	"morse-api/internal/stats"
)

// decodeRequest is the JSON body of POST /decode: the alternating
// light-on/light-off pulse durations, plus the optional integer-microsecond
// size of one duration tick and the optional include_trace diagnostic flag.
// Every member is captured raw so an explicit JSON null can be told apart
// from an omitted member: null is a type error, while an omitted member
// keeps its default handling.
type decodeRequest struct {
	Durations    json.RawMessage `json:"durations"`
	TickMicros   json.RawMessage `json:"tick_micros"`
	IncludeTrace json.RawMessage `json:"include_trace"`
}

// New builds the Gin engine with every route registered.
func New() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	// The decode tally lives as long as the process: it starts empty, is
	// never persisted, and holds counters only — never any pulse data.
	recorder := stats.NewRecorder(time.Now())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.POST("/decode", func(c *gin.Context) { decode(c, recorder) })
	r.GET("/stats", func(c *gin.Context) {
		c.JSON(http.StatusOK, recorder.Snapshot())
	})
	return r
}

func decode(c *gin.Context, recorder *stats.Recorder) {
	// Every completed decode is tallied exactly once, by the final status
	// the chain below produced. The response is already written when this
	// deferred call runs, so recording can never change the outcome.
	defer func() { recorder.Record(classify(c.Writer.Status())) }()

	var req decodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response := gin.H{
			"error": "request body must be a JSON object like {\"durations\": [100, 100, 300], \"tick_micros\": 1000, \"include_trace\": false}",
		}

		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			field := jsonFieldName(typeErr.Field)
			response["field"] = field
			response["error"] = field + " has an invalid JSON type: durations must be an integer array and tick_micros must be an integer"
		}

		c.JSON(http.StatusBadRequest, response)
		return
	}

	durations, ok := parseDurations(c, req.Durations)
	if !ok {
		return
	}
	tickMicros, ok := parseTickMicros(c, req.TickMicros)
	if !ok {
		return
	}
	includeTrace, ok := parseIncludeTrace(c, req.IncludeTrace)
	if !ok {
		return
	}

	// The diagnostic trace never changes the reading itself; on failure the
	// same first-error 422 is returned with no trace or partial message.
	decodeFn := decoder.Decode
	if includeTrace {
		decodeFn = decoder.DecodeWithTrace
	}
	result, err := decodeFn(durations, tickMicros)
	var scaleErr *decoder.ScaleError
	if errors.As(err, &scaleErr) {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": scaleErr.Field,
			"error": scaleErr.Reason,
		})
		return
	}
	if err != nil {
		var decErr *decoder.Error
		if errors.As(err, &decErr) {
			// The whole record is rejected: no partial message is ever returned.
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"index": decErr.Index,
				"error": decErr.Reason,
			})
			return
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "decoder failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// parseDurations decodes the raw durations member into integer ticks. An
// omitted member is left for the decoder to judge as an empty record, but
// an explicit JSON null is not an integer array and is rejected like any
// other non-array value. It reports whether the field is usable, having
// written the 400 response itself when it is not.
func parseDurations(c *gin.Context, raw json.RawMessage) ([]int, bool) {
	if raw == nil {
		return nil, true
	}
	if string(raw) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": "durations",
			"error": "durations must be an array of integers, got null",
		})
		return nil, false
	}
	var durations []int
	if err := json.Unmarshal(raw, &durations); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": "durations",
			"error": "durations has an invalid JSON type: durations must be an array of integers",
		})
		return nil, false
	}
	return durations, true
}

// parseTickMicros decodes the optional raw tick_micros member, defaulting
// an omitted member to the default scale. An explicit JSON null is not an
// integer scale and is rejected like a fractional or non-numeric value. It
// reports whether the field is usable, having written the 400 response
// itself when it is not.
func parseTickMicros(c *gin.Context, raw json.RawMessage) (int, bool) {
	if raw == nil {
		return decoder.DefaultTickMicros, true
	}
	if string(raw) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": "tick_micros",
			"error": "tick_micros must be an integer, got null",
		})
		return 0, false
	}
	var tickMicros int
	if err := json.Unmarshal(raw, &tickMicros); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": "tick_micros",
			"error": "tick_micros has an invalid JSON type: tick_micros must be an integer",
		})
		return 0, false
	}
	if tickMicros < decoder.MinTickMicros || tickMicros > decoder.MaxTickMicros {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": "tick_micros",
			"error": "tick_micros must be between 1 and 1000000 microseconds, got " + strconv.Itoa(tickMicros),
		})
		return 0, false
	}
	return tickMicros, true
}

// parseIncludeTrace decodes the optional raw include_trace member,
// defaulting an omitted member to false. The flag is strictly boolean: an
// explicit JSON null, a string or a number are all rejected with field
// include_trace rather than coerced (in particular 1/0 are not booleans).
// It reports whether the field is usable, having written the 400 response
// itself when it is not.
func parseIncludeTrace(c *gin.Context, raw json.RawMessage) (bool, bool) {
	if raw == nil {
		return false, true
	}
	if string(raw) == "null" {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": "include_trace",
			"error": "include_trace must be a boolean, got null",
		})
		return false, false
	}
	var includeTrace bool
	if err := json.Unmarshal(raw, &includeTrace); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": "include_trace",
			"error": "include_trace has an invalid JSON type: include_trace must be a boolean",
		})
		return false, false
	}
	return includeTrace, true
}

// classify maps the final HTTP status of a completed POST /decode request
// to its tally category: 2xx is a success, 400 a malformed request and 422
// an undecodable record. The decode chain only ever answers those statuses,
// so every completed request lands in exactly one category.
func classify(status int) stats.Category {
	switch {
	case status >= 200 && status < 300:
		return stats.CategorySuccess
	case status == http.StatusBadRequest:
		return stats.CategoryBadRequest
	default:
		return stats.CategoryUndecodable
	}
}

func jsonFieldName(field string) string {
	switch field {
	case "tick_micros":
		return "tick_micros"
	case "include_trace":
		return "include_trace"
	}
	// Field paths for array elements begin with the JSON field name.
	return "durations"
}
