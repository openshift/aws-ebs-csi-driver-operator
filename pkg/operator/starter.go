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
	"github.com/openshift/library-go/pkg/operator/management"
	"github.com/openshift/library-go/pkg/operator/status"

	"github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/common"
	clientset "github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/generated/clientset/versioned"
	informers "github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/generated/informers/externalversions"
)

const (
	resync = 20 * time.Minute
)

func RunOperator(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	cb, err := common.NewBuilder("")
	if err != nil {
		klog.Fatalf("error creating clients: %v", err)
	}
	ctrlCtx := common.CreateControllerContext(cb, ctx.Done(), targetNamespace)

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
		ctrlCtx.KubeNamespacedInformerFactory.Apps().V1().Deployments(),
		ctrlCtx.ClientBuilder.KubeClientOrDie(targetName),
		versionGetter,
		controllerConfig.EventRecorder,
		os.Getenv(operatorVersionEnvName),
		os.Getenv(operandVersionEnvName),
		os.Getenv(operandImageEnvName),
	)

	// TODO remove this controller once we support Removed
	managementStateController := management.NewOperatorManagementStateController(targetName, operatorClient, controllerConfig.EventRecorder)
	management.SetOperatorNotRemovable()

	klog.Info("Starting the Informers.")
	for _, informer := range []interface {
		Start(stopCh <-chan struct{})
	}{
		ctrlInformers,
		apiInformers,
		ctrlCtx.APIExtInformerFactory,         // CRDs
		ctrlCtx.KubeNamespacedInformerFactory, // operand Deployment
	} {
		informer.Start(ctx.Done())
	}

	klog.Info("Starting the controllers")
	for _, controller := range []interface {
		Run(ctx context.Context, workers int)
	}{
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
