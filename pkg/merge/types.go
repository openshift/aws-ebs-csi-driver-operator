package merge

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

type CSIDriverAssetConfig struct {
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
	StaticAssetNames  []string
	AssetPatches      AssetPatches
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
	ClusterFlavour        ClusterFlavour
	ControlPlaneNamespace string
	Replacements          []string
}
