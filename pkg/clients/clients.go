package clients

import (
	"context"

	cfgv1informers "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"

	cfgclientset "github.com/openshift/client-go/config/clientset/versioned"
	cfginformers "github.com/openshift/client-go/config/informers/externalversions"
	opclient "github.com/openshift/client-go/operator/clientset/versioned"
	opinformers "github.com/openshift/client-go/operator/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

const (
	CSIDriverNamespace     = "openshift-cluster-csi-drivers"
	CloudConfigNamespace   = "openshift-config"
	ManagedConfigNamespace = "openshift-config-managed"
)

type Clients struct {
	ControlPlaneNamespace string

	// Client for operator's CR. Always in the guest or standalone cluster.
	OperatorClient           v1helpers.OperatorClientWithFinalizers
	OperatorDynamicInformers dynamicinformer.DynamicSharedInformerFactory

	// Recorder for the operator events. Always in the guest cluster.
	EventRecorder events.Recorder

	// Kubernetes API client for hypershift or standalone control plane.
	ControlPlaneKubeClient kubernetes.Interface
	// Kubernetes API client for hypershift or standalone control plane. Per namespace.
	ControlPlaneKubeInformers v1helpers.KubeInformersForNamespaces

	// Dynamic client in hypershift or standalone control plane. E.g. for hypershift's HostedControlPlane and Prometheus CRs.
	ControlPlaneDynamicClient   dynamic.Interface
	ControlPlaneDynamicInformer dynamicinformer.DynamicSharedInformerFactory

	// Kubernetes API client for guest or standalone.
	GuestKubeClient kubernetes.Interface
	// Kubernetes API client for guest or standalone. Per namespace.
	GuestKubeInformers v1helpers.KubeInformersForNamespaces

	GuestAPIExtClient   apiextclient.Interface
	GuestAPIExtInformer apiextinformers.SharedInformerFactory

	// Dynamic client for the guest cluster. E.g. for VolumeSnapshotClass.
	GuestDynamicClient   dynamic.Interface
	GuestDynamicInformer dynamicinformer.DynamicSharedInformerFactory

	// operator.openshift.io client, e.g. for ClusterCSIDriver. Always in the guest or standalone cluster.
	GuestOperatorClientSet opclient.Interface
	// operator.openshift.io informers.  Always in the guest or standalone cluster.
	GuestOperatorInformers opinformers.SharedInformerFactory

	// config.openshift.io client, e.g. for Infrastructure. Always in the guest or standalone cluster.
	GuestConfigClientSet cfgclientset.Interface
	// config.openshift.io informers. Always in the guest or standalone cluster.
	GuestConfigInformers cfginformers.SharedInformerFactory
}

func (c *Clients) GetControlPlaneSecretInformer(namespace string) coreinformers.SecretInformer {
	return c.ControlPlaneKubeInformers.InformersFor(namespace).Core().V1().Secrets()
}

func (c *Clients) GetControlPlaneConfigMapInformer(namespace string) coreinformers.ConfigMapInformer {
	return c.ControlPlaneKubeInformers.InformersFor(namespace).Core().V1().ConfigMaps()
}

func (c *Clients) GetGuestConfigMapInformer(namespace string) coreinformers.ConfigMapInformer {
	return c.GuestKubeInformers.InformersFor(namespace).Core().V1().ConfigMaps()
}

func (c *Clients) GetGuestNodeInformer() coreinformers.NodeInformer {
	return c.GuestKubeInformers.InformersFor("").Core().V1().Nodes()
}

func (c *Clients) GetGuestInfraInformer() cfgv1informers.InfrastructureInformer {
	return c.GuestConfigInformers.Config().V1().Infrastructures()
}

func (c *Clients) Start(ctx context.Context) {
	c.OperatorDynamicInformers.Start(ctx.Done())
	c.ControlPlaneKubeInformers.Start(ctx.Done())
	c.ControlPlaneDynamicInformer.Start(ctx.Done())
	c.GuestKubeInformers.Start(ctx.Done())
	c.GuestAPIExtInformer.Start(ctx.Done())
	c.GuestDynamicInformer.Start(ctx.Done())
	c.GuestOperatorInformers.Start(ctx.Done())
	c.GuestConfigInformers.Start(ctx.Done())
}
