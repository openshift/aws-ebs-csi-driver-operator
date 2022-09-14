package operator

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	kubeclient "k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	opv1 "github.com/openshift/api/operator/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	v1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/config/client"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/csi/csicontrollerset"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivercontrollerservicecontroller"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	goc "github.com/openshift/library-go/pkg/operator/genericoperatorclient"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/aws-ebs-csi-driver-operator/assets"
)

const (
	defaultNamespace   = "openshift-cluster-csi-drivers"
	operatorName       = "aws-ebs-csi-driver-operator"
	operandName        = "aws-ebs-csi-driver"
	secretName         = "ebs-cloud-credentials"
	infraConfigName    = "cluster"
	trustedCAConfigMap = "aws-ebs-csi-driver-trusted-ca-bundle"

	cloudConfigNamespace = "openshift-config-managed"
	cloudConfigName      = "kube-cloud-config"
	caBundleKey          = "ca-bundle.pem"

	infrastructureName = "cluster"
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext, guestKubeConfigString string) error {
	// Create core clientset and informer for the MANAGEMENT cluster.
	eventRecorder := controllerConfig.EventRecorder
	controlPlaneNamespace := controllerConfig.OperatorNamespace
	controlPlaneKubeClient := kubeclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	controlPlaneKubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(controlPlaneKubeClient, controlPlaneNamespace, cloudConfigNamespace, "")
	controlPlaneSecretInformer := controlPlaneKubeInformersForNamespaces.InformersFor(controlPlaneNamespace).Core().V1().Secrets()
	controlPlaneConfigMapInformer := controlPlaneKubeInformersForNamespaces.InformersFor(controlPlaneNamespace).Core().V1().ConfigMaps()

	// Create informer for the ConfigMaps in the operator namespace.
	// This is used to get the custom CA bundle to use when accessing the AWS API.
	controlPlaneCloudConfigInformer := controlPlaneKubeInformersForNamespaces.InformersFor(controlPlaneNamespace).Core().V1().ConfigMaps()
	controlPlaneCloudConfigLister := controlPlaneCloudConfigInformer.Lister().ConfigMaps(controlPlaneNamespace)

	controlPlaneDynamicClient, err := dynamic.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}

	// Create core clientset for the GUEST cluster.
	guestNamespace := defaultNamespace
	guestKubeConfig := controllerConfig.KubeConfig
	guestKubeClient := controlPlaneKubeClient
	isHypershift := guestKubeConfigString != ""
	if isHypershift {
		guestKubeConfig, err = client.GetKubeConfigOrInClusterConfig(guestKubeConfigString, nil)
		if err != nil {
			return err
		}
		guestKubeClient = kubeclient.NewForConfigOrDie(rest.AddUserAgent(guestKubeConfig, operatorName))

		// Create all events in the GUEST cluster.
		// Use name of the operator Deployment in the management cluster + namespace
		// in the guest cluster as the closest approximation of the real involvedObject.
		controllerRef, err := events.GetControllerReferenceForCurrentPod(ctx, controlPlaneKubeClient, controlPlaneNamespace, nil)
		controllerRef.Namespace = guestNamespace
		if err != nil {
			klog.Warningf("unable to get owner reference (falling back to namespace): %v", err)
		}
		eventRecorder = events.NewKubeRecorder(guestKubeClient.CoreV1().Events(guestNamespace), operandName, controllerRef)
	}

	guestAPIExtClient, err := apiextclient.NewForConfig(rest.AddUserAgent(guestKubeConfig, operatorName))
	if err != nil {
		return err
	}

	guestDynamicClient, err := dynamic.NewForConfig(guestKubeConfig)
	if err != nil {
		return err
	}

	// Client informers for the GUEST cluster.
	guestKubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(guestKubeClient, guestNamespace, "")
	guestConfigMapInformer := guestKubeInformersForNamespaces.InformersFor(guestNamespace).Core().V1().ConfigMaps()
	guestNodeInformer := guestKubeInformersForNamespaces.InformersFor("").Core().V1().Nodes()

	guestConfigClient := configclient.NewForConfigOrDie(rest.AddUserAgent(guestKubeConfig, operatorName))
	guestConfigInformers := configinformers.NewSharedInformerFactory(guestConfigClient, 20*time.Minute)
	guestInfraInformer := guestConfigInformers.Config().V1().Infrastructures()

	// Create client and informers for our ClusterCSIDriver CR.
	gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
	guestOperatorClient, guestDynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(guestKubeConfig, gvr, string(opv1.AWSEBSCSIDriver))
	if err != nil {
		return err
	}

	// Start controllers that manage resources in the MANAGEMENT cluster.
	controlPlaneCSIControllerSet := csicontrollerset.NewCSIControllerSet(
		guestOperatorClient,
		eventRecorder,
	).WithLogLevelController().WithManagementStateController(
		operandName,
		false,
	).WithStaticResourcesController(
		"AWSEBSDriverStaticResourcesController",
		controlPlaneKubeClient,
		controlPlaneDynamicClient,
		controlPlaneKubeInformersForNamespaces,
		assetWithNamespaceFunc(controlPlaneNamespace),
		[]string{
			"controller_sa.yaml",
			"controller_pdb.yaml",
			"service.yaml",
			"cabundle_cm.yaml",
			"rbac/attacher_role.yaml",
			"rbac/attacher_binding.yaml",
			"rbac/privileged_role.yaml",
			"rbac/controller_privileged_binding.yaml",
			"rbac/provisioner_role.yaml",
			"rbac/provisioner_binding.yaml",
			"rbac/resizer_role.yaml",
			"rbac/resizer_binding.yaml",
			"rbac/snapshotter_role.yaml",
			"rbac/snapshotter_binding.yaml",
			"rbac/prometheus_role.yaml",
			"rbac/prometheus_rolebinding.yaml",
			"rbac/kube_rbac_proxy_role.yaml",
			"rbac/kube_rbac_proxy_binding.yaml",
		},
	).WithConditionalStaticResourcesController(
		"AWSEBSDriverConditionalStaticResourcesController",
		kubeClient,
		dynamicClient,
		kubeInformersForNamespaces,
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
	).WithCSIConfigObserverController(
		"AWSEBSDriverCSIConfigObserverController",
		guestConfigInformers,
	).WithCSIDriverControllerService(
		"AWSEBSDriverControllerServiceController",
		assets.ReadFile,
		"controller.yaml",
		controlPlaneKubeClient,
		controlPlaneKubeInformersForNamespaces.InformersFor(controlPlaneNamespace),
		guestConfigInformers,
		[]factory.Informer{
			controlPlaneSecretInformer.Informer(),
			guestNodeInformer.Informer(),
			controlPlaneCloudConfigInformer.Informer(),
			guestInfraInformer.Informer(),
			controlPlaneConfigMapInformer.Informer(),
		},
		withHypershiftDeploymentHook(isHypershift),
		withHypershiftReplicasHook(isHypershift, guestNodeInformer.Lister()),
		withNamespaceDeploymentHook(controlPlaneNamespace),
		csidrivercontrollerservicecontroller.WithSecretHashAnnotationHook(controlPlaneNamespace, secretName, controlPlaneSecretInformer),
		csidrivercontrollerservicecontroller.WithObservedProxyDeploymentHook(),
		withCustomAWSCABundle(guestInfraInformer.Lister(), controlPlaneCloudConfigLister),
		withCustomTags(guestInfraInformer.Lister()),
		withCustomEndPoint(guestInfraInformer.Lister()),
		csidrivercontrollerservicecontroller.WithCABundleDeploymentHook(
			controlPlaneNamespace,
			trustedCAConfigMap,
			controlPlaneConfigMapInformer,
		),
	).WithServiceMonitorController(
		"AWSEBSDriverServiceMonitorController",
		controlPlaneDynamicClient,
		assetWithNamespaceFunc(controlPlaneNamespace),
		"servicemonitor.yaml",
	)
	if err != nil {
		return err
	}

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
			"storageclass_gp2.yaml",
			"volumesnapshotclass.yaml",
			"csidriver.yaml",
			"node_sa.yaml",
			"rbac/node_privileged_binding.yaml",
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
		"storageclass_gp3.yaml",
		guestKubeClient,
		guestKubeInformersForNamespaces.InformersFor(""),
	)

	klog.Info("Starting the control plane informers")
	go controlPlaneKubeInformersForNamespaces.Start(ctx.Done())

	klog.Info("Starting control plane controllerset")
	go controlPlaneCSIControllerSet.Run(ctx, 1)

	// Only start caSyncController in standalone clusters because in Hypershift
	// the ConfigMap is already available in the correct namespace.
	if !isHypershift {
		caSyncController, err := newCustomAWSBundleSyncer(
			guestOperatorClient,
			controlPlaneKubeInformersForNamespaces,
			controlPlaneKubeClient,
			controlPlaneNamespace,
			eventRecorder,
		)
		if err != nil {
			return fmt.Errorf("could not create the custom CA bundle syncer: %w", err)
		}

		klog.Info("Starting custom CA bundle sync controller")
		go caSyncController.Run(ctx, 1)
	}

	klog.Info("Starting the guest cluster informers")
	go guestKubeInformersForNamespaces.Start(ctx.Done())
	go guestDynamicInformers.Start(ctx.Done())
	go guestConfigInformers.Start(ctx.Done())

	klog.Info("Starting guest cluster controllerset")
	go guestCSIControllerSet.Run(ctx, 1)

	<-ctx.Done()

	return fmt.Errorf("stopped")
}

