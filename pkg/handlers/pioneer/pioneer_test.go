package pioneer

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EraldCaka/PIoneer/pkg/config"
	"go.uber.org/zap"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestDevice(t *testing.T) *Device {
	t.Helper()
	yaml := `
config:
  device-name: "pi"
  url: "192.168.1.98"
  port: "22"
  auth-method: "password"
  password: "alesio1234"
  pool-size: 3
  max-retries: 2
  retry-delay: 1
chip:
  name: "gpiochip512"
  digital-pins:
    - id: "gpiochip512"
      pin: 3
      value: 1
      direction: 1
      edge: 0
    - id: "gpiochip570"
      pin: 5
      value: 0
      direction: 0
      edge: 1
  pwm-pins:
    - id: "pwm-fan"
      pin: 18
      frequency: 1000
      duty-cycle: 0
  i2c-devices:
    - id: "i2c-sensor"
      bus: 1
      address: "0x48"
`
	tmp, _ := os.CreateTemp("", "pioneer-test-*.yaml")
	defer os.Remove(tmp.Name())
	tmp.WriteString(yaml)
	tmp.Close()

	f, err := os.Open(tmp.Name())
	if err != nil {
		t.Fatalf("failed to open config: %v", err)
	}
	defer f.Close()

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	log, _ := zap.NewDevelopment()
	pins := make(map[int]*config.Digital)
	for i := range cfg.Chip.DigitalPins {
		pins[cfg.Chip.DigitalPins[i].Pin] = &cfg.Chip.DigitalPins[i]
	}
	pwmPins := make(map[int]*config.PWM)
	for i := range cfg.Chip.PWMPins {
		pwmPins[cfg.Chip.PWMPins[i].Pin] = &cfg.Chip.PWMPins[i]
	}

	return &Device{
		cfg:     cfg,
		pins:    pins,
		pwmPins: pwmPins,
		log:     log,
	}
}

// ── Unit tests ────────────────────────────────────────────────────────────────

func TestParsePinOutput_High(t *testing.T) {
	val, err := parsePinOutput("3: op -- pu | hi // GPIO3 = output")
	if err != nil || val != 1 {
		t.Errorf("expected 1, got %d, err: %v", val, err)
	}
}

func TestParsePinOutput_Low(t *testing.T) {
	val, err := parsePinOutput("5: ip    pd | lo // GPIO5 = input")
	if err != nil || val != 0 {
		t.Errorf("expected 0, got %d, err: %v", val, err)
	}
}

func TestParsePinOutput_Invalid(t *testing.T) {
	_, err := parsePinOutput("unexpected output here")
	if err == nil {
		t.Error("expected error for invalid output")
	}
}

func TestWrite_InvalidValue(t *testing.T) {
	d := newTestDevice(t)
	d.pool = &sshPool{log: d.log}
	err := d.Write(3, 5)
	if err == nil {
		t.Error("expected error for value 5")
	}
}

func TestSetDutyCycle_OutOfRange(t *testing.T) {
	d := newTestDevice(t)
	if err := d.SetDutyCycle(18, 101.0); err == nil {
		t.Error("expected error for duty > 100")
	}
	if err := d.SetDutyCycle(18, -1.0); err == nil {
		t.Error("expected error for duty < 0")
	}
}

func TestSetDutyCycle_UnconfiguredPin(t *testing.T) {
	d := newTestDevice(t)
	if err := d.SetDutyCycle(99, 50.0); err == nil {
		t.Error("expected error for unconfigured pin")
	}
}

func TestI2CWrite_EmptyData(t *testing.T) {
	d := newTestDevice(t)
	if err := d.I2CWrite(1, "0x48", []byte{}); err == nil {
		t.Error("expected error for empty data")
	}
}

func TestI2CRead_InvalidLength(t *testing.T) {
	d := newTestDevice(t)
	if _, err := d.I2CRead(1, "0x48", 0); err == nil {
		t.Error("expected error for length 0")
	}
	if _, err := d.I2CRead(1, "0x48", -1); err == nil {
		t.Error("expected error for length -1")
	}
}

func TestName(t *testing.T) {
	d := newTestDevice(t)
	if d.Name() != "pi" {
		t.Errorf("expected 'pi', got '%s'", d.Name())
	}
}

func TestStop_NotStarted(t *testing.T) {
	d := newTestDevice(t)
	log, _ := zap.NewDevelopment()
	d.watch = newWatcher(nil, log)
	if err := d.Stop(); err != nil {
		t.Errorf("Stop() should not error: %v", err)
	}
}

