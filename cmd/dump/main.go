package main

import (
	"flag"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/aws-ebs"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
)

func main() {
	flavour := flag.String("flavour", "standalone", "cluster flavour")
	path := flag.String("path", "", "path to save assets")

	flag.Parse()

	cfg := aws_ebs.GetAWSEBSGeneratorConfig()

	rcfg := &merge.RuntimeConfig{
		ClusterFlavour: merge.ClusterFlavour(*flavour),
	}
	gen := merge.NewAssetGenerator(rcfg, cfg)
	a, err := gen.GenerateAssets()
	if err != nil {
		panic(err)
	}

	if err := a.Save(*path); err != nil {
		panic(err)
	}
}
