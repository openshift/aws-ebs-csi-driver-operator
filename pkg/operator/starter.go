package operator

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	defaultNamespace   = "openshift-cluster-csi-drivers"
	operatorName       = "aws-ebs-csi-driver-operator"
	operandName        = "aws-ebs-csi-driver"
	instanceName       = "ebs.csi.aws.com"
	secretName         = "ebs-cloud-credentials"
	infraConfigName    = "cluster"
	trustedCAConfigMap = "aws-ebs-csi-driver-trusted-ca-bundle"

	cloudConfigNamespace = "openshift-config-managed"
	cloudConfigName      = "kube-cloud-config"
	caBundleKey          = "ca-bundle.pem"

	infrastructureName = "cluster"
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	// Create core clientset and informer
	kubeClient := kubeclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient, defaultNamespace, cloudConfigNamespace, "")
	secretInformer := kubeInformersForNamespaces.InformersFor(defaultNamespace).Core().V1().Secrets()
	nodeInformer := kubeInformersForNamespaces.InformersFor("").Core().V1().Nodes()
	configMapInformer := kubeInformersForNamespaces.InformersFor(defaultNamespace).Core().V1().ConfigMaps()

	// Create config clientset and informer. This is used to get the cluster ID
	configClient := configclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	configInformers := configinformers.NewSharedInformerFactory(configClient, 20*time.Minute)
	infraInformer := configInformers.Config().V1().Infrastructures()

	// Create informer for the ConfigMaps in the openshift-config-managed namespace. This is used to get the custom CA
	// bundle to use when accessing the AWS API.
	cloudConfigInformer := kubeInformersForNamespaces.InformersFor(cloudConfigNamespace).Core().V1().ConfigMaps()
	cloudConfigLister := cloudConfigInformer.Lister().ConfigMaps(cloudConfigNamespace)

	// Create GenericOperatorclient. This is used by the library-go controllers created down below
	gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
	operatorClient, dynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(controllerConfig.KubeConfig, gvr, instanceName)
	if err != nil {
		return err
	}

	dynamicClient, err := dynamic.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}

	csiControllerSet := csicontrollerset.NewCSIControllerSet(
		operatorClient,
		controllerConfig.EventRecorder,
	).WithLogLevelController().WithManagementStateController(
		operandName,
		false,
	).WithStaticResourcesController(
		"AWSEBSDriverStaticResourcesController",
		kubeClient,
		dynamicClient,
		kubeInformersForNamespaces,
		assets.ReadFile,
		[]string{
			"storageclass_gp2.yaml",
			"storageclass_gp3.yaml",
			"volumesnapshotclass.yaml",
			"csidriver.yaml",
			"controller_sa.yaml",
			"controller_pdb.yaml",
			"node_sa.yaml",
			"service.yaml",
			"cabundle_cm.yaml",
			"rbac/attacher_role.yaml",
			"rbac/attacher_binding.yaml",
			"rbac/privileged_role.yaml",
			"rbac/controller_privileged_binding.yaml",
			"rbac/node_privileged_binding.yaml",
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
	).WithCSIConfigObserverController(
		"AWSEBSDriverCSIConfigObserverController",
		configInformers,
	).WithCSIDriverControllerService(
		"AWSEBSDriverControllerServiceController",
		assets.ReadFile,
		"controller.yaml",
		kubeClient,
		kubeInformersForNamespaces.InformersFor(defaultNamespace),
		configInformers,
		[]factory.Informer{
			secretInformer.Informer(),
			nodeInformer.Informer(),
			cloudConfigInformer.Informer(),
			infraInformer.Informer(),
			configMapInformer.Informer(),
		},
		csidrivercontrollerservicecontroller.WithSecretHashAnnotationHook(defaultNamespace, secretName, secretInformer),
		csidrivercontrollerservicecontroller.WithObservedProxyDeploymentHook(),
		csidrivercontrollerservicecontroller.WithReplicasHook(nodeInformer.Lister()),
		withCustomCABundle(cloudConfigLister),
		withCustomTags(infraInformer.Lister()),
		csidrivercontrollerservicecontroller.WithCABundleDeploymentHook(
			defaultNamespace,
			trustedCAConfigMap,
			configMapInformer,
		),
	).WithCSIDriverNodeService(
		"AWSEBSDriverNodeServiceController",
		assets.ReadFile,
		"node.yaml",
		kubeClient,
		kubeInformersForNamespaces.InformersFor(defaultNamespace),
		[]factory.Informer{configMapInformer.Informer()},
		csidrivernodeservicecontroller.WithObservedProxyDaemonSetHook(),
		csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
			defaultNamespace,
			trustedCAConfigMap,
			configMapInformer,
		),
	).WithServiceMonitorController(
		"AWSEBSDriverServiceMonitorController",
		dynamicClient,
		assets.ReadFile,
		"servicemonitor.yaml",
	)
	if err != nil {
		return err
	}

	caSyncController, err := newCustomCABundleSyncer(
		operatorClient,
		kubeInformersForNamespaces,
		kubeClient,
		controllerConfig.EventRecorder,
	)
	if err != nil {
		return fmt.Errorf("could not create the custom CA bundle syncer: %w", err)
	}

	klog.Info("Starting the informers")
	go kubeInformersForNamespaces.Start(ctx.Done())
	go dynamicInformers.Start(ctx.Done())
	go configInformers.Start(ctx.Done())

	klog.Info("Starting controllerset")
	go csiControllerSet.Run(ctx, 1)
	go caSyncController.Run(ctx, 1)

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

func newCustomCABundleSyncer(
	operatorClient v1helpers.OperatorClient,
	kubeInformers v1helpers.KubeInformersForNamespaces,
	kubeClient kubeclient.Interface,
	eventRecorder events.Recorder,
) (factory.Controller, error) {
	// sync config map with additional trust bundle to the operator namespace,
	// so the operator can get it as a ConfigMap volume.
	srcConfigMap := resourcesynccontroller.ResourceLocation{
		Namespace: cloudConfigNamespace,
		Name:      cloudConfigName,
	}
	dstConfigMap := resourcesynccontroller.ResourceLocation{
		Namespace: defaultNamespace,
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
//  --extra-tags=<key1>=<value1>,<key2>=<value2>,...
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
