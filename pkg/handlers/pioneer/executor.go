package pioneer

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"periph.io/x/conn/v3/gpio"
	"periph.io/x/conn/v3/gpio/gpioreg"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/host/v3"
)

type executor interface {
	connect() error
	close()

	initPin(pin int, direction string, level string) error
	readPin(pin int) (int, error)
	writePin(pin int, value int) error

	setPWM(pin int, freqHz int, duty float64) error
	getPWM(pin int) (float64, error)
	stopPWM(pin int) error

	i2cProbe(bus int, address string) error
	i2cRecover(bus int) error
	i2cListBuses() ([]int, error)
	i2cWrite(bus int, address string, data []byte) error
	i2cRead(bus int, address string, length int) ([]byte, error)
	i2cWriteRegister(bus int, address string, register byte, data []byte) error
	i2cReadRegister(bus int, address string, register byte, length int) ([]byte, error)
}

type sshExecutor struct {
	pool *sshPool
}

func newSSHExecutor(pool *sshPool) executor {
	return &sshExecutor{pool: pool}
}

func (e *sshExecutor) connect() error { return nil }
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
	raw := int(duty / 100.0 * 255.0)
	if _, err := e.pool.Run(fmt.Sprintf("pigs pfs %d %d", pin, freqHz)); err != nil {
		return err
	}
	_, err := e.pool.Run(fmt.Sprintf("pigs pwm %d %d", pin, raw))
	return err
}

func (e *sshExecutor) getPWM(pin int) (float64, error) {
	out, err := e.pool.Run(fmt.Sprintf("pigs gdc %d", pin))
	if err != nil {
		return 0, err
	}
	var raw int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d", &raw); err != nil {
		return 0, err
	}
	return float64(raw) / 255.0 * 100.0, nil
}

func (e *sshExecutor) stopPWM(pin int) error {
	_, err := e.pool.Run(fmt.Sprintf("pigs pwm %d 0", pin))
	return err
}

func (e *sshExecutor) i2cProbe(bus int, address string) error {
	_, err := e.pool.Run(fmt.Sprintf("sudo i2ctransfer -y %d w1@%s 0xD0 r1", bus, address))
	return err
}

func (e *sshExecutor) i2cRecover(bus int) error {
	_, err := e.pool.Run(fmt.Sprintf("sudo i2cdetect -y %d >/dev/null 2>&1 || true", bus))
	return err
}

func (e *sshExecutor) i2cListBuses() ([]int, error) {
	out, err := e.pool.Run(`sh -c 'for f in /dev/i2c-*; do [ -e "$f" ] || continue; echo "${f##*-}"; done'`)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(strings.TrimSpace(out))
	buses := make([]int, 0, len(fields))
	for _, f := range fields {
		var n int
		if _, err := fmt.Sscanf(f, "%d", &n); err == nil {
			buses = append(buses, n)
		}
	}
	sort.Ints(buses)
	return buses, nil
}

func (e *sshExecutor) i2cWrite(bus int, address string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("data cannot be empty")
	}
	cmd := fmt.Sprintf("sudo i2ctransfer -y %d w%d@%s", bus, len(data), address)
	for _, b := range data {
		cmd += fmt.Sprintf(" 0x%02x", b)
	}
	_, err := e.pool.Run(cmd)
	return err
}

func (e *sshExecutor) i2cRead(bus int, address string, length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("length must be > 0")
	}
	out, err := e.pool.Run(fmt.Sprintf("sudo i2ctransfer -y %d r%d@%s", bus, length, address))
	if err != nil {
		return nil, err
	}
	return parseI2CTransferOutput(out, length)
}

func (e *sshExecutor) i2cWriteRegister(bus int, address string, register byte, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("register write data cannot be empty")
	}

	// Single-byte register writes are more reliable with i2cset on this setup.
	if len(data) == 1 {
		cmd := fmt.Sprintf("sudo i2cset -y %d %s 0x%02x 0x%02x", bus, address, register, data[0])
		_, err := e.pool.Run(cmd)
		return err
	}

	cmd := fmt.Sprintf("sudo i2ctransfer -y %d w%d@%s 0x%02x", bus, len(data)+1, address, register)
	for _, b := range data {
		cmd += fmt.Sprintf(" 0x%02x", b)
	}
	_, err := e.pool.Run(cmd)
	return err
}

