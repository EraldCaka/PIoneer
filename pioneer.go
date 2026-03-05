package PIoneer

import (
	"os"

	"github.com/EraldCaka/PIoneer/pkg/config"
	"github.com/EraldCaka/PIoneer/pkg/handlers/pioneer"
)

type Device = config.Device

func New(file *os.File) (Device, error) {
	return pioneer.New(file)
}
