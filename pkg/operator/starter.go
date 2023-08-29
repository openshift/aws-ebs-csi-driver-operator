package operator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/aws"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/clients"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/csi/csicontrollerset"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivercontrollerservicecontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	"github.com/openshift/aws-ebs-csi-driver-operator/assets"
)

const (
	defaultNamespace = "openshift-cluster-csi-drivers"
	operatorName     = "aws-ebs-csi-driver-operator"
	operandName      = "aws-ebs-csi-driver"

	kmsKeyID = "kmsKeyId"

	hypershiftImageEnvName = "HYPERSHIFT_IMAGE"

	hypershiftPriorityClass = "hypershift-control-plane"

	resync = 20 * time.Minute
)

var (
	hostedControlPlaneGVR = schema.GroupVersionResource{
		Group:    "hypershift.openshift.io",
		Version:  "v1beta1",
		Resource: "hostedcontrolplanes",
	}
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext, guestKubeConfigString string) error {
	b := clients.NewBuilder(operatorName, string(opv1.AWSEBSCSIDriver), controllerConfig, resync)
	b.WithHyperShiftGuest(guestKubeConfigString)
	c := b.BuildOrDie(ctx)

	isHypershift := guestKubeConfigString != ""

	// Create core clientset and informer for the MANAGEMENT cluster.
	controlPlaneNamespace := controllerConfig.OperatorNamespace
	controlPlaneSecretInformer := c.GetControlPlaneSecretInformer(controlPlaneNamespace)
	controlPlaneConfigMapInformer := c.GetControlPlaneConfigMapInformer(controlPlaneNamespace)

	guestNodeInformer := c.GetGuestNodeInformer()
	guestInfraInformer := c.GetGuestInfraInformer()

	var hostedControlPlaneLister cache.GenericLister
	var hostedControlPlaneInformer cache.SharedInformer
	if isHypershift {
		hostedControlPlaneInformer = c.ControlPlaneDynamicInformer.ForResource(hostedControlPlaneGVR).Informer()
		hostedControlPlaneLister = c.ControlPlaneDynamicInformer.ForResource(hostedControlPlaneGVR).Lister()
	}

	// Generate assets
	flavour := merge.FlavourStandalone
	if isHypershift {
		flavour = merge.FlavourHyperShift
	}
	runtimeConfig := &merge.RuntimeConfig{
		ClusterFlavour:        flavour,
		ControlPlaneNamespace: controlPlaneNamespace,
		Replacements:          nil,
	}
	operatorConfig, err := aws.GetAWSEBSConfig(c)
	if err != nil {
		return err
	}
	gen := merge.NewAssetGenerator(runtimeConfig, operatorConfig)
	a, err := gen.GenerateAssets()
	if err != nil {
		return err
	}

	controlPlaneControllerInformers := []factory.Informer{
		controlPlaneSecretInformer.Informer(),
		controlPlaneConfigMapInformer.Informer(),
		guestNodeInformer.Informer(),
		guestInfraInformer.Informer(),
	}
	if isHypershift {
		controlPlaneControllerInformers = append(controlPlaneControllerInformers, hostedControlPlaneInformer)
	}

	controllerHooks := []dc.DeploymentHookFunc{
		withHypershiftNodeSelectorHook(isHypershift, controlPlaneNamespace, hostedControlPlaneLister),
		withHypershiftReplicasHook(isHypershift, guestNodeInformer.Lister()),
		csidrivercontrollerservicecontroller.WithObservedProxyDeploymentHook(),
		withHypershiftControlPlaneImages(isHypershift, os.Getenv("DRIVER_CONTROL_PLANE_IMAGE"), os.Getenv("LIVENESS_PROBE_CONTROL_PLANE_IMAGE")),
	}
	for _, fhook := range operatorConfig.ControllerConfig.DeploymentHooks {
		if fhook.ClusterFlavours.Has(flavour) {
			controllerHooks = append(controllerHooks, fhook.Hook)
		}
	}

	for _, secretName := range operatorConfig.ControllerConfig.WatchedSecretNames {
		controllerHooks = append(controllerHooks, csidrivercontrollerservicecontroller.WithSecretHashAnnotationHook(controlPlaneNamespace, secretName, controlPlaneSecretInformer))
	}

	// Start controllers that manage resources in the MANAGEMENT cluster.
	controlPlaneCSIControllerSet := csicontrollerset.NewCSIControllerSet(
		c.OperatorClient,
		c.EventRecorder,
	).WithLogLevelController().WithManagementStateController(
		operandName,
		false,
	).WithStaticResourcesController(
		"AWSEBSDriverControlPlaneStaticResourcesController",
		c.ControlPlaneKubeClient,
		c.ControlPlaneDynamicClient,
		c.ControlPlaneKubeInformers,
		a.GetAsset,
		a.GetStaticControllerAssetNames(),
	).WithCSIConfigObserverController(
		"AWSEBSDriverCSIConfigObserverController",
		c.GuestConfigInformers,
	).WithCSIDriverControllerService(
		"AWSEBSDriverControllerServiceController",
		a.GetAsset,
		merge.ControllerDeploymentAssetName,
		c.ControlPlaneKubeClient,
		c.ControlPlaneKubeInformers.InformersFor(controlPlaneNamespace),
		c.GuestConfigInformers,
		controlPlaneControllerInformers,
		controllerHooks...,
	)
	if err != nil {
		return err
	}
	/*
			// Start controllers that manage resources in GUEST clusters.
			guestCSIControllerSet := csicontrollerset.NewCSIControllerSet(
				guestOperatorClient,
				eventRecorder,
			).WithStaticResourcesController(
				"AWSEBSDriverGuestStaticResourcesController",
				guestKubeClient,
				guestDynamicClient,
				guestKubeInformersForNamespaces,
				assets.ReadFile,
				[]string{
					"csidriver.yaml",
					"node_sa.yaml",
					"rbac/privileged_role.yaml",
					"rbac/node_privileged_binding.yaml",
				},
			).WithConditionalStaticResourcesController(
				"AWSEBSDriverConditionalStaticResourcesController",
				guestKubeClient,
				guestDynamicClient,
				guestKubeInformersForNamespaces,
				assets.ReadFile,
				[]string{
					"volumesnapshotclass.yaml",
				},
				// Only install when CRD exists.
				func() bool {
					name := "volumesnapshotclasses.snapshot.storage.k8s.io"
					_, err := guestAPIExtClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), name, metav1.GetOptions{})
					return err == nil
				},
				// Don't ever remove.
				func() bool {
					return false
				},
			).WithCSIDriverNodeService(
				"AWSEBSDriverNodeServiceController",
				assets.ReadFile,
				"node.yaml",
				guestKubeClient,
				guestKubeInformersForNamespaces.InformersFor(guestNamespace),
				[]factory.Informer{guestConfigMapInformer.Informer()},
				csidrivernodeservicecontroller.WithObservedProxyDaemonSetHook(),
				csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
					guestNamespace,
					trustedCAConfigMap,
					guestConfigMapInformer,
				),
			).WithStorageClassController(
				"AWSEBSDriverStorageClassController",
				assets.ReadFile,
				[]string{
					"storageclass_gp3.yaml",
					"storageclass_gp2.yaml",
				},
				guestKubeClient,
				guestKubeInformersForNamespaces.InformersFor(""),
				guestCCDInformers,
				getKMSKeyHook(guestCCDInformers.Operator().V1().ClusterCSIDrivers().Lister()),
			)

		if !isHypershift {
			caSyncController, err := newCustomAWSBundleSyncer(
				guestOperatorClient,
				controlPlaneCloudConfigInformers,
				controlPlaneKubeClient,
				controlPlaneNamespace,
				eventRecorder,
			)
			if err != nil {
				return fmt.Errorf("could not create the custom CA bundle syncer: %w", err)
			}

			klog.Info("Starting custom CA bundle informers")
			go controlPlaneCloudConfigInformers.Start(ctx.Done())

			klog.Info("Starting custom CA bundle sync controller")
			go caSyncController.Run(ctx, 1)

			staticResourcesController := staticresourcecontroller.NewStaticResourceController(
				"AWSEBSDriverStaticResourcesController",
				assets.ReadFile,
				[]string{
					"rbac/main_attacher_binding.yaml",
					"rbac/main_provisioner_binding.yaml",
					"rbac/volumesnapshot_reader_provisioner_binding.yaml",
					"rbac/main_resizer_binding.yaml",
					"rbac/storageclass_reader_resizer_binding.yaml",
					"rbac/main_snapshotter_binding.yaml",
					//"service.yaml",
					"rbac/prometheus_role.yaml",
					"rbac/prometheus_rolebinding.yaml",
					"rbac/kube_rbac_proxy_role.yaml",
					"rbac/kube_rbac_proxy_binding.yaml",
					"rbac/lease_leader_election_role.yaml",
					"rbac/lease_leader_election_rolebinding.yaml",
				},
				(&resourceapply.ClientHolder{}).WithKubernetes(controlPlaneKubeClient).WithDynamicClient(controlPlaneDynamicClient),
				guestOperatorClient,
				eventRecorder,
			).AddKubeInformers(controlPlaneKubeInformersForNamespaces)

			klog.Info("Starting static resources controller")
			go staticResourcesController.Run(ctx, 1)

			serviceMonitorController := staticresourcecontroller.NewStaticResourceController(
				"AWSEBSDriverServiceMonitorController",
				assets.ReadFile,
				[]string{"servicemonitor.yaml"},
				(&resourceapply.ClientHolder{}).WithDynamicClient(controlPlaneDynamicClient),
				guestOperatorClient,
				eventRecorder,
			).WithIgnoreNotFoundOnCreate()

			klog.Info("Starting ServiceMonitor controller")
			go serviceMonitorController.Run(ctx, 1)
		}
	*/
	c.Start(ctx)
	klog.Info("Starting control plane controllerset")
	go controlPlaneCSIControllerSet.Run(ctx, 1)
	klog.Info("Starting guest controllerset")
	//go guestCSIControllerSet.Run(ctx, 1)

	<-ctx.Done()

	return fmt.Errorf("stopped")
}

