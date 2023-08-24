package merge

type CSIDriverOperatorConfig struct {
	AssetPrefix      string
	AssetShortPrefix string
	DriverName       string
	ControllerConfig *ControllPlaneConfig `json:"controllerConfig,omitempty"`
	NodeConfig       *GuestConfig         `json:"nodeConfig,omitempty"`
}

type ControllPlaneConfig struct {
	DeploymentTemplateAssetName    string
	MetricsPorts                   []MetricsPort
	SidecarLocalMetricsPortStart   uint16
	SidecarExposedMetricsPortStart uint16
	Sidecars                       []SidecarConfig `json:"sidecars,omitempty"`
	LivenessProbePort              uint16
	StaticAssetNames               []string
}

type MetricsPort struct {
	LocalPort           uint16 `json:"port,omitempty"`
	ExposedPort         uint16
	Name                string
	InjectKubeRBACProxy bool `json:"injectKubeRBACProxy,omitempty"`
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
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
	// +optional
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

type GuestConfig struct {
	DaemonSetTemplateAssetName string
	MetricsPorts               []MetricsPort
	LivenessProbePort          uint16
	StaticAssetNames           []string
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
	NodeStaticResources       map[string][]byte
}
