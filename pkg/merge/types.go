package merge

type CSIDriverOperatorConfig struct {
	AssetPrefix      string
	AssetShortPrefix string
	DriverName       string
	ControllerConfig *ControllPlaneConfig
	GuestConfig      *GuestConfig
}

type Assets []Asset
type Asset struct {
	// TODO: this should be a set of flavours
	ClusterFlavour *ClusterFlavour
	AssetName      string
}

type AssetPatches []AssetPatch
type AssetPatch struct {
	// TODO: this should be a set of flavours
	ClusterFlavour *ClusterFlavour
	Name           string
	PatchAssetName string
}

type ControllPlaneConfig struct {
	DeploymentTemplateAssetName    string
	MetricsPorts                   []MetricsPort
	SidecarLocalMetricsPortStart   uint16
	SidecarExposedMetricsPortStart uint16
	Sidecars                       []SidecarConfig
	LivenessProbePort              uint16
	StaticAssets                   Assets
	AssetPatches                   AssetPatches
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
}

type LivenessProbeConfig struct {
	// +optional
	InitialDelaySeconds int32
	// +optional
	TimeoutSeconds int32
	// +optional
	PeriodSeconds int32
	// +optional
	FailureThreshold int32
}

type GuestConfig struct {
	DaemonSetTemplateAssetName string
	MetricsPorts               []MetricsPort
	LivenessProbePort          uint16
	StaticAssets               Assets
	StorageClassAssetNames     []string
}

type ClusterFlavour string

const (
	FlavourStandalone ClusterFlavour = "standalone"
	FlavourHyperShift ClusterFlavour = "hypershift"
)

type CSIDriverAssets struct {
	ControllerTemplate        []byte
	ControllerStaticResources map[string][]byte
	NodeTemplate              []byte
	GuestStaticResources      map[string][]byte
	FlavourAssetNames         map[ClusterFlavour][]string
	FlavourAssetPatches       map[ClusterFlavour][]AssetPatch
}

type RuntimeConfig struct {
	ClusterFlavour ClusterFlavour
}
