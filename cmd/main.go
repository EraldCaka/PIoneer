package main

import (
	"fmt"
	"log"
	"os"

	"github.com/EraldCaka/PIoneer/pkg/handlers/pioneer"
)

func main() {
	configFile, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("failed to open config file: %v", err)
	}
	defer configFile.Close()

	deviceConfig, err := pioneer.New(configFile)
	if err != nil {
		log.Fatalf("failed to initialize device config: %v", err)
	}

	if err := deviceConfig.Start(); err != nil {
		log.Fatalf("failed to start the device: %v", err)
	}
	defer deviceConfig.Stop()

	fmt.Println("GPIO pin set successfully!")
}
