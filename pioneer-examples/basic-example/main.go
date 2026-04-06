package main

import (
	"fmt"
	"log"
	"os"
	"time"

	PIoneer "github.com/EraldCaka/PIoneer"
)

func main() {
	file, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("failed to open config.yaml: %v", err)
	}
	defer file.Close()

	device, err := PIoneer.New(file)
	if err != nil {
		log.Fatalf("failed to create device: %v", err)
	}

	if err := device.Start(); err != nil {
		log.Fatalf("failed to start device: %v", err)
	}
	defer func() {
		if err := device.Stop(); err != nil {
			log.Printf("failed to stop device: %v", err)
		}
	}()

	log.Println("device started")
	log.Println("reading weather every 5 seconds; beeping for 2 seconds on each measurement")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		if err := device.SetDutyCycle(18, 25.0); err != nil {
			log.Printf("buzzer start failed: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
		if err := device.StopPWM(18); err != nil {
			log.Printf("buzzer stop failed: %v", err)
		}
		data, err := device.ReadSensor("weather")
		if err != nil {
			log.Printf("weather read failed: %v", err)
		} else {
			printWeather(data)
		}

		<-ticker.C
	}
}

func printWeather(data map[string]any) {
	now := time.Now().Format("2006-01-02 15:04:05")

	status, _ := data["status"].(string)

	if tempC, okT := asFloat64(data["temperature_c"]); okT {
		if pressureHPa, okP := asFloat64(data["pressure_hpa"]); okP {
			pressureMMHg := pressureHPa * 0.750061683
			fmt.Printf(
				"[%s] Temperature: %.2f °C | Pressure: %.2f hPa | %.2f mbar | %.2f mmHg\n",
				now,
				tempC,
				pressureHPa,
				pressureHPa,
				pressureMMHg,
			)
			return
		}
	}

	if tempRaw, okT := asInt(data["temp_raw"]); okT {
		if pressureRaw, okP := asInt(data["pressure_raw"]); okP {
			fmt.Printf(
				"[%s] BMP280 RAW | status=%s | temp_adc=%d | pressure_adc=%d | bytes=%v\n",
				now,
				status,
				tempRaw,
				pressureRaw,
				data["bytes"],
			)
			return
		}
	}

	fmt.Printf("[%s] weather: %+v\n", now, data)
}
func asFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	default:
		return 0, false
	}
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}
