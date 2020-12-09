package operator

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	opv1 "github.com/openshift/api/operator/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/csi/csicontrollerset"
	goc "github.com/openshift/library-go/pkg/operator/genericoperatorclient"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehash"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated"
)

const (
	// Operand and operator run in the same namespace
	defaultNamespace = "openshift-cluster-csi-drivers"
	operatorName     = "aws-ebs-csi-driver-operator"
	operandName      = "aws-ebs-csi-driver"
	instanceName     = "ebs.csi.aws.com"
	secretName       = "ebs-cloud-credentials"

	cloudConfigNamespace = "openshift-config-managed"
	cloudConfigName      = "kube-cloud-config"
	caBundleKey          = "ca-bundle.pem"
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	// Create core clientset and informer
	kubeClient := kubeclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient, defaultNamespace, cloudConfigNamespace, "")

	// Create config clientset and informer. This is used to get the cluster ID
	configClient := configclient.NewForConfigOrDie(rest.AddUserAgent(controllerConfig.KubeConfig, operatorName))
	configInformers := configinformers.NewSharedInformerFactory(configClient, 20*time.Minute)

	// Create GenericOperatorclient. This is used by the library-go controllers created down below
	gvr := opv1.SchemeGroupVersion.WithResource("clustercsidrivers")
	operatorClient, dynamicInformers, err := goc.NewClusterScopedOperatorClientWithConfigName(controllerConfig.KubeConfig, gvr, instanceName)
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
		kubeInformersForNamespaces,
		generated.Asset,
		[]string{
			"storageclass.yaml",
			"csidriver.yaml",
			"controller_sa.yaml",
			"node_sa.yaml",
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
		},
	).WithCSIDriverControllerService(
		"AWSEBSDriverControllerServiceController",
		withCustomCABundle(generated.MustAsset, kubeClient),
		"controller.yaml",
		kubeClient,
		kubeInformersForNamespaces.InformersFor(defaultNamespace),
		configInformers,
		withSecretHashAnnotation(kubeInformersForNamespaces, defaultNamespace, secretName),
	).WithCSIDriverNodeService(
		"AWSEBSDriverNodeServiceController",
		generated.MustAsset,
		"node.yaml",
		kubeClient,
		kubeInformersForNamespaces.InformersFor(defaultNamespace),
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
func withCustomCABundle(assetFunc func(string) []byte, kubeClient kubeclient.Interface) func(string) []byte {
	templateData := controllerTemplateData{}
	switch used, err := isCustomCABundleUsed(kubeClient); {
	case err != nil:
		klog.Fatalf("could not determine if a custom CA bundle is in use: %v", err)
	case used:
		templateData.CABundleConfigMap = cloudConfigName
	}
	return func(name string) []byte {
		asset := assetFunc(name)
		template := template.Must(template.New("template").Parse(string(asset)))
		buf := &bytes.Buffer{}
		if err := template.Execute(buf, templateData); err != nil {
			klog.Fatalf("Failed to execute ")
		}
		return buf.Bytes()
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
func isCustomCABundleUsed(kubeClient kubeclient.Interface) (bool, error) {
	cloudConfigCM, err := kubeClient.CoreV1().
		ConfigMaps(cloudConfigNamespace).
		Get(context.Background(), cloudConfigName, metav1.GetOptions{})
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

func withSecretHashAnnotation(kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces, namespace, secret string) func(*appsv1.Deployment) error {
	return func(deployment *appsv1.Deployment) error {
		inputHashes, err := resourcehash.MultipleObjectHashStringMapForObjectReferenceFromLister(
			kubeInformersForNamespaces.InformersFor(namespace).Core().V1().ConfigMaps().Lister(),
			kubeInformersForNamespaces.InformersFor(namespace).Core().V1().Secrets().Lister(),
			resourcehash.NewObjectRef().ForSecret().InNamespace(namespace).Named(secret),
		)
		if err != nil {
			return fmt.Errorf("invalid dependency reference: %w", err)
		}
		if deployment.Annotations == nil {
			deployment.Annotations = map[string]string{}
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		for k, v := range inputHashes {
			annotationKey := fmt.Sprintf("operator.openshift.io/dep-%s", k)
			deployment.Annotations[annotationKey] = v
			deployment.Spec.Template.Annotations[annotationKey] = v
		}
		return nil
	}
}
