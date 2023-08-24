package merge

const (
	ProvisionerAssetName         = "patches/sidecar/provisioner.yaml"
	AttacherAssetName            = "patches/sidecar/attacher.yaml"
	SnapshotterAssetName         = "patches/sidecar/snapshotter.yaml"
	ResizerAssetName             = "patches/sidecar/resizer.yaml"
	LivenessProbeAssetName       = "patches/sidecar/livenessprobe.yaml"
	NodeDriverRegistrarAssetName = "patches/sidecar/nodedriverregistrar.yaml"
	StandaloneKubeRBACProxy      = "patches/sidecar/kube-rbac-proxy.yaml"
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
