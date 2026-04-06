package config

import (
	"fmt"
	"os"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
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

type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key"
)

type DeviceConfig struct {
	Config Config `yaml:"config" validate:"required"`
	Chip   *Chip  `yaml:"chip"`
	MQTT   *MQTT  `yaml:"mqtt"`
}

type Config struct {
	Name          string     `yaml:"device-name" validate:"required"`
	Mode          string     `yaml:"mode" validate:"omitempty,oneof=ssh local"`
	Url           string     `yaml:"url"`
	Port          string     `yaml:"port" validate:"omitempty,numeric"`
	Username      string     `yaml:"username"`
	AuthMethod    AuthMethod `yaml:"auth-method" validate:"omitempty,oneof=password key"`
	Password      string     `yaml:"password"`
	SSHKeyPath    string     `yaml:"ssh-key-path"`
	SSHKnownHosts string     `yaml:"ssh-known-hosts"`
	PoolSize      int        `yaml:"pool-size" validate:"min=1,max=10"`
	MaxRetries    int        `yaml:"max-retries" validate:"min=0,max=10"`
	RetryDelay    int        `yaml:"retry-delay" validate:"min=0"`
}

type Chip struct {
	DigitalPins []Digital `yaml:"digital-pins" validate:"dive"`
	PWMPins     []PWM     `yaml:"pwm-pins" validate:"dive"`
	I2CDevices  []I2C     `yaml:"i2c-devices" validate:"dive"`
}

type Digital struct {
	Pin        int `yaml:"pin" validate:"required,gt=0,lt=54"`
	PinDefault `yaml:",inline"`
}

type PinDefault struct {
	Id        string    `yaml:"id" validate:"required"`
	Value     Value     `yaml:"value" validate:"oneof=0 1"`
	Direction Direction `yaml:"direction" validate:"oneof=0 1"`
	Edge      Edge      `yaml:"edge" validate:"oneof=0 1 2 3"`
}

type PWM struct {
	Id          string  `yaml:"id" validate:"required"`
	Pin         int     `yaml:"pin" validate:"required,gt=0,lt=54"`
	FrequencyHz int     `yaml:"frequency" validate:"required,gt=0"`
	DutyCycle   float64 `yaml:"duty-cycle" validate:"min=0,max=100"`
}

type I2C struct {
	Id      string `yaml:"id" validate:"required"`
	Type    string `yaml:"type" validate:"required"`
	Bus     int    `yaml:"bus" validate:"required,oneof=0 1"`
	Address string `yaml:"address" validate:"required"`
}

type MQTT struct {
	Broker   string `yaml:"broker" validate:"required"`
	ClientID string `yaml:"client-id" validate:"required"`
	Topic    string `yaml:"topic" validate:"required"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	UseTLS   bool   `yaml:"use-tls"`
	CertFile string `yaml:"cert-file"`
	KeyFile  string `yaml:"key-file"`
	CAFile   string `yaml:"ca-file"`
	QOS      byte   `yaml:"qos"`
}

type Device interface {
	Start() error
	Stop() error

	Read(pin int) (int, error)
	Write(pin int, value int) error
	Watch(pin int) (<-chan PinEvent, error)
	StopWatch(pin int)

	SetDutyCycle(pin int, duty float64) error
	GetDutyCycle(pin int) (float64, error)
	StopPWM(pin int) error

	I2CProbe(bus int, address string) error
	I2CWrite(bus int, address string, data []byte) error
	I2CRead(bus int, address string, length int) ([]byte, error)
	I2CWriteRegister(bus int, address string, register byte, data []byte) error
	I2CReadRegister(bus int, address string, register byte, length int) ([]byte, error)

	ReadSensor(id string) (map[string]any, error)
	ReadSensors() (map[string]map[string]any, error)

	Health() HealthStatus
	Metrics() DeviceMetrics
}

type PinEvent struct {
	Pin      int
	OldValue int
	NewValue int
}

type HealthStatus struct {
	Connected      bool
	Reconnects     int
	ActiveWatchers int
	MQTTBound      bool
}

type DeviceMetrics struct {
	TotalReads  int64
	TotalWrites int64
	TotalErrors int64
	SSHPoolSize int
	Reconnects  int64
}

func Load(file *os.File) (*DeviceConfig, error) {
	var cfg DeviceConfig
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %v", err)
	}

	if cfg.Config.Mode == "" {
		cfg.Config.Mode = "ssh"
	}
	if cfg.Config.Port == "" {
		cfg.Config.Port = "22"
	}
	if cfg.Config.Username == "" && cfg.Config.Mode == "ssh" {
		cfg.Config.Username = "pi"
	}
	if cfg.Config.PoolSize == 0 {
		cfg.Config.PoolSize = 3
	}
	if cfg.Config.MaxRetries == 0 {
		cfg.Config.MaxRetries = 5
	}
	if cfg.Config.RetryDelay == 0 {
		cfg.Config.RetryDelay = 3
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *DeviceConfig) Validate() error {
	validate := validator.New()

	if err := validate.Struct(c.Config); err != nil {
		return fmt.Errorf("config validation failed: %v", err)
	}

	if c.Config.Mode != "local" {
		if c.Config.Url == "" {
			return fmt.Errorf("url is required in ssh mode")
		}
		if c.Config.Username == "" {
			return fmt.Errorf("username is required in ssh mode")
		}

		switch c.Config.AuthMethod {
		case AuthPassword:
			if c.Config.Password == "" {
				return fmt.Errorf("password is required when auth-method is password")
			}
		case AuthKey:
			if c.Config.SSHKeyPath == "" {
				return fmt.Errorf("ssh-key-path is required when auth-method is key")
			}
			if _, err := os.Stat(c.Config.SSHKeyPath); err != nil {
				return fmt.Errorf("ssh key file not found: %s", c.Config.SSHKeyPath)
			}
		}
	}

	if c.Chip != nil {
		for _, pin := range c.Chip.DigitalPins {
			if err := validate.Struct(pin); err != nil {
				return fmt.Errorf("digital pin %s invalid: %v", pin.Id, err)
			}
		}
		for _, p := range c.Chip.PWMPins {
			if err := validate.Struct(p); err != nil {
				return fmt.Errorf("PWM pin %s invalid: %v", p.Id, err)
			}
		}
		for _, i := range c.Chip.I2CDevices {
			if err := validate.Struct(i); err != nil {
				return fmt.Errorf("I2C device %s invalid: %v", i.Id, err)
			}
		}
	}

	return nil
}
