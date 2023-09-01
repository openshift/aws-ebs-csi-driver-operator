package main

import (
	"flag"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/config/aws-ebs"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generator"
)

func main() {
	flavour := flag.String("flavour", "standalone", "cluster flavour")
	path := flag.String("path", "", "path to save assets")

	flag.Parse()

	cfg := aws_ebs.GetAWSEBSGeneratorConfig()

	rcfg := &generator.RuntimeConfig{
		ClusterFlavour: generator.ClusterFlavour(*flavour),
	}
	gen := generator.NewAssetGenerator(rcfg, cfg)
	a, err := gen.GenerateAssets()
	if err != nil {
		panic(err)
	}

	if err := a.Save(*path); err != nil {
		panic(err)
	}
}
