package config

import (
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/clients"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generator"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	"github.com/openshift/library-go/pkg/operator/csi/csistorageclasscontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"k8s.io/apimachinery/pkg/util/sets"
)

type DeploymentHookBuilder func(c *clients.Clients) (dc.DeploymentHookFunc, []factory.Informer)
type FlavourDeploymentHook struct {
	ClusterFlavours sets.Set[generator.ClusterFlavour]
	Hook            DeploymentHookBuilder
}

type FlavourDeploymentHooks []FlavourDeploymentHook

type DaemonSetHookBuilder func(c *clients.Clients) (csidrivernodeservicecontroller.DaemonSetHookFunc, []factory.Informer)
type FlavourDaemonSetHook struct {
	ClusterFlavours sets.Set[generator.ClusterFlavour]
	Hook            DaemonSetHookBuilder
}
type FlavourDaemonSetHooks []FlavourDaemonSetHook

type ControllerBuilder func(c *clients.Clients) (factory.Controller, error)

type FlavourControllerBuilder struct {
	ClusterFlavours   sets.Set[generator.ClusterFlavour]
	ControllerBuilder ControllerBuilder
}

type FlavourControllerBuilders []FlavourControllerBuilder

type StorageClassHookBuilder func(c *clients.Clients) (csistorageclasscontroller.StorageClassHookFunc, []factory.Informer)

type OperatorConfig struct {
	ControlPlaneDeploymentHooks    FlavourDeploymentHooks
	ExtraControlPlaneControllers   FlavourControllerBuilders
	ControlPlaneWatchedSecretNames []string

	GuestDaemonSetHooks FlavourDaemonSetHooks
	StorageClassHooks   []StorageClassHookBuilder // No flavours here, but can be added when needed
}

func NewDeploymentHooks(flavours sets.Set[generator.ClusterFlavour], hooks ...DeploymentHookBuilder) FlavourDeploymentHooks {
	result := make([]FlavourDeploymentHook, 0, len(hooks))
	for _, hook := range hooks {
		result = append(result, FlavourDeploymentHook{
			ClusterFlavours: flavours,
			Hook:            hook,
		})
	}
	return result
}

func (h FlavourDeploymentHooks) WithHooks(flavours sets.Set[generator.ClusterFlavour], hooks ...DeploymentHookBuilder) FlavourDeploymentHooks {
	result := make([]FlavourDeploymentHook, 0, len(h)+len(hooks))
	result = append(result, h...)
	for _, hook := range hooks {
		result = append(result, FlavourDeploymentHook{
			ClusterFlavours: flavours,
			Hook:            hook,
		})
	}
	return result
}

func NewControllerBuilders(flavours sets.Set[generator.ClusterFlavour], builders ...ControllerBuilder) FlavourControllerBuilders {
	result := make([]FlavourControllerBuilder, 0, len(builders))
	for _, builder := range builders {
		result = append(result, FlavourControllerBuilder{
			ClusterFlavours:   flavours,
			ControllerBuilder: builder,
		})
	}
	return result
}

func (b FlavourControllerBuilders) WithBuilders(flavours sets.Set[generator.ClusterFlavour], builders ...ControllerBuilder) FlavourControllerBuilders {
	result := make([]FlavourControllerBuilder, 0, len(b)+len(builders))
	result = append(result, b...)
	for _, builder := range builders {
		result = append(result, FlavourControllerBuilder{
			ClusterFlavours:   flavours,
			ControllerBuilder: builder,
		})
	}
	return result
}

func NewDaemonSetHooks(flavours sets.Set[generator.ClusterFlavour], hooks ...DaemonSetHookBuilder) FlavourDaemonSetHooks {
	result := make([]FlavourDaemonSetHook, 0, len(hooks))
	for _, hook := range hooks {
		result = append(result, FlavourDaemonSetHook{
			ClusterFlavours: flavours,
			Hook:            hook,
		})
	}
	return result
}

func (h FlavourDaemonSetHooks) WithHooks(flavours sets.Set[generator.ClusterFlavour], hooks ...DaemonSetHookBuilder) FlavourDaemonSetHooks {
	result := make([]FlavourDaemonSetHook, 0, len(h)+len(hooks))
	result = append(result, h...)
	for _, hook := range hooks {
		result = append(result, FlavourDaemonSetHook{
			ClusterFlavours: flavours,
			Hook:            hook,
		})
	}
	return result
}
