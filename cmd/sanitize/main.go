package main

import (
	"flag"
	"os"

	"sigs.k8s.io/yaml"
)

func main() {
	flag.Parse()

	for _, arg := range flag.Args() {
		data, err := os.ReadFile(arg)
		if err != nil {
			panic(err)
		}

		var y interface{}
		err = yaml.Unmarshal(data, &y)
		if err != nil {
			panic(err)
		}

		newData, err := yaml.Marshal(y)
		if err != nil {
			panic(err)
		}
		err = os.WriteFile(arg, newData, 0644)
		if err != nil {
			panic(err)
		}
	}
}
