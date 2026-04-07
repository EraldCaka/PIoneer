package pioneer

import "fmt"

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
		return pwmPin.DutyCycle, fmt.Errorf("get PWM pin %d: %v", pin, err)
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
