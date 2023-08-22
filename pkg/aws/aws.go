package aws

import (
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
)

func GetAWSEBSConfig() (*merge.CSIDriverOperatoConfig, error) {
	cfg := &merge.CSIDriverOperatoConfig{
		AssetPrefix: "aws-ebs-csi-driver",
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
				merge.DefaultProvisioner.WithExtraArguments([]string{"--default-fstype=ext4",
					"--feature-gates=Topology=true",
					"--extra-create-metadata=true",
					"--timeout=60s",
				}),
				merge.DefaultAttacher.WithExtraArguments([]string{"--timeout=60s"}),
				merge.DefaultResizer.WithExtraArguments([]string{"--timeout=300s"}),
				merge.DefaultSnapshotter.WithExtraArguments([]string{
					"--timeout=300s",
					"--extra-create-metadata",
				}),
				merge.DefaultLivenessProbe.WithExtraArguments([]string{
					"--probe-timeout=3s",
				}),
			},
		},
		NodeConfig: &merge.NodeConfig{
			MetricsPorts:               nil,
			LivenessProbePort:          10301,
			DaemonSetTemplateAssetName: "node-template.yaml",
		},
	}
	return cfg, nil
}
