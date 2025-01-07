package pioneer

import (
	"fmt"
	"log"
	"os"

	"github.com/EraldCaka/PIoneer/pkg/config"
	"github.com/EraldCaka/PIoneer/pkg/handlers/digital"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

type Config struct {
	config.DeviceConfig
	sshClient *ssh.Client
	digital   config.Pin
}

func New(file *os.File) (config.Device, error) {
	var config config.DeviceConfig
	decoder := yaml.NewDecoder(file)
	err := decoder.Decode(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the config file: %v", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %v", err)
	}
	log.Printf("Config: %+v\n", config)
	return &Config{
		DeviceConfig: config,
	}, nil
}

func (c *Config) Name() string {
	return c.Config.Name
}

func (c *Config) Start() error {
	sshConfig := &ssh.ClientConfig{
		User: c.Config.Url,
		Auth: []ssh.AuthMethod{
			ssh.Password(c.Config.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	address := fmt.Sprintf("%s:%s", c.Config.Url, c.Config.Port)

	client, err := ssh.Dial("tcp", address, sshConfig)
	if err != nil {
		return fmt.Errorf("failed to establish SSH connection to %s: %v", c.Name(), err)
	}
	c.sshClient = client
	c.digital = digital.New(c.Chip.DigitalPins, client)
	fmt.Printf("Connected to device %s successfully.\n", c.Name())
	return nil
}

func (c *Config) Stop() error {
	if c.sshClient == nil {
		return fmt.Errorf("no active SSH connection to terminate for %s", c.Name())
	}

	err := c.sshClient.Close()
	if err != nil {
		return fmt.Errorf("failed to close SSH connection: %v", err)
	}

	c.sshClient = nil
	fmt.Printf("Disconnected from device %s successfully.\n", c.Name())
	return nil
}

func (c *Config) Read(pin int) (int, error) {
	value, err := c.digital.Read(pin)
	if err != nil {
		return 0, fmt.Errorf("failed to read pin: %v, from the device: %v", pin, err)
	}
	return value, nil
}

func (c *Config) Write(pin int, value int) error {
	err := c.digital.Write(pin, value)
	if err != nil {
		return fmt.Errorf("failed to write to pin: %v, from the device: %v", pin, err)
	}
	return nil
}
