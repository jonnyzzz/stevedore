package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	fmt.Println("Stevedore - Tiny container management system")
	fmt.Println("Running on:", getHostname())
	fmt.Println("Version: 0.1.0")
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		log.Printf("Warning: Could not get hostname: %v", err)
		return "unknown"
	}
	return hostname
}
