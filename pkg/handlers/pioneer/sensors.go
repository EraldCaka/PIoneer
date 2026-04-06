package pioneer

type sensorDriver interface {
	Name() string
	Probe() error
	Init() error
	Read() (map[string]any, error)
}
