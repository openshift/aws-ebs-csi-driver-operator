package aws

import (
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
)

func GetAWSEBSConfig() (*merge.CSIDriverOperatorConfig, error) {
	cfg := &merge.CSIDriverOperatorConfig{
		AssetPrefix:      "aws-ebs-csi-driver",
		AssetShortPrefix: "ebs",
		ControllerConfig: &merge.ControllerConfig{
			DeploymentTemplateAssetName: "controller-template.yaml",
			LivenessProbePort:           10301,
			MetricsPorts: []merge.MetricsPort{
				{
					LocalPort:           8201,
					InjectKubeRBACProxy: true,
					ExposedPort:         9201,
					Name:                "driver-m",
				},
			},
			SidecarLocalMetricsPortStart:   8202,
			SidecarExposedMetricsPortStart: 9202,
			Sidecars: []merge.SidecarConfig{
				merge.DefaultProvisionerWithSnapshots.WithExtraArguments(
					"--default-fstype=ext4",
					"--feature-gates=Topology=true",
					"--extra-create-metadata=true",
					"--timeout=60s",
				),
				merge.DefaultAttacher.WithExtraArguments(
					"--timeout=60s",
				),
				merge.DefaultResizer.WithExtraArguments(
					"--timeout=300s",
				),
				merge.DefaultSnapshotter.WithExtraArguments(
					"--timeout=300s",
					"--extra-create-metadata",
				),
				merge.DefaultLivenessProbe.WithExtraArguments(
					"--probe-timeout=3s",
				),
			},
			StaticAssetNames: merge.DefaultControllerAssetNames,
		},
		NodeConfig: &merge.NodeConfig{
			MetricsPorts:               nil,
			LivenessProbePort:          10301,
			DaemonSetTemplateAssetName: "node-template.yaml",
			StaticAssetNames:           merge.DefaultNodeAssetNames,
		},
	}
	return cfg, nil
}
