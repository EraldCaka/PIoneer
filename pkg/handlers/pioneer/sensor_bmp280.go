package pioneer

import (
	"fmt"
	"time"
)

const (
	bmp280RegChipID  = 0xD0
	bmp280RegData    = 0xF7
	bmp280RegControl = 0xF4
	bmp280RegCalib   = 0x88
	bmp280ChipID     = 0x58
	bmp280ModeNormal = 0x57
)

type bmp280Calib struct {
	T1                             uint16
	T2, T3                         int16
	P1                             uint16
	P2, P3, P4, P5, P6, P7, P8, P9 int16
}

type bmp280Driver struct {
	dev     *Device
	id      string
	bus     int
	address string
	calib   bmp280Calib
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
	if err := d.dev.I2CWriteRegister(d.bus, d.address, bmp280RegControl, []byte{bmp280ModeNormal}); err != nil {
		return fmt.Errorf("bmp280 init: %w", err)
	}
	time.Sleep(40 * time.Millisecond)

	raw, err := d.dev.I2CReadRegister(d.bus, d.address, bmp280RegCalib, 24)
	if err != nil {
		return fmt.Errorf("bmp280 calib read: %w", err)
	}

	c := &d.calib
	c.T1 = uint16(raw[1])<<8 | uint16(raw[0])
	c.T2 = int16(uint16(raw[3])<<8 | uint16(raw[2]))
	c.T3 = int16(uint16(raw[5])<<8 | uint16(raw[4]))
	c.P1 = uint16(raw[7])<<8 | uint16(raw[6])
	c.P2 = int16(uint16(raw[9])<<8 | uint16(raw[8]))
	c.P3 = int16(uint16(raw[11])<<8 | uint16(raw[10]))
	c.P4 = int16(uint16(raw[13])<<8 | uint16(raw[12]))
	c.P5 = int16(uint16(raw[15])<<8 | uint16(raw[14]))
	c.P6 = int16(uint16(raw[17])<<8 | uint16(raw[16]))
	c.P7 = int16(uint16(raw[19])<<8 | uint16(raw[18]))
	c.P8 = int16(uint16(raw[21])<<8 | uint16(raw[20]))
	c.P9 = int16(uint16(raw[23])<<8 | uint16(raw[22]))
	return nil
}

func (d *bmp280Driver) Read() (map[string]any, error) {
	var lastErr error

	for attempt := 0; attempt < 3; attempt++ {
		_ = d.dev.recoverI2C(d.bus)
		time.Sleep(200 * time.Millisecond)

		raw, err := d.dev.I2CReadRegister(d.bus, d.address, bmp280RegData, 6)
		if err == nil && len(raw) == 6 {
			pressureRaw := int32(raw[0])<<12 | int32(raw[1])<<4 | int32(raw[2])>>4
			tempRaw := int32(raw[3])<<12 | int32(raw[4])<<4 | int32(raw[5])>>4

			tempC, pressPA := d.compensate(tempRaw, pressureRaw)

			return map[string]any{
				"id":          d.id,
				"type":        "bmp280",
				"temperature": tempC,
				"pressure":    pressPA,
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

func (d *bmp280Driver) compensate(adcT, adcP int32) (tempC, pressPA float64) {
	c := d.calib

	var1 := (float64(adcT)/16384.0 - float64(c.T1)/1024.0) * float64(c.T2)
	var2 := (float64(adcT)/131072.0 - float64(c.T1)/8192.0) *
		(float64(adcT)/131072.0 - float64(c.T1)/8192.0) * float64(c.T3)
	tFine := var1 + var2
	tempC = tFine / 5120.0

	p1 := tFine/2.0 - 64000.0
	p2 := p1 * p1 * float64(c.P6) / 32768.0
	p2 = p2 + p1*float64(c.P5)*2.0
	p2 = p2/4.0 + float64(c.P4)*65536.0
	p1 = (float64(c.P3)*p1*p1/524288.0 + float64(c.P2)*p1) / 524288.0
	p1 = (1.0 + p1/32768.0) * float64(c.P1)
	if p1 == 0 {
		return tempC, 0
	}
	p := 1048576.0 - float64(adcP)
	p = (p - p2/4096.0) * 6250.0 / p1
	p += (float64(c.P9)*p*p/2147483648.0 + float64(c.P8)*p + float64(c.P7)) / 16.0
	return tempC, p
}
