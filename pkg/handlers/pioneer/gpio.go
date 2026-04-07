package pioneer

import (
	"fmt"

	"github.com/EraldCaka/PIoneer/pkg/config"
)

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

func (d *Device) StopWatch(pin int) {
	d.watch.StopWatch(pin)
}
