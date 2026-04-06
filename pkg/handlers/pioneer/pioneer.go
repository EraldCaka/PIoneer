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
	sensors    map[string]sensorDriver
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
	pwmPins := make(map[int]*config.PWM)
	i2cDevices := make(map[string]*config.I2C)

	if cfg.Chip != nil {
		for i := range cfg.Chip.DigitalPins {
			pins[cfg.Chip.DigitalPins[i].Pin] = &cfg.Chip.DigitalPins[i]
		}
		for i := range cfg.Chip.PWMPins {
			pwmPins[cfg.Chip.PWMPins[i].Pin] = &cfg.Chip.PWMPins[i]
		}
		for i := range cfg.Chip.I2CDevices {
			key := fmt.Sprintf("%d:%s", cfg.Chip.I2CDevices[i].Bus, cfg.Chip.I2CDevices[i].Address)
			i2cDevices[key] = &cfg.Chip.I2CDevices[i]
		}
	}

	d := &Device{
		cfg:        cfg,
		pins:       pins,
		pwmPins:    pwmPins,
		i2cDevices: i2cDevices,
		sensors:    make(map[string]sensorDriver),
		log:        log,
	}

	if cfg.Config.Mode == "local" {
		d.exec = newLocalExecutor()
		log.Info("mode: local")
	} else {
		pool, err := newSSHPool(&cfg.Config, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create SSH pool: %v", err)
		}
		d.pool = pool
		d.exec = newSSHExecutor(pool)
		log.Info("mode: ssh", zap.String("url", cfg.Config.Url))
	}

	if cfg.Chip != nil {
		for i := range cfg.Chip.I2CDevices {
			dev := cfg.Chip.I2CDevices[i]
			switch dev.Type {
			case "bmp280":
				d.sensors[dev.Id] = newBMP280Driver(d, dev.Id, dev.Bus, dev.Address)
			case "":
			default:
				return nil, fmt.Errorf("unsupported sensor type %q for i2c device %q", dev.Type, dev.Id)
			}
		}
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

	if d.cfg.Chip != nil {
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
			}
		}
	}

	// d.autoDiscoverSensors()

	// for id, s := range d.sensors {
	// 	if err := s.Probe(); err != nil {
	// 		d.log.Warn("sensor probe failed", zap.String("sensor", id), zap.Error(err))
	// 		continue
	// 	}
	// 	if err := s.Init(); err != nil {
	// 		d.log.Warn("sensor init failed", zap.String("sensor", id), zap.Error(err))
	// 		continue
	// 	}
	// 	d.log.Info("sensor initialized", zap.String("sensor", id))
	// }

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
	if d.exec != nil {
		d.exec.close()
	}
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
	return value, nil
}

func (d *Device) Write(pin int, value int) error {
	if value != 0 && value != 1 {
		return fmt.Errorf("invalid value %d", value)
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
		return fmt.Errorf("duty cycle %.2f out of range [0,100]", duty)
	}
	pwmPin, ok := d.pwmPins[pin]
	if !ok {
		return fmt.Errorf("PWM pin %d not configured", pin)
	}
	if err := d.exec.setPWM(pin, pwmPin.FrequencyHz, duty); err != nil {
		return fmt.Errorf("set PWM pin %d: %v", pin, err)
	}
	pwmPin.DutyCycle = duty
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
		return pwmPin.DutyCycle, nil
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
	if d.mqtt != nil {
		d.mqtt.PublishPWM(pin, 0)
	}
	return nil
}

func (d *Device) I2CProbe(bus int, address string) error {
	if err := d.exec.i2cProbe(bus, address); err != nil {
		d.totalErrors.Add(1)
		return fmt.Errorf("I2C probe bus=%d addr=%s: %v", bus, address, err)
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
	if d.mqtt != nil {
		d.mqtt.PublishI2C(bus, address, result)
	}
	return result, nil
}

func (d *Device) I2CWriteRegister(bus int, address string, register byte, data []byte) error {
	if err := d.exec.i2cWriteRegister(bus, address, register, data); err != nil {
		d.totalErrors.Add(1)
		return fmt.Errorf("I2C write register bus=%d addr=%s reg=0x%02x: %v", bus, address, register, err)
	}
	return nil
}

func (d *Device) I2CReadRegister(bus int, address string, register byte, length int) ([]byte, error) {
	if length <= 0 {
		return nil, fmt.Errorf("length must be > 0")
	}
	result, err := d.exec.i2cReadRegister(bus, address, register, length)
	if err != nil {
		d.totalErrors.Add(1)
		return nil, fmt.Errorf("I2C read register bus=%d addr=%s reg=0x%02x: %v", bus, address, register, err)
	}
	if d.mqtt != nil {
		d.mqtt.PublishI2C(bus, address, result)
	}
	return result, nil
}

func (d *Device) ReadSensor(id string) (map[string]any, error) {
	s, ok := d.sensors[id]
	if !ok {
		return nil, fmt.Errorf("sensor %q not configured", id)
	}

	data, err := s.Read()
	if err != nil {
		d.totalErrors.Add(1)
		return nil, fmt.Errorf("read sensor %q: %v", id, err)
	}
	return data, nil
}

func (d *Device) ReadSensors() (map[string]map[string]any, error) {
	out := make(map[string]map[string]any)
	for id, s := range d.sensors {
		data, err := s.Read()
		if err != nil {
			d.totalErrors.Add(1)
			out[id] = map[string]any{"error": err.Error()}
			continue
		}
		out[id] = data
	}
	return out, nil
}

func (d *Device) recoverI2C(bus int) error {
	return d.exec.i2cRecover(bus)
}

func (d *Device) autoDiscoverSensors() {
	buses, err := d.exec.i2cListBuses()
	if err != nil || len(buses) == 0 {
		buses = []int{1}
	}

	knownAddresses := []string{"0x76", "0x77"}

	for _, bus := range buses {
		for _, addr := range knownAddresses {
			data, err := d.I2CReadRegister(bus, addr, 0xD0, 1)
			if err != nil || len(data) != 1 {
				continue
			}

			switch data[0] {
			case 0x58:
				id := fmt.Sprintf("bmp280_%d_%s", bus, addr)
				if _, exists := d.sensors[id]; !exists {
					d.sensors[id] = newBMP280Driver(d, id, bus, addr)
					d.log.Info("auto-discovered sensor",
						zap.String("id", id),
						zap.String("type", "bmp280"),
						zap.Int("bus", bus),
						zap.String("address", addr),
					)
				}
			}
		}
	}
}

func (d *Device) Health() config.HealthStatus {
	poolSize := 0
	if d.pool != nil {
		poolSize = d.pool.Size()
	}
	activeWatchers := 0
	if d.watch != nil {
		activeWatchers = d.watch.ActiveCount()
	}
	return config.HealthStatus{
		Connected:      d.started && (d.cfg.Config.Mode == "local" || poolSize > 0),
		Reconnects:     int(d.reconnects.Load()),
		ActiveWatchers: activeWatchers,
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
