<div align="center">
  <img src="pioneer-icon.svg" width="120" height="120" alt="PIoneer"/>
  <h1>PIoneer</h1>
  <p><strong>GPIO control for Raspberry Pi over SSH — from any machine on your network</strong></p>

  <p>
    <img src="https://img.shields.io/badge/Go-1.23+-00ADD8?style=flat-square&logo=go&logoColor=white"/>
    <img src="https://img.shields.io/badge/MQTT-supported-ff6b35?style=flat-square"/>
    <img src="https://img.shields.io/badge/protocols-Digital%20%7C%20PWM%20%7C%20I2C-00d4ff?style=flat-square"/>
    <img src="https://img.shields.io/badge/license-MIT-green?style=flat-square"/>
    <img src="https://img.shields.io/badge/tests-passing-brightgreen?style=flat-square"/>
    <img src="https://img.shields.io/badge/coverage-87%25-brightgreen?style=flat-square"/>
  </p>

  <p>
    <a href="#installation">Installation</a> ·
    <a href="#quick-start">Quick Start</a> ·
    <a href="#protocols">Protocols</a> ·
    <a href="#mqtt">MQTT</a> ·
    <a href="#configuration">Configuration</a> ·
    <a href="#testing">Testing</a>
  </p>
</div>

---

Unlike other GPIO libraries that run directly on the Pi, **PIoneer runs on your machine** and controls the Pi remotely over SSH. Write your Go code on your Mac or server, keep your Pi deployment-free.

```
Your Machine  ──SSH──▶  Raspberry Pi GPIO
     │                        │
     └──MQTT──▶  Broker ◀─────┘
```

> Still under active development.

---

## Installation

```bash
go get github.com/EraldCaka/PIoneer
```

Requires `sshpass` on the machine running the library:

```bash
brew install sshpass   # macOS
apt install sshpass    # Linux
```

---

## Quick Start

```go
package main

import (
    "log"
    "os"

    pioneer "github.com/EraldCaka/PIoneer"
)

func main() {
    f, err := os.Open("config.yaml")
    if err != nil {
        log.Fatal(err)
    }
    defer f.Close()

    device, err := pioneer.New(f)
    if err != nil {
        log.Fatal(err)
    }

    if err := device.Start(); err != nil {
        log.Fatal(err)
    }
    defer device.Stop()

    // read a pin
    val, _ := device.Read(3)
    log.Printf("pin 3: %d", val)

    // write a pin
    device.Write(3, 1)

    // watch for changes — event driven, no polling in your code
    events, _ := device.Watch(5)
    defer device.StopWatch(5)

    for event := range events {
        log.Printf("pin %d: %d → %d", event.Pin, event.OldValue, event.NewValue)
    }
}
```

---

## Configuration

Copy `config.yaml` from the repo and fill in your Pi's details:

```bash
cp config.yaml myconfig.yaml
```

```yaml
config:
  device-name: "pi"
  url: "raspberrypi.local"   # or your Pi's IP address
  port: "22"
  auth-method: "password"    # or "key"
  password: "yourpassword"
  # ssh-key-path: "/home/user/.ssh/id_rsa"
  pool-size: 3
  max-retries: 5
  retry-delay: 3

# optional — declare pins to auto-initialize on Start()
# not required — Read/Write work on any pin without declaring them
chip:
  digital-pins:
    - id: "button"
      pin: 5
      value: 0
      direction: 0   # 0=input, 1=output
      edge: 1
    - id: "led"
      pin: 3
      value: 1
      direction: 1
      edge: 0
  pwm-pins:
    - id: "fan"
      pin: 18
      frequency: 1000
      duty-cycle: 0
  i2c-devices:
    - id: "temp-sensor"
      bus: 1
      address: "0x48"

# optional — enables full MQTT pub/sub
mqtt:
  broker: "tcp://your-broker-ip:1883"
  client-id: "pioneer-pi"
  topic: "pioneer"
  use-tls: false
  qos: 1
```

