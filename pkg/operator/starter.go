package operator

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/klog"

	apiclientset "github.com/openshift/client-go/config/clientset/versioned"
	apiinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/loglevel"
	"github.com/openshift/library-go/pkg/operator/management"
	"github.com/openshift/library-go/pkg/operator/status"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/common"
	clientset "github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated/clientset/versioned"
	informers "github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated/informers/externalversions"
)

const (
	resync = 20 * time.Minute
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	cb, err := common.NewBuilder("")
	if err != nil {
		klog.Fatalf("error creating clients: %v", err)
	}
	ctrlCtx := common.CreateControllerContext(cb, ctx.Done(), operandNamespace)

	ctrlClientset, err := clientset.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}

	ctrlInformers := informers.NewSharedInformerFactoryWithOptions(
		ctrlClientset,
		resync,
		informers.WithTweakListOptions(singleNameListOptions(globalConfigName)),
	)

	apiClientset, err := apiclientset.NewForConfig(controllerConfig.KubeConfig)
	if err != nil {
		return err
	}

	apiInformers := apiinformers.NewSharedInformerFactoryWithOptions(apiClientset, resync)

	operatorClient := &OperatorClient{
		ctrlInformers,
		ctrlClientset.CsiV1alpha1(),
	}

	versionGetter := status.NewVersionGetter()

	operator := NewCSIDriverOperator(
		*operatorClient,
		ctrlCtx.KubeNamespacedInformerFactory.Core().V1().Namespaces(),
		ctrlCtx.KubeNamespacedInformerFactory.Storage().V1beta1().CSIDrivers(),
		ctrlCtx.KubeNamespacedInformerFactory.Core().V1().ServiceAccounts(),
		ctrlCtx.KubeNamespacedInformerFactory.Rbac().V1().ClusterRoles(),
		ctrlCtx.KubeNamespacedInformerFactory.Rbac().V1().ClusterRoleBindings(),
		ctrlCtx.KubeNamespacedInformerFactory.Apps().V1().Deployments(),
		ctrlCtx.KubeNamespacedInformerFactory.Apps().V1().DaemonSets(),
		ctrlCtx.KubeNamespacedInformerFactory.Storage().V1().StorageClasses(),
		ctrlCtx.ClientBuilder.KubeClientOrDie(operandName),
		versionGetter,
		controllerConfig.EventRecorder,
		os.Getenv(operatorVersionEnvName),
		os.Getenv(operandVersionEnvName),
		os.Getenv(operandImageEnvName),
	)

	logLevelController := loglevel.NewClusterOperatorLoggingController(operatorClient, controllerConfig.EventRecorder)
	// TODO remove this controller once we support Removed
	managementStateController := management.NewOperatorManagementStateController(operandName, operatorClient, controllerConfig.EventRecorder)
	management.SetOperatorNotRemovable()

	klog.Info("Starting the Informers.")
	for _, informer := range []interface {
		Start(stopCh <-chan struct{})
	}{
		ctrlInformers,
		apiInformers,
		ctrlCtx.KubeNamespacedInformerFactory,
	} {
		informer.Start(ctx.Done())
	}

	klog.Info("Starting the controllers")
	for _, controller := range []interface {
		Run(ctx context.Context, workers int)
	}{
		logLevelController,
		managementStateController,
	} {
		go controller.Run(ctx, 1)
	}
	klog.Info("Starting the operator.")
	go operator.Run(1, ctx.Done())

	<-ctx.Done()

	return fmt.Errorf("stopped")
}

func singleNameListOptions(name string) func(opts *metav1.ListOptions) {
	return func(opts *metav1.ListOptions) {
		opts.FieldSelector = fields.OneTermEqualSelector("metadata.name", name).String()
	}
}
