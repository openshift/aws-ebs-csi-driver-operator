package merge

import (
	sigyaml "sigs.k8s.io/yaml"
)

// Sanitize reorders YAML files to a canonical order, so they can be compared easily with `diff`.
func Sanitize(src string) (string, error) {
	var obj interface{}
	sigyaml.Unmarshal([]byte(src), &obj)
	bytes, err := sigyaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func MustSanitize(src string) string {
	s, err := Sanitize(src)
	if err != nil {
		panic(err)
	}
	return s
}
