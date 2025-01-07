package config

type Device interface {
	Name() string
	Start() error
	Stop() error
	Read(pin int) (int, error)
	Write(pin int, value int) error
}

type Pin interface {
	Read(pin int) (int, error)
	Write(pin int, value int) error
}