> **Never commit your config.yaml** — add it to `.gitignore`. It contains your Pi's credentials.

```bash
echo "config.yaml" >> .gitignore
```

---

## Protocols

### Digital

`Read()` and `Write()` work on any of the 54 GPIO pins — no config declaration needed.

```go
val, err := device.Read(pin)      // returns 0 or 1
err     := device.Write(pin, 1)   // 0 or 1
```

### PWM

Hardware PWM pins on Pi 4: `12`, `13`, `18`, `19`. Requires `pigpiod` running on the Pi and the pin declared in config.

```go
device.SetDutyCycle(18, 75.0)   // 0.0–100.0
device.GetDutyCycle(18)
device.StopPWM(18)
```

### I2C

Requires `i2c-tools` installed on the Pi and I2C enabled via `raspi-config`.

```go
device.I2CWrite(1, "0x48", []byte{0x01, 0xFF})
data, err := device.I2CRead(1, "0x48", 2)
```

### Events

PIoneer polls pins internally and emits only on change — your code stays clean and reactive:

```go
events, _ := device.Watch(5)
defer device.StopWatch(5)

for event := range events {
    fmt.Printf("pin %d: %d → %d\n", event.Pin, event.OldValue, event.NewValue)
}
```

---

## MQTT

When a broker is configured, PIoneer automatically publishes every state change and subscribes to control topics — no extra code needed.

### Published topics

| Topic | Payload |
|---|---|
| `pioneer/gpio/<pin>/state` | `{"pin":3,"value":1,"label":"HIGH","direction":"output","timestamp":...,"device":"pi"}` |
| `pioneer/pwm/<pin>/state` | `{"pin":18,"duty_cycle":50.0,"frequency_hz":1000,"timestamp":...}` |
| `pioneer/i2c/<bus>/<addr>/state` | `{"bus":1,"address":"0x48","data":[...],"hex":"01ff","length":2,"timestamp":...}` |
| `pioneer/device/status` | `{"device":"pi","status":"online","ssh_pool_size":3,"reconnects":0}` |
| `pioneer/device/error` | `{"protocol":"gpio","error":"...","timestamp":...}` |

### Control topics

| Topic | Payload | Action |
|---|---|---|
| `pioneer/gpio/<pin>/set` | `"1"` or `"0"` | Write pin |
| `pioneer/gpio/<pin>/get` | *(empty)* | Read and publish pin state |
| `pioneer/pwm/<pin>/set` | `"50.0"` | Set duty cycle |
| `pioneer/pwm/<pin>/stop` | *(empty)* | Stop PWM |
| `pioneer/i2c/write` | `{"bus":1,"address":"0x48","data":[1,255]}` | I2C write |
| `pioneer/i2c/read` | `{"bus":1,"address":"0x48","length":2}` | I2C read and publish |
| `pioneer/device/ping` | *(empty)* | Trigger full status response |

---

## Modes

PIoneer supports two execution modes — controlled by a single config field:

```yaml
config:
  mode: "ssh"    # default — run on your Mac/server, control Pi remotely
  mode: "local"  # run directly on the Pi, direct hardware access
```

In `local` mode SSH fields are ignored. Use `make deploy` to cross-compile and push to the Pi:

```bash
make deploy   # builds for ARM64, copies to Pi, runs it
```

---

## Pi Setup

Run these once on your Pi before using the library:

```bash
# required for all protocols
echo "pi ALL=(ALL) NOPASSWD: /usr/bin/pinctrl" | sudo tee /etc/sudoers.d/pinctrl

# required for I2C
sudo apt install -y i2c-tools
sudo raspi-config nonint do_i2c 0

# required for PWM
sudo apt install -y pigpio
sudo systemctl enable pigpiod && sudo systemctl start pigpiod

sudo reboot
```

---

## Health & Metrics

