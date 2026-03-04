package pioneer

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

type Device struct {
	cfg        *config.DeviceConfig
	exec       executor
	pool       *sshPool
	watch      *watcher
	mqtt       *mqttBridge
	pins       map[int]*config.Digital
	pwmPins    map[int]*config.PWM
	i2cDevices map[string]*config.I2C
	log        *zap.Logger
	mu         sync.RWMutex

	totalReads  atomic.Int64
	totalWrites atomic.Int64
	totalErrors atomic.Int64
	reconnects  atomic.Int64

	started bool
}

func New(file *os.File) (config.Device, error) {
	log, _ := zap.NewProduction()

	cfg, err := config.Load(file)
	if err != nil {
		return nil, err
	}

	pins := make(map[int]*config.Digital)
	for i := range cfg.Chip.DigitalPins {
		pins[cfg.Chip.DigitalPins[i].Pin] = &cfg.Chip.DigitalPins[i]
	}
	pwmPins := make(map[int]*config.PWM)
	for i := range cfg.Chip.PWMPins {
		pwmPins[cfg.Chip.PWMPins[i].Pin] = &cfg.Chip.PWMPins[i]
	}
	i2cDevices := make(map[string]*config.I2C)
	for i := range cfg.Chip.I2CDevices {
		key := fmt.Sprintf("%d:%s", cfg.Chip.I2CDevices[i].Bus, cfg.Chip.I2CDevices[i].Address)
		i2cDevices[key] = &cfg.Chip.I2CDevices[i]
	}

	d := &Device{
		cfg:        cfg,
		pins:       pins,
		pwmPins:    pwmPins,
		i2cDevices: i2cDevices,
		log:        log,
	}

	if cfg.Config.Mode == "local" {
		d.exec = newLocalExecutor()
		log.Info("mode: local (direct hardware)")
	} else {
		pool, err := newSSHPool(&cfg.Config, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create SSH pool: %v", err)
		}
		d.pool = pool
		d.exec = newSSHExecutor(pool)
		log.Info("mode: SSH", zap.String("url", cfg.Config.Url))
	}

	return d, nil
}

func (d *Device) Name() string { return d.cfg.Config.Name }

func (d *Device) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.exec.connect(); err != nil {
		return fmt.Errorf("executor connect failed: %v", err)
	}

	d.watch = newWatcher(d.pool, d.log)

	for _, pin := range d.pins {
		direction := "ip"
		level := "dl"
		if pin.Direction == config.OUTPUT {
			direction = "op"
			if pin.Value == config.HIGH {
				level = "dh"
			}
		}
		if err := d.exec.initPin(pin.Pin, direction, level); err != nil {
			d.log.Warn("failed to init pin", zap.Int("pin", pin.Pin), zap.Error(err))
		} else {
			d.log.Info("pin initialized", zap.Int("pin", pin.Pin), zap.String("dir", direction))
		}
	}

	if d.cfg.MQTT != nil {
		bridge, err := newMQTTBridge(d.cfg.MQTT, d, d.log)
		if err != nil {
			d.log.Warn("MQTT bind failed", zap.Error(err))
		} else {
			d.mqtt = bridge
		}
	}

	d.started = true
	d.log.Info("device started", zap.String("name", d.Name()))
	return nil
}

func (d *Device) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.watch != nil {
		d.watch.StopAll()
	}
	if d.mqtt != nil {
		d.mqtt.Close()
	}
	d.exec.close()
	d.started = false
	d.log.Info("device stopped", zap.String("name", d.Name()))
	_ = d.log.Sync()
	return nil
}

func (d *Device) Read(pin int) (int, error) {
	value, err := d.exec.readPin(pin)
	if err != nil {
		d.totalErrors.Add(1)
		return 0, fmt.Errorf("read pin %d: %v", pin, err)
	}
	if p, ok := d.pins[pin]; ok {
		p.Value = config.Value(value)
	}
	d.totalReads.Add(1)
	d.log.Debug("pin read", zap.Int("pin", pin), zap.Int("value", value))
	return value, nil
}

func (d *Device) Write(pin int, value int) error {
	if value != 0 && value != 1 {
		return fmt.Errorf("invalid value %d: must be 0 or 1", value)
	}
	if err := d.exec.writePin(pin, value); err != nil {
		d.totalErrors.Add(1)
		return fmt.Errorf("write pin %d: %v", pin, err)
	}
	if p, ok := d.pins[pin]; ok {
		p.Value = config.Value(value)
		p.Direction = config.OUTPUT
	}
	d.totalWrites.Add(1)
	d.log.Debug("pin written", zap.Int("pin", pin), zap.Int("value", value))
	if d.mqtt != nil {
		d.mqtt.PublishGPIO(pin, value)
	}
	return nil
}

func (d *Device) Watch(pin int) (<-chan config.PinEvent, error) {
	ch, err := d.watch.Watch(pin, d.Read)
	if err != nil {
		return nil, err
	}
	if d.mqtt != nil {
		go func() {
			for event := range ch {
				d.mqtt.Publish(event)
			}
		}()
	}
	return ch, nil
}

func (d *Device) StopWatch(pin int) { d.watch.StopWatch(pin) }

