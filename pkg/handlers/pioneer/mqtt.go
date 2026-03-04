package pioneer

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

type GPIOStatePayload struct {
	Pin       int    `json:"pin"`
	Value     int    `json:"value"`
	Label     string `json:"label"`
	Direction string `json:"direction"`
	Timestamp int64  `json:"timestamp"`
	Device    string `json:"device"`
}

type PWMStatePayload struct {
	Pin         int     `json:"pin"`
	DutyCycle   float64 `json:"duty_cycle"`
	FrequencyHz int     `json:"frequency_hz"`
	Timestamp   int64   `json:"timestamp"`
	Device      string  `json:"device"`
}

type I2CStatePayload struct {
	Bus       int    `json:"bus"`
	Address   string `json:"address"`
	Data      []byte `json:"data"`
	Hex       string `json:"hex"`
	Length    int    `json:"length"`
	Timestamp int64  `json:"timestamp"`
	Device    string `json:"device"`
}

type DeviceStatusPayload struct {
	Device     string `json:"device"`
	Status     string `json:"status"`
	Timestamp  int64  `json:"timestamp"`
	SSHPool    int    `json:"ssh_pool_size"`
	Reconnects int64  `json:"reconnects"`
}

type ErrorPayload struct {
	Device    string `json:"device"`
	Protocol  string `json:"protocol"`
	Pin       int    `json:"pin,omitempty"`
	Bus       int    `json:"bus,omitempty"`
	Address   string `json:"address,omitempty"`
	Error     string `json:"error"`
	Timestamp int64  `json:"timestamp"`
}

type mqttBridge struct {
	client pahomqtt.Client
	cfg    *config.MQTT
	device *Device
	log    *zap.Logger
	bound  atomic.Bool
}

func newMQTTBridge(cfg *config.MQTT, device *Device, log *zap.Logger) (*mqttBridge, error) {
	opts := pahomqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)
	opts.SetAutoReconnect(true)
	opts.SetCleanSession(false)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetPingTimeout(10 * time.Second)

	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	if cfg.UseTLS {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to build TLS config: %v", err)
		}
		opts.SetTLSConfig(tlsCfg)
	}

	bridge := &mqttBridge{
		cfg:    cfg,
		device: device,
		log:    log,
	}

	opts.SetOnConnectHandler(func(c pahomqtt.Client) {
		log.Info("MQTT connected", zap.String("broker", cfg.Broker))
		bridge.PublishStatus("online")
	})
	opts.SetConnectionLostHandler(func(c pahomqtt.Client, err error) {
		log.Warn("MQTT connection lost", zap.Error(err))
	})

	willPayload, _ := json.Marshal(DeviceStatusPayload{
		Device:    device.Name(),
		Status:    "offline",
		Timestamp: time.Now().UnixMilli(),
	})
	opts.SetWill(
		fmt.Sprintf("%s/device/status", cfg.Topic),
		string(willPayload),
		cfg.QOS,
		true,
	)

	client := pahomqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("MQTT connect failed: %v", token.Error())
	}
	bridge.client = client

	subscriptions := map[string]pahomqtt.MessageHandler{
		fmt.Sprintf("%s/gpio/+/set", cfg.Topic):  bridge.onGPIOSet,
		fmt.Sprintf("%s/gpio/+/get", cfg.Topic):  bridge.onGPIOGet,
		fmt.Sprintf("%s/pwm/+/set", cfg.Topic):   bridge.onPWMSet,
		fmt.Sprintf("%s/pwm/+/get", cfg.Topic):   bridge.onPWMGet,
		fmt.Sprintf("%s/pwm/+/stop", cfg.Topic):  bridge.onPWMStop,
		fmt.Sprintf("%s/i2c/write", cfg.Topic):   bridge.onI2CWrite,
		fmt.Sprintf("%s/i2c/read", cfg.Topic):    bridge.onI2CRead,
		fmt.Sprintf("%s/device/ping", cfg.Topic): bridge.onPing,
	}

	for topic, handler := range subscriptions {
		if token := client.Subscribe(topic, cfg.QOS, handler); token.Wait() && token.Error() != nil {
			return nil, fmt.Errorf("MQTT subscribe failed for %s: %v", topic, token.Error())
		}
		log.Info("MQTT subscribed", zap.String("topic", topic))
	}

	bridge.bound.Store(true)
	log.Info("MQTT bridge bound",
		zap.String("broker", cfg.Broker),
		zap.String("topic", cfg.Topic),
	)
	return bridge, nil
}

