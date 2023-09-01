package generated_assets

import (
	sigyaml "sigs.k8s.io/yaml"
)

// Sanitize reorders YAML files to a canonical order, so they can be compared easily with `diff`.
func Sanitize(src []byte) ([]byte, error) {
	var obj interface{}
	sigyaml.Unmarshal(src, &obj)
	bytes, err := sigyaml.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

func MustSanitize(src []byte) []byte {
	s, err := Sanitize(src)
	if err != nil {
		panic(err)
	}
	return s
}