// withCustomAWSCABundle executes the asset as a template to fill out the parts required when using a custom CA bundle.
// The `caBundleConfigMap` parameter specifies the name of the ConfigMap containing the custom CA bundle. If the
// argument supplied is empty, then no custom CA bundle will be used.
func withCustomAWSCABundle(infraLister v1.InfrastructureLister, cloudConfigLister corev1listers.ConfigMapNamespaceLister) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		configName, err := customAWSCABundle(infraLister, cloudConfigLister)
		if err != nil {
			return fmt.Errorf("could not determine if a custom CA bundle is in use: %w", err)
		}
		if configName == "" {
			return nil
		}

		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: configName},
				},
			},
		})
		for i := range deployment.Spec.Template.Spec.Containers {
			container := &deployment.Spec.Template.Spec.Containers[i]
			if container.Name != "csi-driver" {
				continue
			}
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "AWS_CA_BUNDLE",
				Value: "/etc/ca/ca-bundle.pem",
			})
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      "ca-bundle",
				MountPath: "/etc/ca",
				ReadOnly:  true,
			})
			return nil
		}
		return fmt.Errorf("could not use custom CA bundle because the csi-driver container is missing from the deployment")
	}
}

func withCustomEndPoint(infraLister v1.InfrastructureLister) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		infra, err := infraLister.Get(infrastructureName)
		if err != nil {
			return err
		}
		if infra.Status.PlatformStatus == nil || infra.Status.PlatformStatus.AWS == nil {
			return nil
		}
		serviceEndPoints := infra.Status.PlatformStatus.AWS.ServiceEndpoints
		ec2EndPoint := ""
		for _, serviceEndPoint := range serviceEndPoints {
			if serviceEndPoint.Name == "ec2" {
				ec2EndPoint = serviceEndPoint.URL
			}
		}
		if ec2EndPoint == "" {
			return nil
		}

		for i := range deployment.Spec.Template.Spec.Containers {
			container := &deployment.Spec.Template.Spec.Containers[i]
			if container.Name != "csi-driver" {
				continue
			}
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "AWS_EC2_ENDPOINT",
				Value: ec2EndPoint,
			})
			return nil
		}
		return nil
	}
}

