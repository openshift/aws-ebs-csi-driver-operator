package assets

import (
	"embed"
)

//go:embed *.yaml hypershift/*.yaml rbac/*.yaml base/*.yaml base/rbac/*.yaml patches/sidecars/*.yaml patches/metrics/*.yaml patches/standalone/*.yaml patches/hypershift/*.yaml drivers/aws-ebs/*.yaml drivers/aws-ebs/patches/*.yaml
var f embed.FS

// ReadFile reads and returns the content of the named file.
func ReadFile(name string) ([]byte, error) {
	return f.ReadFile(name)
}