func assetWithNamespaceFunc(namespace string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		content, err := assets.ReadFile(name)
		if err != nil {
			panic(err)
		}
		return bytes.ReplaceAll(content, []byte("${NAMESPACE}"), []byte(namespace)), nil
	}
}

func withNamespaceDeploymentHook(namespace string) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		deployment.Namespace = namespace
		return nil
	}
}

func withHypershiftReplicasHook(isHypershift bool, guestNodeLister corev1listers.NodeLister) dc.DeploymentHookFunc {
	if !isHypershift {
		return csidrivercontrollerservicecontroller.WithReplicasHook(guestNodeLister)
	}
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		// TODO: get this information from HostedControlPlane.Spec.AvailabilityPolicy
		replicas := int32(1)
		deployment.Spec.Replicas = &replicas
		return nil
	}

}

func withHypershiftNodeSelectorHook(isHypershift bool, namespace string, hostedControlPlaneLister cache.GenericLister) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		if !isHypershift {
			return nil
		}

		podSpec := &deployment.Spec.Template.Spec
		// Add nodeSelector
		nodeSelector, err := getHostedControlPlaneNodeSelector(hostedControlPlaneLister, namespace)
		if err != nil {
			return err
		}
		podSpec.NodeSelector = nodeSelector

		return nil
	}
}

