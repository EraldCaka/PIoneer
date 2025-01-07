package digital

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/EraldCaka/PIoneer/pkg/config"
	"golang.org/x/crypto/ssh"
)

type digitalHandler struct {
	pins      []config.Digital
	sshClient *ssh.Client
}

func New(config []config.Digital, client *ssh.Client) config.Pin {
	return &digitalHandler{
		pins:      config,
		sshClient: client,
	}
}

func (d *digitalHandler) Read(pin int) (int, error) {
	if d.sshClient == nil {
		return 0, fmt.Errorf("no active SSH connection")
	}

	command := fmt.Sprintf("cat /sys/class/gpio/gpio%d/value", pin)
	session, err := d.sshClient.NewSession()
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

func (d *digitalHandler) Write(pin int, value int) error {
	if d.sshClient == nil {
		return fmt.Errorf("no active SSH connection")
	}

	if value < 0 && value > 1 {
		return fmt.Errorf("invalid pin value: %d", value)
	}

	command := fmt.Sprintf("echo %d > /sys/class/gpio/gpio%d/value", value, pin)

	session, err := d.sshClient.NewSession()
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
