package bmp280

import (
	"fmt"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

const (
	regChipID   = 0xD0
	regCtrlMeas = 0xF4
	regConfig   = 0xF5
	regData     = 0xF7

	chipIDBMP280 = 0x58
)

type Driver struct {
	bus     config.I2CBus
	i2cBus  int
	address string
	name    string
}

func New(bus config.I2CBus, i2cBus int, address string) *Driver {
	return &Driver{
		bus:     bus,
		i2cBus:  i2cBus,
		address: address,
		name:    "bmp280",
	}
}

func (d *Driver) Name() string {
	return d.name
}

func (d *Driver) Probe() error {
	if err := d.bus.I2CProbe(d.i2cBus, d.address); err != nil {
		return fmt.Errorf("probe failed: %w", err)
	}

	data, err := d.bus.I2CReadRegister(d.i2cBus, d.address, regChipID, 1)
	if err != nil {
		return fmt.Errorf("chip ID read failed: %w", err)
	}
	if len(data) != 1 {
		return fmt.Errorf("unexpected chip ID length: %d", len(data))
	}
	if data[0] != chipIDBMP280 {
		return fmt.Errorf("unexpected chip ID: got 0x%02x want 0x%02x", data[0], chipIDBMP280)
	}

	return nil
}

func (d *Driver) Init() error {
	if err := d.bus.I2CWriteRegister(d.i2cBus, d.address, regCtrlMeas, []byte{0x27}); err != nil {
		return fmt.Errorf("write ctrl_meas failed: %w", err)
	}
	if err := d.bus.I2CWriteRegister(d.i2cBus, d.address, regConfig, []byte{0xA0}); err != nil {
		return fmt.Errorf("write config failed: %w", err)
	}
	return nil
}

func (d *Driver) Read() (map[string]any, error) {
	raw, err := d.bus.I2CReadRegister(d.i2cBus, d.address, regData, 6)
	if err != nil {
		return nil, fmt.Errorf("raw read failed: %w", err)
	}
	if len(raw) != 6 {
		return nil, fmt.Errorf("unexpected raw data length: %d", len(raw))
	}

	pressureRaw := int(raw[0])<<12 | int(raw[1])<<4 | int(raw[2])>>4
	tempRaw := int(raw[3])<<12 | int(raw[4])<<4 | int(raw[5])>>4

	return map[string]any{
		"sensor":       "bmp280",
		"address":      d.address,
		"pressure_raw": pressureRaw,
		"temp_raw":     tempRaw,
		"bytes":        raw,
	}, nil
}
