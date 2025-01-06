package pioneer

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/EraldCaka/PIoneer/pkg/config"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

type Config struct {
	*config.DeviceConfig
	sshClient *ssh.Client
}

func New(file *os.File) (config.Device, error) {
	var config config.DeviceConfig
	decoder := yaml.NewDecoder(file)
	err := decoder.Decode(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the config file: %v", err)
	}
	log.Println(config)
	return &Config{
		DeviceConfig: &config,
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

func (c *Config) Read(pin int, isAnalog bool) (int, error) {
	if c.sshClient == nil {
		return 0, fmt.Errorf("no active SSH connection")
	}

	command := ""
	if isAnalog {
		command = fmt.Sprintf("cat /sys/class/gpio/analog%d/value", pin)
	} else {
		command = fmt.Sprintf("cat /sys/class/gpio/gpio%d/value", pin)
	}

	session, err := c.sshClient.NewSession()
	if err != nil {
		return 0, fmt.Errorf("failed to create SSH session: %v", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(command); err != nil {
		return 0, fmt.Errorf("failed to read pin %d: %v\n%s", pin, err, stderr.String())
	}

	value, err := strconv.Atoi(stdout.String())
	if err != nil {
		return 0, fmt.Errorf("invalid pin value format: %v", err)
	}

	return value, nil
}

func (c *Config) Write(pin int, value int, isAnalog bool) error {
	if c.sshClient == nil {
		return fmt.Errorf("no active SSH connection")
	}

	if value < 0 || (isAnalog && value > 1023) || (!isAnalog && value > 1) {
		return fmt.Errorf("invalid pin value: %d", value)
	}

	command := ""
	if isAnalog {
		command = fmt.Sprintf("echo %d > /sys/class/gpio/analog%d/value", value, pin)
	} else {
		command = fmt.Sprintf("echo %d > /sys/class/gpio/gpio%d/value", value, pin)
	}

	session, err := c.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %v", err)
	}
	defer session.Close()

	var stderr bytes.Buffer
	session.Stderr = &stderr

	if err := session.Run(command); err != nil {
		return fmt.Errorf("failed to write pin %d: %v\n%s", pin, err, stderr.String())
	}

	return nil
}
