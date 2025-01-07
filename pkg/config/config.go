package config

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

type Direction int
type Edge int
type Value int

const (
	INPUT Direction = iota
	OUTPUT
)

const (
	NONE Edge = iota
	RISING
	FALLING
	BOTH
)

const (
	LOW Value = iota
	HIGH
)

type DeviceConfig struct {
	Config Config `yaml:"config" validate:"required,dive"`
	Chip   Chip   `yaml:"chip" validate:"dive"`
}

type Config struct {
	Name     string `yaml:"name" validate:"required"`
	Url      string `yaml:"url" validate:"required"`
	Password string `yaml:"password" validate:"required"`
	Port     string `yaml:"port" validate:"required,numeric"`
}

type Chip struct {
	DigitalPins []Digital `yaml:"digital-pins" validate:"required,dive"`
	AnalogPins  []Analog  `yaml:"analog-pins" validate:"dive"`
}

type Digital struct {
	Pin        int `yaml:"pin" validate:"required,gt=0,lt=54"`
	PinDefault `yaml:",inline"`
}

type Analog struct {
	Pin        int `yaml:"pin" validate:"required,gt=0,lt=54"`
	PinDefault `yaml:",inline"`
}

type PinDefault struct {
	Id        string    `yaml:"id" validate:"required"`
	Value     Value     `yaml:"value" validate:"oneof=0 1"`
	Direction Direction `yaml:"direction" validate:"oneof=0 1"`
	Edge      Edge      `yaml:"edge" validate:"oneof=0 1 2 3"`
}

func (c *DeviceConfig) Validate() error {
	validate := validator.New()
	for _, pin := range c.Chip.DigitalPins {
		if err := validate.Struct(pin); err != nil {
			return fmt.Errorf("validation failed for digital pin %s: %v", pin.Id, err)
		}
	}
	for _, pin := range c.Chip.AnalogPins {
		if err := validate.Struct(pin); err != nil {
			return fmt.Errorf("validation failed for analog pin %s: %v", pin.Id, err)
		}
	}
	if err := validate.Struct(c.Config); err != nil {
		return fmt.Errorf("validation failed for config: %v", err)
	}
	return nil
}
