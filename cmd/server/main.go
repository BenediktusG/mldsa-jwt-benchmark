package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"mldsa-jwt-benchmark/internal/profile"
	"mldsa-jwt-benchmark/internal/server"
	"mldsa-jwt-benchmark/internal/signing"
	"mldsa-jwt-benchmark/internal/token"
)

func main() {
	configPath := flag.String("config", "config/config.json", "configuration path")
	keyDir := flag.String("keys", "", "override key directory")
	flag.Parse()
	c, err := profile.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if *keyDir != "" {
		c.KeyDir = *keyDir
	}
	keys, err := signing.LoadAll(c)
	if err != nil {
		log.Fatal(err)
	}
	s := &http.Server{Addr: c.Listen, Handler: server.Server{Service: token.Service{Config: c, Keys: keys, Now: time.Now}}, ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 * 1024, IdleTimeout: 90 * time.Second}
	log.Printf("ready on %s", c.Listen)
	log.Fatal(s.ListenAndServe())
}
