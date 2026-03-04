package pioneer

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

type Device struct {
	cfg        *config.DeviceConfig
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

	log.Info("device config loaded",
		zap.String("name", cfg.Config.Name),
		zap.String("url", cfg.Config.Url),
		zap.String("auth", string(cfg.Config.AuthMethod)),
		zap.Int("pool_size", cfg.Config.PoolSize),
	)

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

	return d, nil
}

func (d *Device) Name() string {
	return d.cfg.Config.Name
}

func (d *Device) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	pool, err := newSSHPool(&d.cfg.Config, d.log)
	if err != nil {
		return fmt.Errorf("failed to create SSH pool: %v", err)
	}
	d.pool = pool
	d.watch = newWatcher(pool, d.log)

	for _, pin := range d.pins {
		if err := d.initPin(pin); err != nil {
			d.log.Warn("failed to init pin", zap.Int("pin", pin.Pin), zap.Error(err))
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
	if d.pool != nil {
		d.pool.Close()
	}
	d.started = false
	d.log.Info("device stopped", zap.String("name", d.Name()))
	_ = d.log.Sync()
	return nil
}

func (d *Device) initPin(pin *config.Digital) error {
	direction := "ip"
	if pin.Direction == config.OUTPUT {
		direction = "op"
	}
	var cmd string
	if pin.Direction == config.OUTPUT {
		level := "dl"
		if pin.Value == config.HIGH {
			level = "dh"
		}
		cmd = fmt.Sprintf("sudo pinctrl set %d %s %s", pin.Pin, direction, level)
	} else {
		cmd = fmt.Sprintf("sudo pinctrl set %d %s", pin.Pin, direction)
	}
	if _, err := d.pool.Run(cmd); err != nil {
		return err
	}
	d.log.Info("pin initialized",
		zap.Int("pin", pin.Pin),
		zap.String("direction", direction),
		zap.Int("value", int(pin.Value)),
	)
	return nil
}

func (d *Device) Read(pin int) (int, error) {
	out, err := d.pool.Run(fmt.Sprintf("sudo pinctrl get %d", pin))
	if err != nil {
		d.totalErrors.Add(1)
		return 0, fmt.Errorf("read pin %d failed: %v", pin, err)
	}

	value, err := parsePinOutput(out)
	if err != nil {
		d.totalErrors.Add(1)
		return 0, err
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
		return fmt.Errorf("invalid pin value %d: must be 0 or 1", value)
	}
	level := "dl"
	if value == 1 {
		level = "dh"
	}
	if _, err := d.pool.Run(fmt.Sprintf("sudo pinctrl set %d op %s", pin, level)); err != nil {
		d.totalErrors.Add(1)
		return fmt.Errorf("write pin %d failed: %v", pin, err)
	}
	if p, ok := d.pins[pin]; ok {
		p.Value = config.Value(value)
		p.Direction = config.OUTPUT
	}
	d.totalWrites.Add(1)
	d.log.Debug("pin written", zap.Int("pin", pin), zap.Int("value", value))

	// publish to MQTT if bound
	if d.mqtt != nil {
		d.mqtt.Publish(config.PinEvent{Pin: pin, NewValue: value})
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

func (d *Device) StopWatch(pin int) {
	d.watch.StopWatch(pin)
}

func (d *Device) SetDutyCycle(pin int, duty float64) error {
	if duty < 0 || duty > 100 {
		return fmt.Errorf("duty cycle %.2f out of range [0-100]", duty)
	}
	pwmPin, ok := d.pwmPins[pin]
	if !ok {
		return fmt.Errorf("PWM pin %d not configured", pin)
	}
	raw := int(duty / 100.0 * 255)
	if _, err := d.pool.Run(fmt.Sprintf("pigs PFS %d %d", pin, pwmPin.FrequencyHz)); err != nil {
		return fmt.Errorf("failed to set PWM frequency on pin %d: %v", pin, err)
	}
	if _, err := d.pool.Run(fmt.Sprintf("pigs PWM %d %d", pin, raw)); err != nil {
		return fmt.Errorf("failed to set PWM duty on pin %d: %v", pin, err)
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
	out, err := d.pool.Run(fmt.Sprintf("pigs GDC %d", pin))
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

func (d *Device) StopPWM(pin int) error {
	if _, ok := d.pwmPins[pin]; !ok {
		return fmt.Errorf("PWM pin %d not configured", pin)
	}
	if _, err := d.pool.Run(fmt.Sprintf("pigs PWM %d 0", pin)); err != nil {
		return fmt.Errorf("failed to stop PWM on pin %d: %v", pin, err)
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
	cmd := fmt.Sprintf("i2cset -y %d %s", bus, address)
	for _, b := range data {
		cmd += fmt.Sprintf(" 0x%02x", b)
	}
	if _, err := d.pool.Run(cmd); err != nil {
		d.totalErrors.Add(1)
		return fmt.Errorf("I2C write failed bus=%d addr=%s: %v", bus, address, err)
	}
	d.log.Debug("I2C write", zap.Int("bus", bus), zap.String("addr", address), zap.Int("bytes", len(data)))
	if d.mqtt != nil {
		d.mqtt.PublishI2C(bus, address, data)
	}
	return nil
}

func (d *Device) I2CRead(bus int, address string, length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("length must be > 0")
	}
	result := make([]byte, 0, length)
	for i := 0; i < length; i++ {
		out, err := d.pool.Run(fmt.Sprintf("i2cget -y %d %s 0x%02x", bus, address, i))
		if err != nil {
			d.totalErrors.Add(1)
			return nil, fmt.Errorf("I2C read failed bus=%d addr=%s offset=%d: %v", bus, address, i, err)
		}
		out = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "0x"))
		val, err := strconv.ParseInt(out, 16, 32)
		if err != nil {
			return nil, fmt.Errorf("failed to parse I2C byte '%s': %v", out, err)
		}
		result = append(result, byte(val))
	}
	d.log.Debug("I2C read", zap.Int("bus", bus), zap.String("addr", address), zap.Int("bytes", length))
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
	return config.HealthStatus{
		Connected:      d.pool != nil && d.pool.Size() > 0,
		Reconnects:     int(d.reconnects.Load()),
		ActiveWatchers: d.watch.ActiveCount(),
		MQTTBound:      d.mqtt != nil && d.mqtt.bound.Load(),
	}
}

func (d *Device) Metrics() config.DeviceMetrics {
	return config.DeviceMetrics{
		TotalReads:  d.totalReads.Load(),
		TotalWrites: d.totalWrites.Load(),
		TotalErrors: d.totalErrors.Load(),
		SSHPoolSize: d.pool.Size(),
		Reconnects:  d.reconnects.Load(),
	}
}

func parsePinOutput(out string) (int, error) {
	lower := strings.ToLower(out)
	if strings.Contains(lower, "| hi") {
		return 1, nil
	}
	if strings.Contains(lower, "| lo") {
		return 0, nil
	}
	fields := strings.Fields(out)
	if len(fields) > 0 {
		last := strings.ToLower(fields[len(fields)-1])
		switch last {
		case "hi":
			return 1, nil
		case "lo":
			return 0, nil
		}
		if val, err := strconv.Atoi(last); err == nil {
			return val, nil
		}
	}
	return 0, fmt.Errorf("unexpected pin output: %s", out)
}
