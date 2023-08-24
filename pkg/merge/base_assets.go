package merge

const (
	ProvisionerAssetName         = "patches/sidecars/provisioner.yaml"
	AttacherAssetName            = "patches/sidecars/attacher.yaml"
	SnapshotterAssetName         = "patches/sidecars/snapshotter.yaml"
	ResizerAssetName             = "patches/sidecars/resizer.yaml"
	LivenessProbeAssetName       = "patches/sidecars/livenessprobe.yaml"
	NodeDriverRegistrarAssetName = "patches/sidecars/nodedriverregistrar.yaml"
	StandaloneKubeRBACProxy      = "patches/sidecars/kube-rbac-proxy.yaml"
)

var (
	DefaultControllerAssetNames = []string{
		"base/cabundle_cm.yaml",
		"base/controller_sa.yaml",
		"base/controller_pdb.yaml",
		"base/rbac/kube_rbac_proxy_role.yaml",
		"base/rbac/kube_rbac_proxy_binding.yaml",
		"base/rbac/lease_leader_election_role.yaml",
		"base/rbac/lease_leader_election_binding.yaml",
		"base/rbac/prometheus_role.yaml",
		"base/rbac/prometheus_binding.yaml",
	}
	DefaultNodeAssetNames = []string{
		"base/node_sa.yaml",
		"base/rbac/privileged_role.yaml",
		"base/rbac/privileged_role_binding.yaml",
	}
)

var (
	DefaultProvisioner = SidecarConfig{
		TemplateAssetName: ProvisionerAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "provisioner-m",
		StaticAssetNames: []string{
			"base/rbac/main_provisioner_binding.yaml",
		},
	}

	// Provisioner sidecar with restore-from-snapshot support.
	DefaultProvisionerWithSnapshots = DefaultProvisioner.WithAdditionalAssets(
		"base/rbac/volumesnapshot_reader_provisioner_binding.yaml",
	)

	DefaultAttacher = SidecarConfig{
		TemplateAssetName: AttacherAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "attacher-m",
		StaticAssetNames: []string{
			"base/rbac/main_attacher_binding.yaml",
		},
	}
	DefaultSnapshotter = SidecarConfig{
		TemplateAssetName: SnapshotterAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "snapshotter-m",
		StaticAssetNames: []string{
			"base/rbac/main_snapshotter_binding.yaml",
		},
	}
	DefaultResizer = SidecarConfig{
		TemplateAssetName: ResizerAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "resizer-m",
		StaticAssetNames: []string{
			"base/rbac/main_resizer_binding.yaml",
			"base/rbac/storageclass_reader_resizer_binding.yaml",
		},
	}
	DefaultLivenessProbe = SidecarConfig{
		TemplateAssetName: LivenessProbeAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    false,
	}

	DefaultNodeDriverRegistrar = SidecarConfig{
		TemplateAssetName: NodeDriverRegistrarAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    false,
	}

	DefaultStandaloneKubeRBACProxy = SidecarConfig{
		TemplateAssetName: StandaloneKubeRBACProxy,
		ExtraArguments:    nil,
		HasMetricsPort:    false,
	}
)

func (cfg SidecarConfig) WithExtraArguments(extraArguments ...string) SidecarConfig {
	newCfg := cfg
	newCfg.ExtraArguments = extraArguments
	return newCfg
}

func (cfg SidecarConfig) WithAdditionalAssets(assets ...string) SidecarConfig {
	newCfg := cfg
	newCfg.StaticAssetNames = append(newCfg.StaticAssetNames, assets...)
	return newCfg
}
