package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/aws"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/clients"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/csi/csicontrollerset"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivercontrollerservicecontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
)

const (
	operatorName = "aws-ebs-csi-driver-operator"
	operandName  = "aws-ebs-csi-driver"

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
	controlPlaneNamespace := controllerConfig.OperatorNamespace

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
	genConfig, opConfig, err := aws.GetAWSEBSConfig()
	if err != nil {
		return err
	}
	gen := merge.NewAssetGenerator(runtimeConfig, genConfig)
	a, err := gen.GenerateAssets()
	if err != nil {
		return err
	}

	controlPlaneControllerInformers := []factory.Informer{}
	controllerHooks := []dc.DeploymentHookFunc{}
	for _, hookBuilder := range opConfig.ControlPlaneDeploymentHooks {
		if hookBuilder.ClusterFlavours.Has(flavour) {
			hook, informers := hookBuilder.Hook(c)
			controllerHooks = append(controllerHooks, hook)
			controlPlaneControllerInformers = append(controlPlaneControllerInformers, informers...)
		}
	}

	if len(opConfig.ControlPlaneWatchedSecretNames) > 0 {
		controlPlaneSecretInformer := c.GetControlPlaneSecretInformer(controlPlaneNamespace)
		for _, secretName := range opConfig.ControlPlaneWatchedSecretNames {
			controllerHooks = append(controllerHooks, csidrivercontrollerservicecontroller.WithSecretHashAnnotationHook(controlPlaneNamespace, secretName, controlPlaneSecretInformer))
		}
		controlPlaneControllerInformers = append(controlPlaneControllerInformers, controlPlaneSecretInformer.Informer())
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
		a.GetControllerStaticAssetNames(),
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

	controllers := []factory.Controller{}
	for _, builder := range opConfig.ExtraControlPlaneControllers {
		if builder.ClusterFlavours.Has(flavour) {
			controller, err := builder.ControllerBuilder(c)
			if err != nil {
				return err
			}
			controllers = append(controllers, controller)
		}
	}

	// Start controllers that manage resources in the GUEST cluster.
	guestInformers := []factory.Informer{}
	dsHooks := []csidrivernodeservicecontroller.DaemonSetHookFunc{
		csidrivernodeservicecontroller.WithObservedProxyDaemonSetHook(),
	}
	for _, hookBuilder := range opConfig.GuestDaemonSetHooks {
		if hookBuilder.ClusterFlavours.Has(flavour) {
			hook, informers := hookBuilder.Hook(c)
			guestInformers = append(guestInformers, informers...)
			dsHooks = append(dsHooks, hook)
		}
	}

	guestCSIControllerSet := csicontrollerset.NewCSIControllerSet(
		c.OperatorClient,
		c.EventRecorder,
	).WithStaticResourcesController(
		"AWSEBSDriverGuestStaticResourcesController",
		c.GuestKubeClient,
		c.GuestDynamicClient,
		c.GuestKubeInformers,
		a.GetAsset,
		a.GetGuestStaticAssetNames(),
	).
		// TODO: conditional assets
		/*
			WithConditionalStaticResourcesController(
					"AWSEBSDriverConditionalStaticResourcesController",
					c.GuestKubeClient,
			c.GuestDynamicClient,
			c.GuestKubeInformers,
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
				).
		*/
		WithCSIDriverNodeService(
			"AWSEBSDriverNodeServiceController",
			a.GetAsset,
			merge.NodeDaemonSetAssetName,
			c.GuestKubeClient,
			c.GuestKubeInformers.InformersFor(clients.CSIDriverNamespace),
			guestInformers,
			dsHooks...,
		).WithStorageClassController(
		"AWSEBSDriverStorageClassController",
		a.GetAsset,
		a.GetStorageClassAssetNames(),
		c.GuestKubeClient,
		c.GuestKubeInformers.InformersFor(""),
		c.GuestOperatorInformers,
	)

	c.Start(ctx)
	klog.V(2).Infof("Waiting for informers to sync")
	c.WaitForCacheSync(ctx)
	klog.V(2).Infof("Informers synced")

	for _, controller := range controllers {
		klog.Infof("Starting controller %s", controller.Name())
		go controller.Run(ctx, 1)
	}
	klog.Info("Starting control plane controllerset")
	go controlPlaneCSIControllerSet.Run(ctx, 1)
	klog.Info("Starting guest controllerset")
	go guestCSIControllerSet.Run(ctx, 1)

	<-ctx.Done()

	return fmt.Errorf("stopped")
}