func newCustomAWSBundleSyncer(
	operatorClient v1helpers.OperatorClient,
	kubeInformers v1helpers.KubeInformersForNamespaces,
	kubeClient kubeclient.Interface,
	destinationNamespace string,
	eventRecorder events.Recorder,
) (factory.Controller, error) {
	// sync config map with additional trust bundle to the operator namespace,
	// so the operator can get it as a ConfigMap volume.
	srcConfigMap := resourcesynccontroller.ResourceLocation{
		Namespace: cloudConfigNamespace,
		Name:      cloudConfigName,
	}
	dstConfigMap := resourcesynccontroller.ResourceLocation{
		Namespace: destinationNamespace,
		Name:      cloudConfigName,
	}
	certController := resourcesynccontroller.NewResourceSyncController(
		operatorClient,
		kubeInformers,
		kubeClient.CoreV1(),
		kubeClient.CoreV1(),
		eventRecorder)
	err := certController.SyncConfigMap(dstConfigMap, srcConfigMap)
	if err != nil {
		return nil, err
	}
	return certController, nil
}

// customAWSCABundle returns true if the cloud config ConfigMap exists and contains a custom CA bundle.
func customAWSCABundle(infraLister v1.InfrastructureLister, cloudConfigLister corev1listers.ConfigMapNamespaceLister) (string, error) {
	infra, err := infraLister.Get(infrastructureName)
	if err != nil {
		return "", err
	}

	configName := cloudConfigName
	if infra.Status.ControlPlaneTopology == configv1.ExternalTopologyMode {
		configName = "user-ca-bundle"
	}

	cloudConfigCM, err := cloudConfigLister.Get(configName)
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get the %s ConfigMap: %w", configName, err)
	}

	if _, ok := cloudConfigCM.Data[caBundleKey]; !ok {
		return "", nil
	}
	return configName, nil
}

