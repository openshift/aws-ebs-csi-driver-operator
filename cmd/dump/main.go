package main

import (
	"os"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/aws"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
	"k8s.io/klog/v2"
)

func main() {
	cfg, err := aws.GetAWSEBSConfig()
	if err != nil {
		panic(err)
	}
	a, err := merge.GenerateAssets(merge.FlavourStandalone, cfg)
	if err != nil {
		panic(err)
	}

	dumpYaml("controller.yaml", a.ControllerTemplate)
	for k, v := range a.ControllerStaticResources {
		dumpYaml(k, v)
	}
}

func dumpYaml(filename string, content []byte) error {
	if content == nil {
		klog.Infof("%s not set", filename)
		return nil
	}
	klog.Infof("dumping %s", filename)
	return os.WriteFile(filename, content, 0644)
}
