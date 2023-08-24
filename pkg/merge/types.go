package merge

type CSIDriverOperatorConfig struct {
	AssetPrefix      string
	AssetShortPrefix string
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

type NodeConfig struct {
	DaemonSetTemplateAssetName string
	MetricsPorts               []MetricsPort
	LivenessProbePort          uint16
	StaticAssetNames           []string
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

var (
	DefaultControllerAssetNames = []string{
		"base/controller_sa.yaml",
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
