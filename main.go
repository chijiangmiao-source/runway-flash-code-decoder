package main

import (
	"log"
	"os"

	"morse-api/internal/server"
)

func main() {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if err := server.New().Run(addr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}