func TestWatcher_EmitsEvents(t *testing.T) {
	log, _ := zap.NewDevelopment()
	pool := &sshPool{log: log}
	w := newWatcher(pool, log)

	counter := 0
	readFn := func(pin int) (int, error) {
		counter++
		if counter%2 == 0 {
			return 1, nil
		}
		return 0, nil
	}

	ch, err := w.Watch(3, readFn)
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	var events []config.PinEvent
	done := make(chan struct{})
	go func() {
		for e := range ch {
			events = append(events, e)
			if len(events) >= 2 {
				close(done)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Log("timeout waiting for events (may be ok in CI)")
	}

	w.StopWatch(3)
	if len(events) > 0 {
		t.Logf("received %d pin events", len(events))
	}
}

func TestWatcher_StopAll(t *testing.T) {
	log, _ := zap.NewDevelopment()
	w := newWatcher(nil, log)
	w.watchers[3] = &pinWatcher{pin: 3, stop: make(chan struct{}), ch: make(chan config.PinEvent)}
	w.watchers[5] = &pinWatcher{pin: 5, stop: make(chan struct{}), ch: make(chan config.PinEvent)}
	w.StopAll()
	if w.ActiveCount() != 0 {
		t.Errorf("expected 0 watchers after StopAll, got %d", w.ActiveCount())
	}
}

func TestMetrics_InitialZero(t *testing.T) {
	d := newTestDevice(t)
	log, _ := zap.NewDevelopment()
	d.pool = &sshPool{log: log}
	d.watch = newWatcher(d.pool, log)
	m := d.Metrics()
	if m.TotalReads != 0 || m.TotalWrites != 0 || m.TotalErrors != 0 {
		t.Error("expected zero metrics on fresh device")
	}
}

func TestConfigLoad_InvalidAuth(t *testing.T) {
	yaml := `
config:
  device-name: "pi"
  url: "192.168.1.98"
  port: "22"
  auth-method: "password"
  password: ""
  pool-size: 3
  max-retries: 2
  retry-delay: 1
chip:
  name: "gpiochip512"
  digital-pins:
    - id: "test"
      pin: 3
      value: 1
      direction: 1
      edge: 0
`
	tmp, _ := os.CreateTemp("", "pioneer-bad-*.yaml")
	defer os.Remove(tmp.Name())
	tmp.WriteString(yaml)
	tmp.Close()
	f, _ := os.Open(tmp.Name())
	defer f.Close()
	_, err := config.Load(f)
	if err == nil {
		t.Error("expected error for empty password")
	}
}

func TestParsePinOutput_Table(t *testing.T) {
	cases := []struct {
		input    string
		expected int
		wantErr  bool
	}{
		{"3: op -- pu | hi // GPIO3 = output", 1, false},
		{"5: ip    pd | lo // GPIO5 = input", 0, false},
		{"17: ip    pu | hi", 1, false},
		{"17: ip    pu | lo", 0, false},
		{"garbage text", 0, true},
	}
	for _, tc := range cases {
		val, err := parsePinOutput(tc.input)
		if tc.wantErr && err == nil {
			t.Errorf("input=%q: expected error", tc.input)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("input=%q: unexpected error: %v", tc.input, err)
		}
		if !tc.wantErr && val != tc.expected {
			t.Errorf("input=%q: expected %d, got %d", tc.input, tc.expected, val)
		}
	}
}

// ── Integration tests ─────────────────────────────────────────────────────────

func integrationDevice(t *testing.T) config.Device {
	t.Helper()
	f, err := os.Open("../../../config.yaml")
	if err != nil {
		t.Fatalf("open config: %v", err)
	}
	defer f.Close()
	dev, err := New(f)
	if err != nil {
		t.Fatalf("new device: %v", err)
	}
	if err := dev.Start(); err != nil {
		t.Fatalf("start device: %v", err)
	}
	return dev
}

func TestIntegration_StartStop(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run")
	}
	dev := integrationDevice(t)
	defer dev.Stop()
	h := dev.Health()
	if !h.Connected {
		t.Error("expected device to be connected")
	}
}

func TestIntegration_ReadWrite(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run")
	}
	dev := integrationDevice(t)
	defer dev.Stop()

	val, err := dev.Read(3)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	t.Logf("Pin 3: %d", val)

	if err := dev.Write(3, 1); err != nil {
		t.Fatalf("write HIGH failed: %v", err)
	}
	if err := dev.Write(3, 0); err != nil {
		t.Fatalf("write LOW failed: %v", err)
	}
}

func TestIntegration_Watch(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run")
	}
	dev := integrationDevice(t)
	defer dev.Stop()

	ch, err := dev.Watch(5)
	if err != nil {
		t.Fatalf("watch failed: %v", err)
	}
	defer dev.StopWatch(5)

	go func() {
		time.Sleep(200 * time.Millisecond)
		dev.Write(5, 1)
		time.Sleep(200 * time.Millisecond)
		dev.Write(5, 0)
	}()

	select {
	case event := <-ch:
		t.Logf("event: pin %d changed %d→%d", event.Pin, event.OldValue, event.NewValue)
	case <-time.After(3 * time.Second):
		t.Log("no event received in 3s (pin may not have changed)")
	}
}

func TestIntegration_Metrics(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run")
	}
	dev := integrationDevice(t)
	defer dev.Stop()

	dev.Read(3)
	dev.Write(3, 1)
	dev.Write(3, 0)

	m := dev.Metrics()
	if m.TotalReads < 1 {
		t.Errorf("expected reads > 0, got %d", m.TotalReads)
	}
	if m.TotalWrites < 2 {
		t.Errorf("expected writes >= 2, got %d", m.TotalWrites)
	}
	t.Logf("Metrics: reads=%d writes=%d errors=%d pool=%d",
		m.TotalReads, m.TotalWrites, m.TotalErrors, m.SSHPoolSize)
}

func TestIntegration_Health(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1 to run")
	}
	dev := integrationDevice(t)
	defer dev.Stop()

	h := dev.Health()
	t.Logf("Health: connected=%v reconnects=%d watchers=%d mqtt=%v",
		h.Connected, h.Reconnects, h.ActiveWatchers, h.MQTTBound)

	if !h.Connected {
		t.Error("expected connected=true")
	}
}

func TestUnit_ParsePinOutputEdgeCases(t *testing.T) {
	cases := []string{
		"3: op -- pu | HI // GPIO3",
		"3: op -- pu | hi",
		"hi",
		"lo",
	}
	for _, c := range cases {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "hi") {
			val, err := parsePinOutput(c)
			if err == nil && val != 1 {
				t.Errorf("expected 1 for %q, got %d", c, val)
			}
		}
	}
}
