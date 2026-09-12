// Package server wires the HTTP API for the Morse record decoder.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"morse-api/internal/decoder"
)

// decodeRequest is the JSON body of POST /decode: the alternating
// light-on/light-off pulse durations, plus the optional integer-microsecond
// size of one duration tick. Both members are captured raw so an explicit
// JSON null can be told apart from an omitted member: null is a type error,
// while an omitted member keeps its default handling.
type decodeRequest struct {
	Durations  json.RawMessage `json:"durations"`
	TickMicros json.RawMessage `json:"tick_micros"`
}

// New builds the Gin engine with every route registered.
func New() *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.POST("/decode", decode)
	return r
}

func decode(c *gin.Context) {
	var req decodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response := gin.H{
			"error": "request body must be a JSON object like {\"durations\": [100, 100, 300], \"tick_micros\": 1000}",
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

	result, err := decoder.Decode(durations, tickMicros)
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

func jsonFieldName(field string) string {
	if field == "tick_micros" {
		return "tick_micros"
	}
	// Field paths for array elements begin with the JSON field name.
	return "durations"
}
