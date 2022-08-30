package operator

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	v1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/config/client"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	kubeclient "k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	opv1 "github.com/openshift/api/operator/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/csi/csicontrollerset"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivercontrollerservicecontroller"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	"github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	goc "github.com/openshift/library-go/pkg/operator/genericoperatorclient"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/aws-ebs-csi-driver-operator/assets"
)

const (
	// Operand and operator run in the same namespace
	operatorName = "aws-ebs-csi-driver-operator"
	operandName  = "aws-ebs-csi-driver"
	// operandNamespace   = "openshift-cluster-csi-drivers"
	secretName         = "ebs-cloud-credentials"
	infraConfigName    = "cluster"
	trustedCAConfigMap = "aws-ebs-csi-driver-trusted-ca-bundle"

	cloudConfigNamespace = "openshift-config-managed"
	cloudConfigName      = "kube-cloud-config"
	caBundleKey          = "ca-bundle.pem"

	infrastructureName = "cluster"
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext, guestKubeConfigString *string) error {
	// Create core clientset and informer
	controlPlaneNamespace := controllerConfig.OperatorNamespace
	controlPlaneEventRecorder := controllerConfig.EventRecorder
	kubeClient := kubeclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient, controlPlaneNamespace, cloudConfigNamespace, "") // FIXME: do we really need to watch all namespaces in the management cluster?
	secretInformer := kubeInformersForNamespaces.InformersFor(controlPlaneNamespace).Core().V1().Secrets()
	nodeInformer := kubeInformersForNamespaces.InformersFor("").Core().V1().Nodes()
	configMapInformer := kubeInformersForNamespaces.InformersFor(controlPlaneNamespace).Core().V1().ConfigMaps()

	// Create informer for the ConfigMaps in the openshift-config-managed namespace.
	// This is used to get the custom CA bundle to use when accessing the AWS API.
	cloudConfigInformer := kubeInformersForNamespaces.InformersFor(controlPlaneNamespace).Core().V1().ConfigMaps()
	cloudConfigLister := cloudConfigInformer.Lister().ConfigMaps(controlPlaneNamespace)

	dynamicClient, err := dynamic.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}

	// Guest
	guestNamespace := "openshift-cluster-csi-drivers"
	guestKubeConfig := controllerConfig.KubeConfig
	if guestKubeConfigString != nil {
		guestKubeConfig, err = client.GetKubeConfigOrInClusterConfig(*guestKubeConfigString, nil)
		if err != nil {
			return err
		}
	}

	guestAPIExtClient, err := apiextclient.NewForConfig(rest.AddUserAgent(guestKubeConfig, operatorName))
	if err != nil {
		return err
	}

	guestDynamicClient, err := dynamic.NewForConfig(guestKubeConfig)
	if err != nil {
		return err
	}

	// Create config clientset and informer. This is used to get the cluster ID from the GUEST.
	guestKubeClient := kubeclient.NewForConfigOrDie(rest.AddUserAgent(guestKubeConfig, operatorName))
	guestKubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(guestKubeClient, guestNamespace, "")
	guestConfigMapInformer := guestKubeInformersForNamespaces.InformersFor(guestNamespace).Core().V1().ConfigMaps()

	guestConfigClient := configclient.NewForConfigOrDie(rest.AddUserAgent(guestKubeConfig, operatorName))
	guestConfigInformers := configinformers.NewSharedInformerFactory(guestConfigClient, 20*time.Minute)
	guestInfraInformer := guestConfigInformers.Config().V1().Infrastructures()

	// FIXME(bertinatto): need a valid recorder for the guest cluster
	// guestEventRecorder := events.NewKubeRecorder(guestKubeClient.CoreV1().Events(guestNamespace), operandName, nil)
	guestEventRecorder := controllerConfig.EventRecorder

	// Create client and informers for our ClusterCSIDriver CR
	gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
	guestOperatorClient, guestDynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(guestKubeConfig, gvr, string(opv1.AWSEBSCSIDriver))
	if err != nil {
		return err
	}

	// Start control plane controllers
	controlPlaneCSIControllerSet := csicontrollerset.NewCSIControllerSet(
		guestOperatorClient,
		controlPlaneEventRecorder,
	).WithLogLevelController().WithManagementStateController(
		operandName,
		false,
	).WithStaticResourcesController(
		"AWSEBSDriverStaticResourcesController",
		kubeClient,
		dynamicClient,
		kubeInformersForNamespaces,
		assetWithNamespaceFunc(controlPlaneNamespace),
		[]string{
			"storageclass_gp2.yaml",
			"csidriver.yaml",
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
		kubeClient,
		kubeInformersForNamespaces.InformersFor(controlPlaneNamespace),
		guestConfigInformers,
		[]factory.Informer{
			secretInformer.Informer(),
			nodeInformer.Informer(),
			cloudConfigInformer.Informer(),
			guestInfraInformer.Informer(),
			configMapInformer.Informer(),
		},
		withHypershiftDeploymentHook(),
		withNamespaceDeploymentHook(controlPlaneNamespace),
		csidrivercontrollerservicecontroller.WithSecretHashAnnotationHook(controlPlaneNamespace, secretName, secretInformer),
		csidrivercontrollerservicecontroller.WithObservedProxyDeploymentHook(),
		csidrivercontrollerservicecontroller.WithReplicasHook(nodeInformer.Lister()),
		withCustomCABundle(cloudConfigLister),
		withCustomTags(guestInfraInformer.Lister()),
		withCustomEndPoint(guestInfraInformer.Lister()),
		csidrivercontrollerservicecontroller.WithCABundleDeploymentHook(
			controlPlaneNamespace,
			trustedCAConfigMap,
			configMapInformer,
		),
	).WithServiceMonitorController(
		"AWSEBSDriverServiceMonitorController",
		dynamicClient,
		assetWithNamespaceFunc(controlPlaneNamespace),
		"servicemonitor.yaml",
	).WithStorageClassController(
		"AWSEBSDriverStorageClassController",
		assets.ReadFile,
		"storageclass_gp3.yaml",
		kubeClient,
		kubeInformersForNamespaces.InformersFor(""),
	)
	if err != nil {
		return err
	}

	guestCSIControllerSet := csicontrollerset.NewCSIControllerSet(
		guestOperatorClient,
		guestEventRecorder,
	).WithStaticResourcesController(
		"AWSEBSDriverGuestStaticResourcesController",
		guestKubeClient,
		guestDynamicClient,
		guestKubeInformersForNamespaces,
		assetWithNamespaceFunc(guestNamespace),
		[]string{
			"storageclass_gp2.yaml",
			"storageclass_gp3.yaml",
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
		withNamespaceDaemonSetHook(guestNamespace),
		csidrivernodeservicecontroller.WithObservedProxyDaemonSetHook(),
		csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
			guestNamespace,
			trustedCAConfigMap,
			guestConfigMapInformer,
		),
	)

	caSyncController, err := newCustomCABundleSyncer(
		guestOperatorClient,
		kubeInformersForNamespaces,
		kubeClient,
		cloudConfigNamespace,
		controlPlaneNamespace,
		controlPlaneEventRecorder,
	)
	if err != nil {
		return fmt.Errorf("could not create the custom CA bundle syncer: %w", err)
	}

	klog.Info("Starting the control plane informers")
	go kubeInformersForNamespaces.Start(ctx.Done())

	klog.Info("Starting control plane controllerset")
	// Only start caSyncController in standalone clusters because in Hypershift
	// the ConfigMap is already available in the correct namespace.
	// FIXME(bertinatto): this depends on https://issues.redhat.com/browse/HOSTEDCP-544
	if controlPlaneNamespace == guestNamespace {
		go caSyncController.Run(ctx, 1)
	}
	go controlPlaneCSIControllerSet.Run(ctx, 1)

	klog.Info("Starting the guest cluster informers")
	go guestKubeInformersForNamespaces.Start(ctx.Done())
	go guestDynamicInformers.Start(ctx.Done())
	go guestConfigInformers.Start(ctx.Done())

	klog.Info("Starting guest cluster controllerset")
	go guestCSIControllerSet.Run(ctx, 1)

	<-ctx.Done()

	return fmt.Errorf("stopped")
}

