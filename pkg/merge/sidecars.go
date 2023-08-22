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
	}
	DefaultAttacher = SidecarConfig{
		TemplateAssetName: AttacherAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "attacher-m",
	}
	DefaultSnapshotter = SidecarConfig{
		TemplateAssetName: SnapshotterAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "snapshotter-m",
	}
	DefaultResizer = SidecarConfig{
		TemplateAssetName: ResizerAssetName,
		ExtraArguments:    nil,
		HasMetricsPort:    true,
		MetricPortName:    "resizer-m",
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

func (cfg SidecarConfig) WithExtraArguments(extraArguments []string) SidecarConfig {
	newCfg := cfg
	newCfg.ExtraArguments = extraArguments
	return newCfg
}
