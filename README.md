# PIoneer

A Go library for controlling Raspberry Pi GPIO pins remotely over SSH. Supports digital pins, PWM, and I2C with an event-driven architecture and optional MQTT integration.

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
    "github.com/EraldCaka/PIoneer/pkg/handlers/pioneer"
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

    // watch for changes
    events, _ := device.Watch(5)
    for event := range events {
        log.Printf("pin %d changed: %d → %d", event.Pin, event.OldValue, event.NewValue)
    }
}
```

---

## Configuration

```yaml
config:
  device-name: "pi"
  url: "raspberry.local"
  port: "22"
  auth-method: "password"   # or "key"
  password: "yourpassword"
  # ssh-key-path: "/home/user/.ssh/id_rsa"
  pool-size: 3
  max-retries: 5
  retry-delay: 3

chip:
  name: "gpiochip512"
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

# optional
mqtt:
  broker: "tcp://192.168.1.x:1883"
  client-id: "pioneer-pi"
  topic: "pioneer"
  use-tls: false
  qos: 1
```

---

## Protocols

### Digital

```go
device.Read(pin)           // returns 0 or 1
device.Write(pin, value)   // value must be 0 or 1
```

### PWM

Hardware PWM pins on Pi 4: **12, 13, 18, 19**. Requires `pigpiod` running on the Pi.

```go
device.SetDutyCycle(18, 75.0)   // 0.0 - 100.0
device.GetDutyCycle(18)
device.StopPWM(18)
```

### I2C

```go
device.I2CWrite(1, "0x48", []byte{0x01, 0xFF})
device.I2CRead(1, "0x48", 2)
```

### Events

```go
events, _ := device.Watch(5)
defer device.StopWatch(5)

for event := range events {
    fmt.Printf("pin %d: %d → %d\n", event.Pin, event.OldValue, event.NewValue)
}
```

---

## MQTT

When MQTT is configured, PIoneer automatically publishes state changes and subscribes to control topics.

**Published state:**
```
pioneer/gpio/<pin>/state    {"pin":3,"value":1,"label":"HIGH","direction":"output","timestamp":...}
pioneer/pwm/<pin>/state     {"pin":18,"duty_cycle":50.0,"frequency_hz":1000,"timestamp":...}
pioneer/i2c/<bus>/<addr>/state  {"bus":1,"address":"0x48","data":[...],"hex":"01ff","timestamp":...}
pioneer/device/status       {"device":"pi","status":"online","ssh_pool_size":3,"reconnects":0}
pioneer/device/error        {"protocol":"gpio","error":"...","timestamp":...}
```

**Control topics:**
```
pioneer/gpio/<pin>/set      → "1" or "0"
pioneer/gpio/<pin>/get      → (empty, triggers read)
pioneer/pwm/<pin>/set       → "50.0"
pioneer/pwm/<pin>/stop      → (empty)
pioneer/i2c/write           → {"bus":1,"address":"0x48","data":[1,255]}
pioneer/i2c/read            → {"bus":1,"address":"0x48","length":2}
pioneer/device/ping         → (empty, triggers status response)
```

---

## Config Reference

**config:**
| Field | Type | Description |
|---|---|---|
| device-name | string | Name of the device |
| url | string | IP address of the Pi |
| port | string | SSH port (usually 22) |
| auth-method | string | `password` or `key` |
| password | string | SSH password |
| ssh-key-path | string | Path to private key |
| pool-size | int | Concurrent SSH connections (default 3) |
| max-retries | int | Retries on failure (default 5) |
| retry-delay | int | Seconds between retries (default 3) |

**digital pin:**
| Field | Type | Description |
|---|---|---|
| id | string | Unique identifier |
| pin | int | GPIO pin number (1-53) |
| value | int | Default value: 0 or 1 |
| direction | int | 0=input, 1=output |
| edge | int | 0=none, 1=rising, 2=falling, 3=both |

**pwm pin:**
| Field | Type | Description |
|---|---|---|
| id | string | Unique identifier |
| pin | int | Hardware PWM pin (12, 13, 18, 19) |
| frequency | int | Frequency in Hz |
| duty-cycle | float | Initial duty cycle (0-100) |

**i2c device:**
| Field | Type | Description |
|---|---|---|
| id | string | Unique identifier |
| bus | int | I2C bus number (0 or 1) |
| address | string | Device address e.g. `0x48` |

---

## Pi Setup

Enable I2C and install required packages:

```bash
sudo apt install -y i2c-tools pigpio
sudo raspi-config nonint do_i2c 0
sudo systemctl enable pigpiod && sudo systemctl start pigpiod
echo "pi ALL=(ALL) NOPASSWD: /usr/bin/pinctrl" | sudo tee /etc/sudoers.d/pinctrl
```

---

## Health & Metrics

```go
health := device.Health()
// health.Connected, health.Reconnects, health.ActiveWatchers, health.MQTTBound

metrics := device.Metrics()
// metrics.TotalReads, metrics.TotalWrites, metrics.TotalErrors, metrics.SSHPoolSize
```
