package generator

import (
	"k8s.io/apimachinery/pkg/util/sets"
)

type ClusterFlavour string

const (
	FlavourStandalone ClusterFlavour = "standalone"
	FlavourHyperShift ClusterFlavour = "hypershift"
)

var (
	AllFlavours    = sets.New[ClusterFlavour](FlavourStandalone, FlavourHyperShift)
	StandaloneOnly = sets.New[ClusterFlavour](FlavourStandalone)
	HyperShiftOnly = sets.New[ClusterFlavour](FlavourHyperShift)
)

type Assets []Asset
type Asset struct {
	ClusterFlavours sets.Set[ClusterFlavour]
	AssetName       string
}

type AssetPatches []AssetPatch
type AssetPatch struct {
	ClusterFlavours sets.Set[ClusterFlavour]
	SourceAssetName string
	PatchAssetName  string
}

type CSIDriverGeneratorConfig struct {
	AssetPrefix      string
	AssetShortPrefix string
	DriverName       string
	ControllerConfig *ControlPlaneConfig
	GuestConfig      *GuestConfig
}

type ControlPlaneConfig struct {
	DeploymentTemplateAssetName string
	MetricsPorts                []MetricsPort
	LivenessProbePort           uint16

	SidecarLocalMetricsPortStart   uint16
	SidecarExposedMetricsPortStart uint16
	Sidecars                       []SidecarConfig

	StaticAssets Assets
	AssetPatches AssetPatches
}

type MetricsPort struct {
	LocalPort           uint16
	ExposedPort         uint16
	Name                string
	InjectKubeRBACProxy bool
}

type SidecarConfig struct {
	TemplateAssetName string
	ExtraArguments    []string
	HasMetricsPort    bool
	MetricPortName    string
	// Most sidecars need to add RBAC objects to the *guest* cluster, even if the sidecar runs in the control plane.
	GuestStaticAssetNames []string
	AssetPatches          AssetPatches
}

type GuestConfig struct {
	DaemonSetTemplateAssetName string
	// TODO: add node Service and ServiceMonitor for metrics
	// MetricsPorts               []MetricsPort
	LivenessProbePort uint16
	Sidecars          []SidecarConfig

	StaticAssets Assets
	AssetPatches AssetPatches

	StorageClassAssetNames        []string
	VolumeSnapshotClassAssetNames []string
}

type RuntimeConfig struct {
	ClusterFlavour ClusterFlavour
	Replacements   []string
}

func (cfg SidecarConfig) WithExtraArguments(extraArguments ...string) SidecarConfig {
	newCfg := cfg
	newCfg.ExtraArguments = extraArguments
	return newCfg
}

func (cfg SidecarConfig) WithAdditionalAssets(assets ...string) SidecarConfig {
	newCfg := cfg
	newCfg.GuestStaticAssetNames = append(newCfg.GuestStaticAssetNames, assets...)
	return newCfg
}

func (a Assets) WithAssets(flavours sets.Set[ClusterFlavour], assets ...string) Assets {
	newAssets := make([]Asset, 0, len(a)+len(assets))
	newAssets = append(newAssets, a...)
	for _, assetName := range assets {
		newAssets = append(newAssets, Asset{
			ClusterFlavours: flavours,
			AssetName:       assetName,
		})
	}
	return newAssets
}

func (p AssetPatches) WithPatches(flavours sets.Set[ClusterFlavour], namePatchPairs ...string) AssetPatches {
	if len(namePatchPairs)%2 != 0 {
		panic("namePatchPairs must be even")
	}
	newPatches := make([]AssetPatch, 0, len(p)+len(namePatchPairs)/2)
	newPatches = append(newPatches, p...)
	for i := 0; i < len(namePatchPairs); i += 2 {
		newPatches = append(newPatches, AssetPatch{
			ClusterFlavours: flavours,
			SourceAssetName: namePatchPairs[i],
			PatchAssetName:  namePatchPairs[i+1],
		})
	}
	return newPatches
}

func NewAssets(flavours sets.Set[ClusterFlavour], assets ...string) Assets {
	return Assets{}.WithAssets(flavours, assets...)
}

func NewAssetPatches(flavours sets.Set[ClusterFlavour], namePatchPairs ...string) AssetPatches {
	return AssetPatches{}.WithPatches(flavours, namePatchPairs...)
}
