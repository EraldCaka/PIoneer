# Go Raspberry Pi GPIO Library

This Go library provides an abstraction for accessing Raspberry Pi GPIO pins via SSH. It supports both **Digital** and **Analog** pins, providing flexible configurations and interaction with your Raspberry Pi devices.

---

## Table of Contents
- [Installation](#installation)
- [Usage](#usage)
  - [Example Configuration](#example-configuration)
- [Config Structs](#config-structs)
  - [DeviceConfig](#deviceconfig-struct)
  - [Config](#config-struct)
  - [Chip](#chip-struct)
  - [Digital](#digital-struct)
  - [Analog](#analog-struct)
- [Example](#example)


---

## Installation

To install the library:

```bash
$ go get github.com/EraldCaka/PIoneer
```

---

## Usage

### Example Configuration

1. Create a \`config.yaml\` file with your Raspberry Pi details:

```yaml
name: "raspberry-pi"
url: "raspberrypi.local"
port: "22"
password: "raspberry"

chip:
  name: "RaspberryPi"
  digital-pins:
    - id: "gpio17"
      pin: 17
      value: 1
      mode: 1
      pull: 0
    - id: "gpio18"
      pin: 18
      value: 0
      mode: 0
      pull: 1

analog-pins:
  - id: "adc0"
    pin: 0
    value: 0
    mode: 0
```

2. Initialize and use the library in your code:

```go
package main

import (
    "log"
    "os"

    "github.com/EraldCaka/PIoneer/pkg/config"
    "github.com/EraldCaka/PIoneer/pkg/pioneer"
)

func main() {
    configFile, err := os.Open("config.yaml")
    if err != nil {
        log.Fatalf("failed to open config file: %v", err)
    }
    defer configFile.Close()

    dev, err := pioneer.New(configFile)
    if err != nil {
        log.Fatalf("failed to initialize device: %v", err)
    }

    err = dev.Start()
    if err != nil {
        log.Fatalf("failed to start the device: %v", err)
    }

    defer dev.Stop()
}
```

---

## Config Structs

### DeviceConfig Struct

| Field     | Type    | Description                                         |
|-----------|---------|-----------------------------------------------------|
| Config     | Config  | Nested Config structure containing SSH credentials  |
| Chip       | Chip    | Configuration for Raspberry Pi Chip                 |
| AnalogPins | []Analog| Slice of Analog Pin configurations                  |

### Config Struct

| Field     | Type    | Description              |
|-----------|---------|--------------------------|
| Name      | string  | Device Name               |
| Url       | string  | Device URL                   |
| Password  | string  | Device Password              |
| Port      | string  | Device Port                  |

### Chip Struct

| Field       | Type        | Description                          |
|-------------|-------------|--------------------------------------|
| Name         | string      | Name of the chip        |
| DigitalPins  | []Digital   | List of Digital Pin configurations   |

### Digital Struct

| Field     | Type    | Description                      |
|-----------|---------|----------------------------------|
| Id        | string  | Pin ID                           |
| Pin       | int     | GPIO Pin Number                  |
| Value     | int     | Current Pin Value                |
| Mode      | int     | Pin Mode (Input/Output)          |
| Pull      | int     | Pin Pull configuration           |

### Analog Struct

| Field     | Type    | Description                      |
|-----------|---------|----------------------------------|
| Id        | string  | Pin ID                           |
| Pin       | int     | GPIO Pin Number                  |
| Value     | int     | Current Pin Value                |
| Mode      | int     | Pin Mode (Input/Output)           |
| Pull      | int     | Pin Pull configuration           |


---

## Example

Check the [example]() directory for a sample implementation using the library.

---
