package aws

import (
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
)

func GetAWSEBSConfig() (*merge.CSIDriverOperatorConfig, error) {
	cfg := &merge.CSIDriverOperatorConfig{
		AssetPrefix:      "aws-ebs-csi-driver",
		AssetShortPrefix: "ebs",

		ControllerConfig: &merge.ControllPlaneConfig{
			DeploymentTemplateAssetName: "drivers/aws-ebs/controller.yaml",
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
			StaticAssets: merge.DefaultControllerAssets,
			AssetPatches: merge.DefaultAssetPatches.WithPatches(merge.HyperShiftOnly,
				"controller.yaml", "drivers/aws-ebs/patches/controller_minter.yaml",
			),
		},

		GuestConfig: &merge.GuestConfig{
			MetricsPorts:               nil,
			LivenessProbePort:          10301,
			DaemonSetTemplateAssetName: "drivers/aws-ebs/node.yaml",
			StaticAssets: merge.DefaultNodeAssets.WithAssets(merge.AllFlavours,
				"drivers/aws-ebs/csidriver.yaml",
				"drivers/aws-ebs/volumesnapshotclass.yaml",
			),
			StorageClassAssetNames: []string{
				"drivers/aws-ebs/storageclass_gp2.yaml",
				"drivers/aws-ebs/storageclass_gp3.yaml",
			},
		},
	}
	return cfg, nil
}

// TODO:
// withHypershiftDeploymentHook
// withHypershiftReplicasHook - from runtime, different on hypershift and standalone
// withNamespaceDeploymentHook - not needed, ${NAMESPACE} could be set from a parameter of GenerateAssets
// WithSecretHashAnnotationHook - generated from a new array in CSIDriverOperatorConfig
// WithObservedProxyDeploymentHook - just add, it's the same on all clouds
// withCustomAWSCABundle - looks like a custom AWS specific code + patch
// withAWSRegion - simple replacement ?
// withCustomTags, withCustomEndPoint - specific hook?
// WithCABundleDeploymentHook - keep it, it's the same on all clouds
// withHypershiftControlPlaneImages - simple env. var replacements on HyperShift