type controllerTemplateData struct {
	CABundleConfigMap string
}

// withCustomCABundle executes the asset as a template to fill out the parts required when using a custom CA bundle.
// The `caBundleConfigMap` parameter specifies the name of the ConfigMap containing the custom CA bundle. If the
// argument supplied is empty, then no custom CA bundle will be used.
func withCustomCABundle(cloudConfigLister corev1listers.ConfigMapNamespaceLister) deploymentcontroller.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		switch used, err := isCustomCABundleUsed(cloudConfigLister); {
		case err != nil:
			return fmt.Errorf("could not determine if a custom CA bundle is in use: %w", err)
		case !used:
			return nil
		}
		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: cloudConfigName},
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

func withCustomEndPoint(infraLister v1.InfrastructureLister) deploymentcontroller.DeploymentHookFunc {
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

func newCustomCABundleSyncer(
	operatorClient v1helpers.OperatorClient,
	kubeInformers v1helpers.KubeInformersForNamespaces,
	kubeClient kubeclient.Interface,
	sourceNamespace string,
	destinationNamespace string,
	eventRecorder events.Recorder,
) (factory.Controller, error) {
	// sync config map with additional trust bundle to the operator namespace,
	// so the operator can get it as a ConfigMap volume.
	srcConfigMap := resourcesynccontroller.ResourceLocation{
		Namespace: sourceNamespace,
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

// isCustomCABundleUsed returns true if the cloud config ConfigMap exists and contains a custom CA bundle.
func isCustomCABundleUsed(cloudConfigLister corev1listers.ConfigMapNamespaceLister) (bool, error) {
	cloudConfigCM, err := cloudConfigLister.Get(cloudConfigName)
	if errors.IsNotFound(err) {
		// no cloud config ConfigMap so there is no CA bundle
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get the %s/%s ConfigMap: %w", cloudConfigNamespace, cloudConfigName, err)
	}
	_, exists := cloudConfigCM.Data[caBundleKey]
	return exists, nil
}

// withCustomTags add tags from Infrastructure.Status.PlatformStatus.AWS.ResourceTags to the driver command line as
// --extra-tags=<key1>=<value1>,<key2>=<value2>,...
func withCustomTags(infraLister v1.InfrastructureLister) deploymentcontroller.DeploymentHookFunc {
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

func withNamespaceDeploymentHook(namespace string) deploymentcontroller.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		deployment.Namespace = namespace
		return nil
	}
}

func withNamespaceDaemonSetHook(namespace string) csidrivernodeservicecontroller.DaemonSetHookFunc {
	return func(_ *opv1.OperatorSpec, ds *appsv1.DaemonSet) error {
		ds.Namespace = namespace
		return nil
	}
}

func withHypershiftDeploymentHook() deploymentcontroller.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		// TODO: quit if not hypershift
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
					SecretName: "service-network-admin-kubeconfig",
				},
			},
		}
		podSpec := &deployment.Spec.Template.Spec
		for i := range podSpec.InitContainers {
			container := &podSpec.InitContainers[i]
			container.Env = append(container.Env, kubeConfigEnvVar)
			container.VolumeMounts = append(container.VolumeMounts, volumeMount)
		}
		for i := range podSpec.Containers {
			container := &podSpec.InitContainers[i]
			container.VolumeMounts = append(container.VolumeMounts, volumeMount)
			container.Env = append(container.Env, kubeConfigEnvVar)
		}
		podSpec.Volumes = append(podSpec.Volumes, volume)
		return nil
	}
}