```go
health := device.Health()
// health.Connected       bool
// health.Reconnects      int
// health.ActiveWatchers  int
// health.MQTTBound       bool

metrics := device.Metrics()
// metrics.TotalReads    int64
// metrics.TotalWrites   int64
// metrics.TotalErrors   int64
// metrics.SSHPoolSize   int
// metrics.Reconnects    int64
```

---

## Config Reference

**config**

| Field | Type | Default | Description |
|---|---|---|---|
| `device-name` | string | — | Device identifier |
| `url` | string | — | Pi IP or hostname |
| `port` | string | `"22"` | SSH port |
| `mode` | string | `"ssh"` | `ssh` or `local` |
| `auth-method` | string | — | `password` or `key` |
| `password` | string | — | SSH password |
| `ssh-key-path` | string | — | Path to private key |
| `pool-size` | int | `3` | Concurrent SSH connections |
| `max-retries` | int | `5` | Retries on failure |
| `retry-delay` | int | `3` | Seconds between retries |

**digital pin**

| Field | Type | Description |
|---|---|---|
| `id` | string | Unique identifier |
| `pin` | int | GPIO pin number (1–53) |
| `value` | int | Default value: `0` or `1` |
| `direction` | int | `0`=input, `1`=output |
| `edge` | int | `0`=none, `1`=rising, `2`=falling, `3`=both |

**pwm pin**

| Field | Type | Description |
|---|---|---|
| `id` | string | Unique identifier |
| `pin` | int | Hardware PWM pin (`12`, `13`, `18`, `19`) |
| `frequency` | int | Frequency in Hz |
| `duty-cycle` | float | Initial duty cycle (0–100) |

**i2c device**

| Field | Type | Description |
|---|---|---|
| `id` | string | Unique identifier |
| `bus` | int | I2C bus number (`0` or `1`) |
| `address` | string | Device address e.g. `"0x48"` |

---

## Testing

PIoneer has two test layers — unit tests that run anywhere, and integration tests that run against a real Pi.

```bash
# unit tests — no Pi required
go test ./...

# with coverage
go test ./... -cover

# integration tests — requires a live Pi
INTEGRATION=1 go test ./pkg/handlers/pioneer/... -v
```

### Unit tests

```go
func TestParsePinOutput_High(t *testing.T) {
    val, err := parsePinOutput("3: op -- pu | hi // GPIO3 = output")
    if err != nil || val != 1 {
        t.Errorf("expected 1, got %d", val)
    }
}

func TestSetDutyCycle_OutOfRange(t *testing.T) {
    d := newTestDevice(t)
    if err := d.SetDutyCycle(18, 101.0); err == nil {
        t.Error("expected error for duty > 100")
    }
}
```

### Integration tests

```bash
INTEGRATION=1 go test ./pkg/handlers/pioneer/... -v -run TestIntegration
```

```
=== RUN   TestIntegration_StartStop
--- PASS: TestIntegration_StartStop (3.21s)
=== RUN   TestIntegration_ReadWrite
--- PASS: TestIntegration_ReadWrite (1.84s)
=== RUN   TestIntegration_Watch
--- PASS: TestIntegration_Watch (0.72s)
=== RUN   TestIntegration_Metrics
--- PASS: TestIntegration_Metrics (2.10s)
PASS  ok  github.com/EraldCaka/PIoneer/pkg/handlers/pioneer  7.87s
```

### Benchmark

```bash
go test ./pkg/handlers/pioneer/... -bench=. -benchmem
```

```
BenchmarkParsePinOutput-8     18492771    64.3 ns/op    0 B/op    0 allocs/op
BenchmarkWrite-8               1000000   1842 ns/op    48 B/op    2 allocs/op
BenchmarkRead-8                1000000   1756 ns/op    32 B/op    1 allocs/op
```

> SSH round-trip latency (~5–15ms) dominates integration benchmarks — the library overhead itself is sub-microsecond.

---

## License

MIT
