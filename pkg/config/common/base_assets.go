package common

import (
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generator"
)

const (
	ProvisionerAssetName         = "patches/sidecars/provisioner.yaml"
	AttacherAssetName            = "patches/sidecars/attacher.yaml"
	SnapshotterAssetName         = "patches/sidecars/snapshotter.yaml"
	ResizerAssetName             = "patches/sidecars/resizer.yaml"
	LivenessProbeAssetName       = "patches/sidecars/livenessprobe.yaml"
	NodeDriverRegistrarAssetName = "patches/sidecars/node_driver_registrar.yaml"
)

var (
	DefaultControllerAssets = generator.NewAssets(generator.AllFlavours,
		"base/cabundle_cm.yaml",
		"base/controller_sa.yaml",
		"base/controller_pdb.yaml",
	).WithAssets(generator.StandaloneOnly,
		// TODO: figure out metrics in hypershift - it's probably a different Prometheus there
		"base/rbac/kube_rbac_proxy_role.yaml",
		"base/rbac/kube_rbac_proxy_binding.yaml",
		"base/rbac/prometheus_role.yaml",
		"base/rbac/prometheus_binding.yaml",
	)
	DefaultNodeAssets = generator.NewAssets(generator.AllFlavours,
		"base/node_sa.yaml",
		"base/rbac/privileged_role.yaml",
		"base/rbac/node_privileged_binding.yaml",
		// The controller Deployment runs leader election in the GUEST cluster
		"base/rbac/lease_leader_election_role.yaml",
		"base/rbac/lease_leader_election_binding.yaml",
	)

	DefaultAssetPatches = generator.NewAssetPatches(generator.StandaloneOnly,
		"controller.yaml", "patches/standalone/controller_proxy.yaml",
		"controller.yaml", "patches/standalone/controller_affinity.yaml",
	).WithPatches(generator.HyperShiftOnly,
		"controller_sa.yaml", "patches/hypershift/controller_sa_pull_secret.yaml",
		"controller.yaml", "patches/hypershift/controller_affinity.yaml",
	)
)

var (
	DefaultProvisioner = generator.SidecarConfig{
		TemplateAssetName: ProvisionerAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "provisioner-m",
		GuestStaticAssetNames: []string{
			"base/rbac/main_provisioner_binding.yaml",
		},
		AssetPatches: generator.NewAssetPatches(generator.HyperShiftOnly, "sidecar.yaml", "patches/hypershift/sidecar_kubeconfig.yaml.patch"),
	}

	// Provisioner sidecar with restore-from-snapshot support.
	DefaultProvisionerWithSnapshots = DefaultProvisioner.WithAdditionalAssets(
		"base/rbac/volumesnapshot_reader_provisioner_binding.yaml",
	)

	DefaultAttacher = generator.SidecarConfig{
		TemplateAssetName: AttacherAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "attacher-m",
		GuestStaticAssetNames: []string{
			"base/rbac/main_attacher_binding.yaml",
		},
	}
	DefaultSnapshotter = generator.SidecarConfig{
		TemplateAssetName: SnapshotterAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "snapshotter-m",
		GuestStaticAssetNames: []string{
			"base/rbac/main_snapshotter_binding.yaml",
		},
	}
	DefaultResizer = generator.SidecarConfig{
		TemplateAssetName: ResizerAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "resizer-m",
		GuestStaticAssetNames: []string{
			"base/rbac/main_resizer_binding.yaml",
			"base/rbac/storageclass_reader_resizer_binding.yaml",
		},
	}
	DefaultLivenessProbe = generator.SidecarConfig{
		TemplateAssetName: LivenessProbeAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    false,
	}

	DefaultNodeDriverRegistrar = generator.SidecarConfig{
		TemplateAssetName: NodeDriverRegistrarAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    false,
	}

	DefaultControllerAssetPatches = map[generator.ClusterFlavour][]generator.AssetPatch{
		generator.FlavourHyperShift: {},
		generator.FlavourStandalone: {},
	}
)
