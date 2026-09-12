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
// size of one duration tick.
type decodeRequest struct {
	Durations  []int `json:"durations"`
	TickMicros *int  `json:"tick_micros"`
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

	tickMicros := decoder.DefaultTickMicros
	if req.TickMicros != nil {
		tickMicros = *req.TickMicros
	}
	if tickMicros < decoder.MinTickMicros || tickMicros > decoder.MaxTickMicros {
		c.JSON(http.StatusBadRequest, gin.H{
			"field": "tick_micros",
			"error": "tick_micros must be between 1 and 1000000 microseconds, got " + strconv.Itoa(tickMicros),
		})
		return
	}

	result, err := decoder.Decode(req.Durations, tickMicros)
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

func jsonFieldName(field string) string {
	if field == "tick_micros" {
		return "tick_micros"
	}
	// Field paths for array elements begin with the JSON field name.
	return "durations"
}
