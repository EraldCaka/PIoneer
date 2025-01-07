package main

import (
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

	_, err = pioneer.New(configFile)
	if err != nil {
		log.Fatalf("failed to initialize device config: %v", err)
	}

	// if err := pioneer.Start(); err != nil {
	// 	log.Fatalf("failed to start the device: %v", err)
	// }
	// defer pioneer.Stop()

	// value, err := pioneer.Read(17, false)
	// if err != nil {
	// 	log.Fatalf("failed to read from the device: %v", err)
	// }
	// fmt.Println("GPIO pin read successfully! Value: ", value)

	// if err := pioneer.Write(17, 1, false); err != nil {
	// 	log.Fatalf("failed to write to the device: %v", err)
	// }

	// fmt.Println("GPIO pin written successfully!")

	// value, err = pioneer.Read(17, false)
	// if err != nil {
	// 	log.Fatalf("failed to read from the device: %v", err)
	// }
	// fmt.Println("GPIO pin read successfully! Value: ", value)

}
