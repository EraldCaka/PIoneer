package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/EraldCaka/PIoneer"
)

func main() {
	configFile, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("failed to open config file: %v", err)
	}
	defer configFile.Close()

	device, err := PIoneer.New(configFile)
	if err != nil {
		log.Fatalf("failed to initialize device: %v", err)
	}

	if err := device.Start(); err != nil {
		log.Fatalf("failed to start device: %v", err)
	}
	defer device.Stop()

	health := device.Health()
	fmt.Printf("\n=== Health ===\nConnected: %v\nReconnects: %d\nMQTT: %v\n\n",
		health.Connected, health.Reconnects, health.MQTTBound)

	fmt.Println("=== Digital Pins ===")

	val, err := device.Read(3)
	if err != nil {
		log.Printf("read pin 3 failed: %v", err)
	} else {
		log.Printf("Pin 3: %d", val)
	}

	if err := device.Write(3, 1); err != nil {
		log.Printf("write pin 3 HIGH failed: %v", err)
	} else {
		log.Println("Pin 3 → HIGH")
	}

	if err := device.Write(3, 0); err != nil {
		log.Printf("write pin 3 LOW failed: %v", err)
	} else {
		log.Println("Pin 3 → LOW")
	}

	fmt.Println("\n=== Watching Pin 5 for 3 seconds ===")

	events, err := device.Watch(5)
	if err != nil {
		log.Printf("watch pin 5 failed: %v", err)
	} else {
		go func() {
			for event := range events {
				log.Printf("Pin %d changed: %d → %d",
					event.Pin, event.OldValue, event.NewValue)
			}
		}()
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		device.Write(5, 1)
		time.Sleep(500 * time.Millisecond)
		device.Write(5, 0)
	}()

	time.Sleep(3 * time.Second)
	device.StopWatch(5)

	fmt.Println("\n=== PWM Pin 18 ===")

	if err := device.SetDutyCycle(18, 50.0); err != nil {
		log.Printf("PWM set failed: %v", err)
	} else {
		log.Println("PWM pin 18 → 50%")
	}

	duty, err := device.GetDutyCycle(18)
	if err != nil {
		log.Printf("PWM get failed: %v", err)
	} else {
		log.Printf("PWM pin 18 duty: %.2f%%", duty)
	}

	if err := device.StopPWM(18); err != nil {
		log.Printf("PWM stop failed: %v", err)
	} else {
		log.Println("PWM pin 18 stopped")
	}

	fmt.Println("\n=== I2C ===")

	if err := device.I2CWrite(1, "0x48", []byte{0x01, 0xFF}); err != nil {
		log.Printf("I2C write failed (no device may be connected): %v", err)
	} else {
		log.Println("I2C write successful")
	}

	data, err := device.I2CRead(1, "0x48", 2)
	if err != nil {
		log.Printf("I2C read failed (no device may be connected): %v", err)
	} else {
		log.Printf("I2C data: %x", data)
	}

	fmt.Println("\n=== Metrics ===")
	m := device.Metrics()
	fmt.Printf("Reads: %d | Writes: %d | Errors: %d | Pool: %d | Reconnects: %d\n",
		m.TotalReads, m.TotalWrites, m.TotalErrors, m.SSHPoolSize, m.Reconnects)
}
