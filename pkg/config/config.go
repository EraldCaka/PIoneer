package config

type Device interface {
	Name() string
	Start() error
	Stop() error
	Read(pin int, isAnalog bool) (int, error)
	Write(pin int, value int, isAnalog bool) error
}

type Chip struct {
	DigitalPins []Digital `yaml:"digital-pins"`
}

type DeviceConfig struct {
	Config     Config   `yaml:"config"`
	Chip       Chip     `yaml:"chip"`
	AnalogPins []Analog `yaml:"analog-pins"`
}

type Config struct {
	Name     string `yaml:"name"`
	Url      string `yaml:"url"`
	Password string `yaml:"password"`
	Port     string `yaml:"port"`
}

type Analog struct {
	PinDefault `yaml:",inline"`
}

type Digital struct {
	PinDefault `yaml:",inline"`
}

type PinDefault struct {
	Id    string `yaml:"id"`
	Pin   int    `yaml:"pin"`
	Value int    `yaml:"value"`
	Mode  int    `yaml:"mode"`
	Pull  int    `yaml:"pull"`
}
