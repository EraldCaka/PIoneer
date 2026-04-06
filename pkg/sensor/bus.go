package sensor

type I2CBus interface {
	I2CProbe(bus int, address string) error
	I2CWrite(bus int, address string, data []byte) error
	I2CRead(bus int, address string, length int) ([]byte, error)
	I2CWriteRegister(bus int, address string, register byte, data []byte) error
	I2CReadRegister(bus int, address string, register byte, length int) ([]byte, error)
}
