package merge

import (
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/clients"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	"github.com/openshift/library-go/pkg/operator/csi/csistorageclasscontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"k8s.io/apimachinery/pkg/util/sets"
)

type CSIDriverOperatorConfig struct {
	AssetPrefix      string
	AssetShortPrefix string
	DriverName       string
	ControllerConfig *ControllPlaneConfig
	GuestConfig      *GuestConfig
}

type Assets []Asset
type Asset struct {
	ClusterFlavours sets.Set[ClusterFlavour]
	AssetName       string
}

type AssetPatches []AssetPatch
type AssetPatch struct {
	ClusterFlavours sets.Set[ClusterFlavour]
	Name            string
	PatchAssetName  string
}

type FlavourDeploymentHook struct {
	ClusterFlavours sets.Set[ClusterFlavour]
	Hook            dc.DeploymentHookFunc
}
type FlavourDeploymentHooks []FlavourDeploymentHook

type FlavourDaemonSetHook struct {
	ClusterFlavours sets.Set[ClusterFlavour]
	Hook            csidrivernodeservicecontroller.DaemonSetHookFunc
}
type FlavourDaemonSetHooks []FlavourDaemonSetHook

type ControllerBuilder func(c *clients.Clients) (factory.Controller, error)

type FlavourControllerBuilder struct {
	ClusterFlavours   sets.Set[ClusterFlavour]
	ControllerBuilder ControllerBuilder
}

type FlavourControllerBuilders []FlavourControllerBuilder

type ControllPlaneConfig struct {
	DeploymentTemplateAssetName    string
	MetricsPorts                   []MetricsPort
	SidecarLocalMetricsPortStart   uint16
	SidecarExposedMetricsPortStart uint16
	Sidecars                       []SidecarConfig
	LivenessProbePort              uint16
	StaticAssets                   Assets
	AssetPatches                   AssetPatches

	WatchedSecretNames []string
	DeploymentHooks    FlavourDeploymentHooks

	ExtraControllers FlavourControllerBuilders
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
	// TODO: add node Service and ServiceMonitor for metrics
	// MetricsPorts               []MetricsPort
	LivenessProbePort uint16
	Sidecars          []SidecarConfig

	StorageClassAssetNames []string
	StaticAssets           Assets
	AssetPatches           AssetPatches

	DaemonSetHooks    FlavourDaemonSetHooks
	StorageClassHooks []csistorageclasscontroller.StorageClassHookFunc
}

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

type CSIDriverAssets struct {
	ControllerTemplate        []byte
	ControllerStaticResources map[string][]byte
	NodeTemplate              []byte
	GuestStaticResources      map[string][]byte
	GuestStorageClassAssets   map[string][]byte
	FlavourAssetNames         map[ClusterFlavour][]string
	FlavourAssetPatches       map[ClusterFlavour][]AssetPatch
}

type RuntimeConfig struct {
	ClusterFlavour        ClusterFlavour
	ControlPlaneNamespace string
	Replacements          []string
}
