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
	DefaultControllerAssets = NewAssets("",
		"base/cabundle_cm.yaml",
		"base/controller_sa.yaml",
		"base/controller_pdb.yaml",
		"base/rbac/kube_rbac_proxy_role.yaml",
		"base/rbac/kube_rbac_proxy_binding.yaml",
		"base/rbac/lease_leader_election_role.yaml",
		"base/rbac/lease_leader_election_binding.yaml",
		"base/rbac/prometheus_role.yaml",
		"base/rbac/prometheus_binding.yaml",
	)
	DefaultNodeAssets = NewAssets("",
		"base/node_sa.yaml",
		"base/rbac/privileged_role.yaml",
		"base/rbac/privileged_role_binding.yaml",
	)

	DefaultAssetPatches = NewAssetPatches(FlavourStandalone,
		"controller.yaml", "patches/standalone/controller_proxy.yaml",
		"controller.yaml", "patches/standalone/controller_affinity.yaml",
	).WithPatches(FlavourHyperShift,
		"controller_sa.yaml", "patches/hypershift/controller_sa_pull_secret.yaml",
		"controller.yaml", "patches/hypershift/controller_affinity.yaml",
	)
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

	DefaultControllerAssetPatches = map[ClusterFlavour][]AssetPatch{
		FlavourHyperShift: {},
		FlavourStandalone: {},
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

func (a Assets) WithAssets(flavour ClusterFlavour, assets ...string) Assets {
	newAssets := make([]Asset, 0, len(a)+len(assets))
	newAssets = append(newAssets, a...)
	pFlavour := &flavour
	if flavour == "" {
		pFlavour = nil
	}
	for _, assetName := range assets {
		newAssets = append(newAssets, Asset{
			ClusterFlavour: pFlavour,
			AssetName:      assetName,
		})
	}
	return newAssets
}

func (p AssetPatches) WithPatches(flavour ClusterFlavour, namePatchPairs ...string) AssetPatches {
	if len(namePatchPairs)%2 != 0 {
		panic("namePatchPairs must be even")
	}
	newPatches := make([]AssetPatch, 0, len(p)+len(namePatchPairs)/2)
	newPatches = append(newPatches, p...)
	pFlavour := &flavour
	if flavour == "" {
		pFlavour = nil
	}
	for i := 0; i < len(namePatchPairs); i += 2 {
		newPatches = append(newPatches, AssetPatch{
			ClusterFlavour: pFlavour,
			Name:           namePatchPairs[i],
			PatchAssetName: namePatchPairs[i+1],
		})
	}
	return newPatches
}

func NewAssets(flavour ClusterFlavour, assets ...string) Assets {
	return Assets{}.WithAssets(flavour, assets...)
}

func NewAssetPatches(flavour ClusterFlavour, namePatchPairs ...string) AssetPatches {
	return AssetPatches{}.WithPatches(flavour, namePatchPairs...)
}
