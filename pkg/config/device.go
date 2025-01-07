package config

type Device interface {
	Name() string
	Start() error
	Stop() error
	Read(pin int, isAnalog bool) (int, error)
	Write(pin int, value int, isAnalog bool) error
}
