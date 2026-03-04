package i2c

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

type i2cHandler struct {
	devices  []config.I2C
	url      string
	password string
	port     string
}

func New(devices []config.I2C, url, password, port string) config.I2CHandler {
	return &i2cHandler{
		devices:  devices,
		url:      url,
		password: password,
		port:     port,
	}
}

func (h *i2cHandler) runSSH(command string) (string, error) {
	target := fmt.Sprintf("pi@%s", h.url)
	cmd := exec.Command("sshpass",
		"-p", h.password,
		"ssh",
		"-o", "StrictHostKeyChecking=no",
		"-p", h.port,
		target,
		command,
	)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("SSH command failed: %v, stderr: %s", err, stderr.String())
	}
	return strings.TrimSpace(out.String()), nil
}

func (h *i2cHandler) Write(bus int, address string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("data cannot be empty")
	}

	args := fmt.Sprintf("i2cset -y %d %s", bus, address)
	for _, b := range data {
		args += fmt.Sprintf(" 0x%02x", b)
	}

	_, err := h.runSSH(args)
	if err != nil {
		return fmt.Errorf("I2C write failed on bus %d addr %s: %v", bus, address, err)
	}
	return nil
}

func (h *i2cHandler) Read(bus int, address string, length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("length must be greater than 0")
	}

	result := make([]byte, 0, length)

	for i := 0; i < length; i++ {
		cmd := fmt.Sprintf("i2cget -y %d %s 0x%02x", bus, address, i)
		out, err := h.runSSH(cmd)
		if err != nil {
			return nil, fmt.Errorf("I2C read failed on bus %d addr %s offset %d: %v", bus, address, i, err)
		}

		out = strings.TrimSpace(strings.TrimPrefix(out, "0x"))
		b, err := hex.DecodeString(fmt.Sprintf("%02s", out))
		if err != nil {
			val, err2 := strconv.ParseInt(out, 16, 32)
			if err2 != nil {
				return nil, fmt.Errorf("failed to parse I2C byte '%s': %v", out, err)
			}
			result = append(result, byte(val))
			continue
		}
		result = append(result, b...)
	}

	return result, nil
}
