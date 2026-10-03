package main

import (
	"flag"
	"log"

	"mldsa-jwt-benchmark/internal/profile"
	"mldsa-jwt-benchmark/internal/signing"
)

func main() {
	configPath := flag.String("config", "config/config.json", "configuration path")
	keyDir := flag.String("keys", "keys", "key output directory")
	flag.Parse()
	c, err := profile.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	c.KeyDir = *keyDir
	if err := signing.GenerateFiles(c); err != nil {
		log.Fatal(err)
	}
	log.Printf("generated six key pairs in %s", c.KeyDir)
}
