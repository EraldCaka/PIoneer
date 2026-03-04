package pioneer

import (
	"fmt"
	"os/exec"
	"strings"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/host/v3"
)

// executor is the only thing that differs between SSH and local mode
// SSH:  runs shell commands remotely
// local: talks directly to hardware
type executor interface {
	readPin(pin int) (int, error)
	writePin(pin int, value int) error
	initPin(pin int, direction string, level string) error
	setPWM(pin int, freqHz int, duty float64) error
	getPWM(pin int) (float64, error)
	stopPWM(pin int) error
	i2cWrite(bus int, address string, data []byte) error
	i2cRead(bus int, address string, length int) ([]byte, error)
	connect() error
	close()
}

type sshExecutor struct {
	pool *sshPool
}

func newSSHExecutor(pool *sshPool) executor {
	return &sshExecutor{pool: pool}
}

func (e *sshExecutor) connect() error { return nil } // pool handles this
func (e *sshExecutor) close()         { e.pool.Close() }

func (e *sshExecutor) initPin(pin int, direction string, level string) error {
	cmd := fmt.Sprintf("sudo pinctrl set %d %s", pin, direction)
	if direction == "op" {
		cmd += " " + level
	}
	_, err := e.pool.Run(cmd)
	return err
}

func (e *sshExecutor) readPin(pin int) (int, error) {
	out, err := e.pool.Run(fmt.Sprintf("sudo pinctrl get %d", pin))
	if err != nil {
		return 0, err
	}
	return parsePinOutput(out)
}

func (e *sshExecutor) writePin(pin int, value int) error {
	level := "dl"
	if value == 1 {
		level = "dh"
	}
	_, err := e.pool.Run(fmt.Sprintf("sudo pinctrl set %d op %s", pin, level))
	return err
}

func (e *sshExecutor) setPWM(pin int, freqHz int, duty float64) error {
	raw := int(duty / 100.0 * 255)
	if _, err := e.pool.Run(fmt.Sprintf("pigs PFS %d %d", pin, freqHz)); err != nil {
		return err
	}
	_, err := e.pool.Run(fmt.Sprintf("pigs PWM %d %d", pin, raw))
	return err
}

func (e *sshExecutor) getPWM(pin int) (float64, error) {
	out, err := e.pool.Run(fmt.Sprintf("pigs GDC %d", pin))
	if err != nil {
		return 0, err
	}
	var raw int
	fmt.Sscanf(strings.TrimSpace(out), "%d", &raw)
	return float64(raw) / 255.0 * 100.0, nil
}

func (e *sshExecutor) stopPWM(pin int) error {
	_, err := e.pool.Run(fmt.Sprintf("pigs PWM %d 0", pin))
	return err
}

func (e *sshExecutor) i2cWrite(bus int, address string, data []byte) error {
	cmd := fmt.Sprintf("i2cset -y %d %s", bus, address)
	for _, b := range data {
		cmd += fmt.Sprintf(" 0x%02x", b)
	}
	_, err := e.pool.Run(cmd)
	return err
}

func (e *sshExecutor) i2cRead(bus int, address string, length int) ([]byte, error) {
	result := make([]byte, 0, length)
	for i := 0; i < length; i++ {
		out, err := e.pool.Run(fmt.Sprintf("i2cget -y %d %s 0x%02x", bus, address, i))
		if err != nil {
			return nil, err
		}
		out = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "0x"))
		var val int64
		fmt.Sscanf(out, "%x", &val)
		result = append(result, byte(val))
	}
	return result, nil
}

type localExecutor struct {
	gpioPins map[int]gpio.PinIO
	i2cBuses map[int]i2c.BusCloser
}

func newLocalExecutor() executor {
	return &localExecutor{
		gpioPins: make(map[int]gpio.PinIO),
		i2cBuses: make(map[int]i2c.BusCloser),
	}
}

