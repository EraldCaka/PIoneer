package pioneer

import (
	"fmt"
	"time"
)

const (
	bmp280RegChipID = 0xD0
	bmp280RegData   = 0xF7

	bmp280ChipID = 0x58
)

type bmp280Driver struct {
	dev     *Device
	id      string
	bus     int
	address string
}

func newBMP280Driver(dev *Device, id string, bus int, address string) *bmp280Driver {
	return &bmp280Driver{
		dev:     dev,
		id:      id,
		bus:     bus,
		address: address,
	}
}

func (d *bmp280Driver) Name() string {
	return d.id
}

func (d *bmp280Driver) Probe() error {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		_ = d.dev.recoverI2C(d.bus)
		time.Sleep(200 * time.Millisecond)

		data, err := d.dev.I2CReadRegister(d.bus, d.address, bmp280RegChipID, 1)
		if err == nil && len(data) == 1 && data[0] == bmp280ChipID {
			return nil
		}

		if err != nil {
			lastErr = err
		} else if len(data) != 1 {
			lastErr = fmt.Errorf("unexpected chip ID length: %d", len(data))
		} else {
			lastErr = fmt.Errorf("unexpected chip ID: got 0x%02x want 0x%02x", data[0], bmp280ChipID)
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("probe failed: %w", lastErr)
}

func (d *bmp280Driver) Init() error {
	// No writes for now. Your hardware path is not reliable enough.
	return nil
}

func (d *bmp280Driver) Read() (map[string]any, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		_ = d.dev.recoverI2C(d.bus)
		time.Sleep(200 * time.Millisecond)

		raw, err := d.dev.I2CReadRegister(d.bus, d.address, bmp280RegData, 6)
		if err == nil && len(raw) == 6 {
			pressureRaw := int(raw[0])<<12 | int(raw[1])<<4 | int(raw[2])>>4
			tempRaw := int(raw[3])<<12 | int(raw[4])<<4 | int(raw[5])>>4

			return map[string]any{
				"id":           d.id,
				"type":         "bmp280",
				"address":      d.address,
				"bus":          d.bus,
				"pressure_raw": pressureRaw,
				"temp_raw":     tempRaw,
				"bytes":        raw,
				"status":       "raw_only",
			}, nil
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("unexpected raw data length")
		}

		time.Sleep(200 * time.Millisecond)
	}

	return nil, fmt.Errorf("raw read failed: %w", lastErr)
}
