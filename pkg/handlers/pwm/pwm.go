package pwm

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

type pwmHandler struct {
	pins     []config.PWM
	pinMap   map[int]*config.PWM
	url      string
	password string
	port     string
}

func New(pins []config.PWM, url, password, port string) config.PWMHandler {
	pinMap := make(map[int]*config.PWM)
	for i := range pins {
		pinMap[pins[i].Pin] = &pins[i]
	}
	return &pwmHandler{
		pins:     pins,
		pinMap:   pinMap,
		url:      url,
		password: password,
		port:     port,
	}
}

func (p *pwmHandler) runSSH(command string) (string, error) {
	target := fmt.Sprintf("pi@%s", p.url)
	cmd := exec.Command("sshpass",
		"-p", p.password,
		"ssh",
		"-o", "StrictHostKeyChecking=no",
		"-p", p.port,
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

func (p *pwmHandler) Set(pin int, duty float64) error {
	if duty < 0 || duty > 100 {
		return fmt.Errorf("duty cycle must be between 0 and 100, got %.2f", duty)
	}

	pwmPin, ok := p.pinMap[pin]
	if !ok {
		return fmt.Errorf("PWM pin %d not configured", pin)
	}

	dutyCycleRaw := int(duty / 100.0 * 255)
	freqCmd := fmt.Sprintf("pigs PFS %d %d", pin, pwmPin.FrequencyHz)
	dutyCmd := fmt.Sprintf("pigs PWM %d %d", pin, dutyCycleRaw)

	if _, err := p.runSSH(freqCmd); err != nil {
		return fmt.Errorf("failed to set PWM frequency on pin %d: %v", pin, err)
	}
	if _, err := p.runSSH(dutyCmd); err != nil {
		return fmt.Errorf("failed to set PWM duty cycle on pin %d: %v", pin, err)
	}

	pwmPin.DutyCycle = duty
	return nil
}

func (p *pwmHandler) Get(pin int) (float64, error) {
	pwmPin, ok := p.pinMap[pin]
	if !ok {
		return 0, fmt.Errorf("PWM pin %d not configured", pin)
	}

	out, err := p.runSSH(fmt.Sprintf("pigs GDC %d", pin))
	if err != nil {
		return pwmPin.DutyCycle, nil
	}

	raw, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return pwmPin.DutyCycle, nil
	}

	duty := float64(raw) / 255.0 * 100.0
	pwmPin.DutyCycle = duty
	return duty, nil
}

func (p *pwmHandler) Stop(pin int) error {
	if _, ok := p.pinMap[pin]; !ok {
		return fmt.Errorf("PWM pin %d not configured", pin)
	}
	_, err := p.runSSH(fmt.Sprintf("pigs PWM %d 0", pin))
	if err != nil {
		return fmt.Errorf("failed to stop PWM on pin %d: %v", pin, err)
	}
	p.pinMap[pin].DutyCycle = 0
	return nil
}