func (e *sshExecutor) i2cReadRegister(bus int, address string, register byte, length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("length must be > 0")
	}
	cmd := fmt.Sprintf("sudo i2ctransfer -y %d w1@%s 0x%02x r%d", bus, address, register, length)
	out, err := e.pool.Run(cmd)
	if err != nil {
		return nil, err
	}
	return parseI2CTransferOutput(out, length)
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
		return fmt.Errorf("periph init failed: %v", err)
	}
	return nil
}

func (e *localExecutor) close() {
	for _, bus := range e.i2cBuses {
		_ = bus.Close()
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
	rawPeriod := 1000000000 / freqHz
	rawDuty := int(duty / 100.0 * float64(rawPeriod))
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("echo %d > /sys/class/pwm/pwmchip0/pwm%d/period && echo %d > /sys/class/pwm/pwmchip0/pwm%d/duty_cycle",
			rawPeriod, pin, rawDuty, pin,
		))
	return cmd.Run()
}

func (e *localExecutor) getPWM(pin int) (float64, error) {
	out, err := exec.Command("cat", fmt.Sprintf("/sys/class/pwm/pwmchip0/pwm%d/duty_cycle", pin)).Output()
	if err != nil {
		return 0, err
	}
	var raw int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &raw); err != nil {
		return 0, err
	}

	periodOut, err := exec.Command("cat", fmt.Sprintf("/sys/class/pwm/pwmchip0/pwm%d/period", pin)).Output()
	if err != nil {
		return 0, err
	}
	var period int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(periodOut)), "%d", &period); err != nil {
		return 0, err
	}
	if period == 0 {
		return 0, nil
	}
	return float64(raw) / float64(period) * 100.0, nil
}

func (e *localExecutor) stopPWM(pin int) error {
	return exec.Command("bash", "-c",
		fmt.Sprintf("echo 0 > /sys/class/pwm/pwmchip0/pwm%d/duty_cycle", pin),
	).Run()
}

func (e *localExecutor) i2cProbe(bus int, address string) error {
	b, err := e.getOrOpenBus(bus)
	if err != nil {
		return err
	}
	addr, err := parseI2CAddress(address)
	if err != nil {
		return err
	}
	dev := &i2c.Dev{Bus: b, Addr: addr}
	buf := make([]byte, 1)
	return dev.Tx([]byte{0xD0}, buf)
}

func (e *localExecutor) i2cRecover(bus int) error {
	return nil
}

func (e *localExecutor) i2cListBuses() ([]int, error) {
	return []int{1}, nil
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

func (e *localExecutor) i2cWriteRegister(bus int, address string, register byte, data []byte) error {
	b, err := e.getOrOpenBus(bus)
	if err != nil {
		return err
	}
	addr, err := parseI2CAddress(address)
	if err != nil {
		return err
	}
	dev := &i2c.Dev{Bus: b, Addr: addr}
	payload := append([]byte{register}, data...)
	return dev.Tx(payload, nil)
}

func (e *localExecutor) i2cReadRegister(bus int, address string, register byte, length int) ([]byte, error) {
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
	return result, dev.Tx([]byte{register}, result)
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

func parsePinOutput(out string) (int, error) {
	s := strings.ToLower(out)
	if strings.Contains(s, "| hi") {
		return 1, nil
	}
	if strings.Contains(s, "| lo") {
		return 0, nil
	}

	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, fmt.Errorf("unexpected pin output: %s", out)
	}
	last := fields[len(fields)-1]
	switch last {
	case "hi":
		return 1, nil
	case "lo":
		return 0, nil
	}
	var val int
	if _, err := fmt.Sscanf(last, "%d", &val); err == nil {
		return val, nil
	}
	return 0, fmt.Errorf("unexpected pin output: %s", out)
}

func parseI2CAddress(address string) (uint16, error) {
	var addr uint64
	if _, err := fmt.Sscanf(address, "0x%x", &addr); err == nil {
		return uint16(addr), nil
	}
	if _, err := fmt.Sscanf(address, "%x", &addr); err == nil {
		return uint16(addr), nil
	}
	return 0, fmt.Errorf("invalid I2C address %s", address)
}

func parseI2CTransferOutput(out string, expected int) ([]byte, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != expected {
		return nil, fmt.Errorf("unexpected i2ctransfer output: %q", out)
	}

	result := make([]byte, 0, expected)
	for _, f := range fields {
		f = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(f)), "0x")
		var val int64
		if _, err := fmt.Sscanf(f, "%x", &val); err != nil {
			return nil, fmt.Errorf("failed to parse i2ctransfer byte %q: %w", f, err)
		}
		result = append(result, byte(val))
	}
	return result, nil
}
