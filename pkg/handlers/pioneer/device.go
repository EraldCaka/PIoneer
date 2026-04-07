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

func (d *Device) Info() config.DeviceInfo {
	status := "Offline"
	if d.started {
		status = "Online"
	}
	return config.DeviceInfo{
		Name:   d.cfg.Config.Name,
		Mode:   d.cfg.Config.Mode,
		Status: status,
	}
}

func (d *Device) SystemMetrics() (config.SystemMetrics, error) {
	return d.exec.systemMetrics()
}

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

	for id, s := range d.sensors {
		if err := s.Probe(); err != nil {
			d.log.Warn("sensor probe failed", zap.String("sensor", id), zap.Error(err))
			continue
		}
		if err := s.Init(); err != nil {
			d.log.Warn("sensor init failed", zap.String("sensor", id), zap.Error(err))
			continue
		}
		d.log.Info("sensor initialized", zap.String("sensor", id))
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
	if d.exec != nil {
		d.exec.close()
	}
	d.started = false
	d.log.Info("device stopped", zap.String("name", d.Name()))
	_ = d.log.Sync()
	return nil
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