func getHostedControlPlaneNodeSelector(hostedControlPlaneLister cache.GenericLister, namespace string) (map[string]string, error) {
	hcp, err := getHostedControlPlane(hostedControlPlaneLister, namespace)
	if err != nil {
		return nil, err
	}
	nodeSelector, exists, err := unstructured.NestedStringMap(hcp.UnstructuredContent(), "spec", "nodeSelector")
	if !exists {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	klog.V(4).Infof("Using node selector %v", nodeSelector)
	return nodeSelector, nil
}

func getHostedControlPlane(hostedControlPlaneLister cache.GenericLister, namespace string) (*unstructured.Unstructured, error) {
	list, err := hostedControlPlaneLister.ByNamespace(namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no HostedControlPlane found in namespace %s", namespace)
	}
	if len(list) > 1 {
		return nil, fmt.Errorf("more than one HostedControlPlane found in namespace %s", namespace)
	}

	hcp := list[0].(*unstructured.Unstructured)
	if hcp == nil {
		return nil, fmt.Errorf("unknown type of HostedControlPlane found in namespace %s", namespace)
	}
	return hcp, nil
}

func withHypershiftControlPlaneImages(isHypershift bool, driverControlPlaneImage, livenessProbeControlPlaneImage string) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		if !isHypershift {
			return nil
		}
		for i := range deployment.Spec.Template.Spec.Containers {
			container := &deployment.Spec.Template.Spec.Containers[i]
			if container.Name == "csi-driver" && driverControlPlaneImage != "" {
				container.Image = driverControlPlaneImage
			}
			if container.Name == "csi-liveness-probe" && livenessProbeControlPlaneImage != "" {
				container.Image = livenessProbeControlPlaneImage
			}
		}
		return nil
	}
}
