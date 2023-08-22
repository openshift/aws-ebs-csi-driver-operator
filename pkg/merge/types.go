package merge

type CSIDriverOperatoConfig struct {
	AssetPrefix      string
	DriverName       string
	ControllerConfig *ControllerConfig `json:"controllerConfig,omitempty"`
	NodeConfig       *NodeConfig       `json:"nodeConfig,omitempty"`
}

type ControllerConfig struct {
	DeploymentTemplateAssetName    string
	MetricsPorts                   []MetricsPort
	SidecarLocalMetricsPortStart   uint16
	SidecarExposedMetricsPortStart uint16
	Sidecars                       []SidecarConfig `json:"sidecars,omitempty"`
	LivenessProbePort              uint16
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

type NodeConfig struct {
	DaemonSetTemplateAssetName string
	MetricsPorts               []MetricsPort
	LivenessProbePort          uint16
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
