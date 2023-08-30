package main

import (
	"flag"
	"os"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/aws"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/clients"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
	"k8s.io/klog/v2"
)

func main() {
	flavour := flag.String("flavour", "standalone", "cluster flavour")

	flag.Parse()

	c := clients.NewFakeClients(clients.CSIDriverNamespace, false)

	cfg, _, err := aws.GetAWSEBSConfig()
	if err != nil {
		panic(err)
	}

	rcfg := &merge.RuntimeConfig{
		ClusterFlavour:        merge.ClusterFlavour(*flavour),
		ControlPlaneNamespace: c.ControlPlaneNamespace,
	}
	gen := merge.NewAssetGenerator(rcfg, cfg)

	a, err := gen.GenerateAssets()
	if err != nil {
		panic(err)
	}

	dumpYAML("controller.yaml", a.ControllerTemplate)
	for k, v := range a.ControllerStaticResources {
		dumpYAML(k, v)
	}
	dumpYAML("node.yaml", a.NodeTemplate)
	for k, v := range a.GuestStorageClassAssets {
		dumpYAML(k, v)
	}
	for k, v := range a.GuestStaticResources {
		dumpYAML(k, v)
	}
}

func dumpYAML(filename string, content []byte) error {
	if content == nil {
		klog.Infof("%s not set", filename)
		return nil
	}
	klog.Infof("dumping %s", filename)
	sanitized := merge.MustSanitize(string(content))
	return os.WriteFile(filename, []byte(sanitized), 0644)
}
