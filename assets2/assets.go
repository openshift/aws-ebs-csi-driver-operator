package assets2

import (
	"embed"
)

//go:embed *.yaml hypershift/*.yaml rbac/*.yaml
var f embed.FS

// ReadFile reads and returns the content of the named file.
func ReadFile(name string) ([]byte, error) {
	return f.ReadFile(name)
}
