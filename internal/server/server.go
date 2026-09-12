// Package server wires the HTTP API for the Morse record decoder.
package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"morse-api/internal/decoder"
)

// decodeRequest is the JSON body of POST /decode: the alternating
// light-on/light-off pulse durations in milliseconds.
type decodeRequest struct {
	Durations []int `json:"durations"`
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
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "request body must be a JSON object like {\"durations\": [100, 100, 300]}",
		})
		return
	}

	result, decErr := decoder.Decode(req.Durations)
	if decErr != nil {
		// The whole record is rejected: no partial message is ever returned.
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"index": decErr.Index,
			"error": decErr.Reason,
		})
		return
	}
	c.JSON(http.StatusOK, result)
}