func (d *Device) SetDutyCycle(pin int, duty float64) error {
	if duty < 0 || duty > 100 {
		return fmt.Errorf("duty cycle %.2f out of range [0-100]", duty)
	}
	pwmPin, ok := d.pwmPins[pin]
	if !ok {
		return fmt.Errorf("PWM pin %d not configured", pin)
	}
	if err := d.exec.setPWM(pin, pwmPin.FrequencyHz, duty); err != nil {
		return fmt.Errorf("set PWM pin %d: %v", pin, err)
	}
	pwmPin.DutyCycle = duty
	d.log.Info("PWM set", zap.Int("pin", pin), zap.Float64("duty", duty))
	if d.mqtt != nil {
		d.mqtt.PublishPWM(pin, duty)
	}
	return nil
}

func (d *Device) GetDutyCycle(pin int) (float64, error) {
	pwmPin, ok := d.pwmPins[pin]
	if !ok {
		return 0, fmt.Errorf("PWM pin %d not configured", pin)
	}
	duty, err := d.exec.getPWM(pin)
	if err != nil {
		return pwmPin.DutyCycle, nil // fallback to cache
	}
	pwmPin.DutyCycle = duty
	return duty, nil
}

func (d *Device) StopPWM(pin int) error {
	if _, ok := d.pwmPins[pin]; !ok {
		return fmt.Errorf("PWM pin %d not configured", pin)
	}
	if err := d.exec.stopPWM(pin); err != nil {
		return fmt.Errorf("stop PWM pin %d: %v", pin, err)
	}
	d.pwmPins[pin].DutyCycle = 0
	d.log.Info("PWM stopped", zap.Int("pin", pin))
	if d.mqtt != nil {
		d.mqtt.PublishPWM(pin, 0)
	}
	return nil
}

func (d *Device) I2CWrite(bus int, address string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("data cannot be empty")
	}
	if err := d.exec.i2cWrite(bus, address, data); err != nil {
		d.totalErrors.Add(1)
		return fmt.Errorf("I2C write bus=%d addr=%s: %v", bus, address, err)
	}
	d.log.Debug("I2C write", zap.Int("bus", bus), zap.String("addr", address))
	if d.mqtt != nil {
		d.mqtt.PublishI2C(bus, address, data)
	}
	return nil
}

func (d *Device) I2CRead(bus int, address string, length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("length must be > 0")
	}
	result, err := d.exec.i2cRead(bus, address, length)
	if err != nil {
		d.totalErrors.Add(1)
		return nil, fmt.Errorf("I2C read bus=%d addr=%s: %v", bus, address, err)
	}
	d.log.Debug("I2C read", zap.Int("bus", bus), zap.String("addr", address))
	if d.mqtt != nil {
		d.mqtt.PublishI2C(bus, address, result)
	}
	return result, nil
}

func (d *Device) BindMQTT(cfg *config.MQTT) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mqtt != nil {
		d.mqtt.Close()
	}
	bridge, err := newMQTTBridge(cfg, d, d.log)
	if err != nil {
		return err
	}
	d.mqtt = bridge
	return nil
}

func (d *Device) UnbindMQTT() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.mqtt != nil {
		d.mqtt.Close()
		d.mqtt = nil
	}
}

func (d *Device) Health() config.HealthStatus {
	poolSize := 0
	if d.pool != nil {
		poolSize = d.pool.Size()
	}
	return config.HealthStatus{
		Connected:      d.started && (d.cfg.Config.Mode == "local" || poolSize > 0),
		Reconnects:     int(d.reconnects.Load()),
		ActiveWatchers: d.watch.ActiveCount(),
		MQTTBound:      d.mqtt != nil && d.mqtt.bound.Load(),
	}
}

func (d *Device) Metrics() config.DeviceMetrics {
	poolSize := 0
	if d.pool != nil {
		poolSize = d.pool.Size()
	}
	return config.DeviceMetrics{
		TotalReads:  d.totalReads.Load(),
		TotalWrites: d.totalWrites.Load(),
		TotalErrors: d.totalErrors.Load(),
		SSHPoolSize: poolSize,
		Reconnects:  d.reconnects.Load(),
	}
}

func parsePinOutput(out string) (int, error) {
	lower := out
	if len(out) > 0 {
		lower = string([]byte(out))
	}
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			lower = out[:i] + string(c+32) + out[i+1:]
		}
	}
	if contains(lower, "| hi") {
		return 1, nil
	}
	if contains(lower, "| lo") {
		return 0, nil
	}
	fields := splitFields(out)
	if len(fields) > 0 {
		last := fields[len(fields)-1]
		switch last {
		case "hi", "HI":
			return 1, nil
		case "lo", "LO":
			return 0, nil
		}
		var val int
		if _, err := fmt.Sscanf(last, "%d", &val); err == nil {
			return val, nil
		}
	}
	return 0, fmt.Errorf("unexpected pin output: %s", out)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func splitFields(s string) []string {
	var fields []string
	inField := false
	start := 0
	for i, c := range s {
		if c == ' ' || c == '\t' || c == '\n' {
			if inField {
				fields = append(fields, s[start:i])
				inField = false
			}
		} else {
			if !inField {
				start = i
				inField = true
			}
		}
	}
	if inField {
		fields = append(fields, s[start:])
	}
	return fields
}

func parseI2CAddress(address string) (uint16, error) {
	var addr uint64
	_, err := fmt.Sscanf(address, "0x%x", &addr)
	if err != nil {
		_, err = fmt.Sscanf(address, "%x", &addr)
		if err != nil {
			return 0, fmt.Errorf("invalid I2C address %s", address)
		}
	}
	return uint16(addr), nil
}
