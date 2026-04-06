package sensor

type Driver interface {
	Name() string
	Probe() error
	Init() error
	Read() (map[string]any, error)
}
