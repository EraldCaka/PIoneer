package pioneer

import "fmt"

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

func (d *Device) recoverI2C(bus int) error {
	return d.exec.i2cRecover(bus)
}