// withCustomTags add tags from Infrastructure.Status.PlatformStatus.AWS.ResourceTags to the driver command line as
// --extra-tags=<key1>=<value1>,<key2>=<value2>,...
func withCustomTags(infraLister v1.InfrastructureLister) dc.DeploymentHookFunc {
	return func(spec *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		infra, err := infraLister.Get(infrastructureName)
		if err != nil {
			return err
		}
		if infra.Status.PlatformStatus == nil || infra.Status.PlatformStatus.AWS == nil {
			return nil
		}

		userTags := infra.Status.PlatformStatus.AWS.ResourceTags
		if len(userTags) == 0 {
			return nil
		}

		tagPairs := make([]string, 0, len(userTags))
		for _, userTag := range userTags {
			pair := fmt.Sprintf("%s=%s", userTag.Key, userTag.Value)
			tagPairs = append(tagPairs, pair)
		}
		tags := strings.Join(tagPairs, ",")
		tagsArgument := fmt.Sprintf("--extra-tags=%s", tags)

		for i := range deployment.Spec.Template.Spec.Containers {
			container := &deployment.Spec.Template.Spec.Containers[i]
			if container.Name != "csi-driver" {
				continue
			}
			container.Args = append(container.Args, tagsArgument)
		}
		return nil
	}
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

func withHypershiftDeploymentHook(isHypershift bool) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		if !isHypershift {
			return nil
		}
		kubeConfigEnvVar := corev1.EnvVar{
			Name:  "KUBECONFIG",
			Value: "/etc/hosted-kubernetes/kubeconfig",
		}
		volumeMount := corev1.VolumeMount{
			Name:      "hosted-kubeconfig",
			MountPath: "/etc/hosted-kubernetes",
			ReadOnly:  true,
		}
		volume := corev1.Volume{
			Name: "hosted-kubeconfig",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					// FIXME: use a ServiceAccount from the guest cluster
					SecretName: "admin-kubeconfig",
				},
			},
		}
		podSpec := &deployment.Spec.Template.Spec
		for i := range podSpec.Containers {
			container := &podSpec.Containers[i]
			switch container.Name {
			case "csi-provisioner":
			case "csi-attacher":
			case "csi-snapshotter":
			case "csi-resizer":
			default:
				continue
			}
			container.Args = append(container.Args, "--kubeconfig=$(KUBECONFIG)")
			container.Env = append(container.Env, kubeConfigEnvVar)
			container.VolumeMounts = append(container.VolumeMounts, volumeMount)
		}
		podSpec.Volumes = append(podSpec.Volumes, volume)
		return nil
	}
}