func (b *mqttBridge) onGPIOSet(_ pahomqtt.Client, msg pahomqtt.Message) {
	pin, ok := b.pinFromTopic(msg.Topic())
	if !ok {
		return
	}
	val, err := strconv.Atoi(strings.TrimSpace(string(msg.Payload())))
	if err != nil || (val != 0 && val != 1) {
		b.publishError("gpio", pin, 0, "", fmt.Errorf("invalid value: %s", string(msg.Payload())))
		return
	}
	if err := b.device.Write(pin, val); err != nil {
		b.publishError("gpio", pin, 0, "", err)
		return
	}
	b.log.Info("MQTT GPIO set", zap.Int("pin", pin), zap.Int("value", val))
	b.PublishGPIO(pin, val)
}

func (b *mqttBridge) onGPIOGet(_ pahomqtt.Client, msg pahomqtt.Message) {
	pin, ok := b.pinFromTopic(msg.Topic())
	if !ok {
		return
	}
	val, err := b.device.Read(pin)
	if err != nil {
		b.publishError("gpio", pin, 0, "", err)
		return
	}
	b.log.Info("MQTT GPIO get", zap.Int("pin", pin), zap.Int("value", val))
	b.PublishGPIO(pin, val)
}

func (b *mqttBridge) onPWMSet(_ pahomqtt.Client, msg pahomqtt.Message) {
	pin, ok := b.pinFromTopic(msg.Topic())
	if !ok {
		return
	}
	duty, err := strconv.ParseFloat(strings.TrimSpace(string(msg.Payload())), 64)
	if err != nil {
		b.publishError("pwm", pin, 0, "", fmt.Errorf("invalid duty: %s", string(msg.Payload())))
		return
	}
	if err := b.device.SetDutyCycle(pin, duty); err != nil {
		b.publishError("pwm", pin, 0, "", err)
		return
	}
	b.log.Info("MQTT PWM set", zap.Int("pin", pin), zap.Float64("duty", duty))
	b.PublishPWM(pin, duty)
}

func (b *mqttBridge) onPWMGet(_ pahomqtt.Client, msg pahomqtt.Message) {
	pin, ok := b.pinFromTopic(msg.Topic())
	if !ok {
		return
	}
	duty, err := b.device.GetDutyCycle(pin)
	if err != nil {
		b.publishError("pwm", pin, 0, "", err)
		return
	}
	b.log.Info("MQTT PWM get", zap.Int("pin", pin), zap.Float64("duty", duty))
	b.PublishPWM(pin, duty)
}

func (b *mqttBridge) onPWMStop(_ pahomqtt.Client, msg pahomqtt.Message) {
	pin, ok := b.pinFromTopic(msg.Topic())
	if !ok {
		return
	}
	if err := b.device.StopPWM(pin); err != nil {
		b.publishError("pwm", pin, 0, "", err)
		return
	}
	b.log.Info("MQTT PWM stopped", zap.Int("pin", pin))
	b.PublishPWM(pin, 0)
}

type I2CWriteRequest struct {
	Bus     int    `json:"bus"`
	Address string `json:"address"`
	Data    []byte `json:"data"`
}

type I2CReadRequest struct {
	Bus     int    `json:"bus"`
	Address string `json:"address"`
	Length  int    `json:"length"`
}

func (b *mqttBridge) onI2CWrite(_ pahomqtt.Client, msg pahomqtt.Message) {
	var req I2CWriteRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		b.publishError("i2c", 0, req.Bus, req.Address, fmt.Errorf("invalid JSON: %v", err))
		return
	}
	if err := b.device.I2CWrite(req.Bus, req.Address, req.Data); err != nil {
		b.publishError("i2c", 0, req.Bus, req.Address, err)
		return
	}
	b.log.Info("MQTT I2C write", zap.Int("bus", req.Bus), zap.String("addr", req.Address))
	b.PublishI2C(req.Bus, req.Address, req.Data)
}

func (b *mqttBridge) onI2CRead(_ pahomqtt.Client, msg pahomqtt.Message) {
	var req I2CReadRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		b.publishError("i2c", 0, req.Bus, req.Address, fmt.Errorf("invalid JSON: %v", err))
		return
	}
	data, err := b.device.I2CRead(req.Bus, req.Address, req.Length)
	if err != nil {
		b.publishError("i2c", 0, req.Bus, req.Address, err)
		return
	}
	b.log.Info("MQTT I2C read", zap.Int("bus", req.Bus), zap.String("addr", req.Address))
	b.PublishI2C(req.Bus, req.Address, data)
}