func (e *localExecutor) connect() error {
	if _, err := host.Init(); err != nil {
		return fmt.Errorf("periph.io init failed: %v", err)
	}
	return nil
}

func (e *localExecutor) close() {
	for _, bus := range e.i2cBuses {
		bus.Close()
	}
}

func (e *localExecutor) initPin(pin int, direction string, level string) error {
	p := gpioreg.ByName(fmt.Sprintf("GPIO%d", pin))
	if p == nil {
		return fmt.Errorf("GPIO%d not found", pin)
	}
	if direction == "op" {
		lvl := gpio.Low
		if level == "dh" {
			lvl = gpio.High
		}
		if err := p.Out(lvl); err != nil {
			return err
		}
	} else {
		if err := p.In(gpio.PullUp, gpio.BothEdges); err != nil {
			return err
		}
	}
	e.gpioPins[pin] = p
	return nil
}

func (e *localExecutor) readPin(pin int) (int, error) {
	p, ok := e.gpioPins[pin]
	if !ok {
		return 0, fmt.Errorf("pin %d not initialized", pin)
	}
	if p.Read() == gpio.High {
		return 1, nil
	}
	return 0, nil
}

func (e *localExecutor) writePin(pin int, value int) error {
	p, ok := e.gpioPins[pin]
	if !ok {
		return fmt.Errorf("pin %d not initialized", pin)
	}
	lvl := gpio.Low
	if value == 1 {
		lvl = gpio.High
	}
	return p.Out(lvl)
}

func (e *localExecutor) setPWM(pin int, freqHz int, duty float64) error {
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("echo %d > /sys/class/pwm/pwmchip0/pwm%d/period && echo %d > /sys/class/pwm/pwmchip0/pwm%d/duty_cycle",
			1000000000/freqHz, pin,
			int(duty/100.0*float64(1000000000/freqHz)), pin,
		))
	return cmd.Run()
}

func (e *localExecutor) getPWM(pin int) (float64, error) {
	out, err := exec.Command("cat",
		fmt.Sprintf("/sys/class/pwm/pwmchip0/pwm%d/duty_cycle", pin)).Output()
	if err != nil {
		return 0, err
	}
	var raw int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &raw)
	period, _ := exec.Command("cat",
		fmt.Sprintf("/sys/class/pwm/pwmchip0/pwm%d/period", pin)).Output()
	var p int
	fmt.Sscanf(strings.TrimSpace(string(period)), "%d", &p)
	if p == 0 {
		return 0, nil
	}
	return float64(raw) / float64(p) * 100.0, nil
}

func (e *localExecutor) stopPWM(pin int) error {
	return exec.Command("bash", "-c",
		fmt.Sprintf("echo 0 > /sys/class/pwm/pwmchip0/pwm%d/duty_cycle", pin)).Run()
}

func (e *localExecutor) i2cWrite(bus int, address string, data []byte) error {
	b, err := e.getOrOpenBus(bus)
	if err != nil {
		return err
	}
	addr, err := parseI2CAddress(address)
	if err != nil {
		return err
	}
	dev := &i2c.Dev{Bus: b, Addr: addr}
	return dev.Tx(data, nil)
}

func (e *localExecutor) i2cRead(bus int, address string, length int) ([]byte, error) {
	b, err := e.getOrOpenBus(bus)
	if err != nil {
		return nil, err
	}
	addr, err := parseI2CAddress(address)
	if err != nil {
		return nil, err
	}
	dev := &i2c.Dev{Bus: b, Addr: addr}
	result := make([]byte, length)
	return result, dev.Tx(nil, result)
}

func (e *localExecutor) getOrOpenBus(bus int) (i2c.BusCloser, error) {
	if b, ok := e.i2cBuses[bus]; ok {
		return b, nil
	}
	b, err := i2creg.Open(fmt.Sprintf("%d", bus))
	if err != nil {
		return nil, fmt.Errorf("failed to open I2C bus %d: %v", bus, err)
	}
	e.i2cBuses[bus] = b
	return b, nil
}