func (b *mqttBridge) onPing(_ pahomqtt.Client, _ pahomqtt.Message) {
	b.PublishStatus("online")
}

func (b *mqttBridge) PublishGPIO(pin, value int) {
	if !b.bound.Load() {
		return
	}
	direction := "output"
	if p, ok := b.device.pins[pin]; ok && p.Direction == config.INPUT {
		direction = "input"
	}
	label := "LOW"
	if value == 1 {
		label = "HIGH"
	}
	payload := GPIOStatePayload{
		Pin:       pin,
		Value:     value,
		Label:     label,
		Direction: direction,
		Timestamp: time.Now().UnixMilli(),
		Device:    b.device.Name(),
	}
	b.publish(fmt.Sprintf("%s/gpio/%d/state", b.cfg.Topic, pin), payload, false)
}

func (b *mqttBridge) PublishPWM(pin int, duty float64) {
	if !b.bound.Load() {
		return
	}
	freqHz := 0
	if p, ok := b.device.pwmPins[pin]; ok {
		freqHz = p.FrequencyHz
	}
	payload := PWMStatePayload{
		Pin:         pin,
		DutyCycle:   duty,
		FrequencyHz: freqHz,
		Timestamp:   time.Now().UnixMilli(),
		Device:      b.device.Name(),
	}
	b.publish(fmt.Sprintf("%s/pwm/%d/state", b.cfg.Topic, pin), payload, false)
}

func (b *mqttBridge) PublishI2C(bus int, address string, data []byte) {
	if !b.bound.Load() {
		return
	}
	payload := I2CStatePayload{
		Bus:       bus,
		Address:   address,
		Data:      data,
		Hex:       fmt.Sprintf("%x", data),
		Length:    len(data),
		Timestamp: time.Now().UnixMilli(),
		Device:    b.device.Name(),
	}
	b.publish(fmt.Sprintf("%s/i2c/%d/%s/state", b.cfg.Topic, bus, strings.TrimPrefix(address, "0x")), payload, false)
}

func (b *mqttBridge) PublishStatus(status string) {
	if b.client == nil {
		return
	}
	m := b.device.Metrics()
	payload := DeviceStatusPayload{
		Device:     b.device.Name(),
		Status:     status,
		Timestamp:  time.Now().UnixMilli(),
		SSHPool:    m.SSHPoolSize,
		Reconnects: m.Reconnects,
	}
	b.publish(fmt.Sprintf("%s/device/status", b.cfg.Topic), payload, true) // retain=true
}

func (b *mqttBridge) publishError(protocol string, pin, bus int, address string, err error) {
	payload := ErrorPayload{
		Device:    b.device.Name(),
		Protocol:  protocol,
		Pin:       pin,
		Bus:       bus,
		Address:   address,
		Error:     err.Error(),
		Timestamp: time.Now().UnixMilli(),
	}
	b.publish(fmt.Sprintf("%s/device/error", b.cfg.Topic), payload, false)
	b.log.Error("MQTT operation error",
		zap.String("protocol", protocol),
		zap.Int("pin", pin),
		zap.Error(err),
	)
}

func (b *mqttBridge) publish(topic string, payload interface{}, retain bool) {
	data, err := json.Marshal(payload)
	if err != nil {
		b.log.Error("failed to marshal MQTT payload", zap.Error(err))
		return
	}
	b.client.Publish(topic, b.cfg.QOS, retain, data)
	b.log.Debug("MQTT published",
		zap.String("topic", topic),
		zap.String("payload", string(data)),
	)
}

func (b *mqttBridge) Publish(event config.PinEvent) {
	b.PublishGPIO(event.Pin, event.NewValue)
}

func (b *mqttBridge) Close() {
	b.PublishStatus("offline")
	b.bound.Store(false)
	b.client.Disconnect(250)
	b.log.Info("MQTT bridge unbound")
}

func (b *mqttBridge) pinFromTopic(topic string) (int, bool) {
	parts := strings.Split(strings.TrimPrefix(topic, b.cfg.Topic+"/"), "/")
	if len(parts) < 2 {
		b.log.Warn("malformed topic", zap.String("topic", topic))
		return 0, false
	}
	pin, err := strconv.Atoi(parts[1])
	if err != nil {
		b.log.Warn("invalid pin in topic", zap.String("topic", topic))
		return 0, false
	}
	return pin, true
}

func buildTLSConfig(cfg *config.MQTT) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.CAFile != "" {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caCert)
		tlsCfg.RootCAs = pool
	}
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return tlsCfg, nil
}
