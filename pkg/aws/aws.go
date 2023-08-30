package aws

import (
	"fmt"
	"strings"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/clients"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/operator/config"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivercontrollerservicecontroller"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	"github.com/openshift/library-go/pkg/operator/csi/csistorageclasscontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

const (
	cloudCredSecretName   = "ebs-cloud-credentials"
	metricsCertSecretName = "aws-ebs-csi-driver-controller-metrics-serving-cert"
	infrastructureName    = "cluster"
	cloudConfigNamespace  = "openshift-config-managed"
	cloudConfigName       = "kube-cloud-config"
	caBundleKey           = "ca-bundle.pem"
	trustedCAConfigMap    = "aws-ebs-csi-driver-trusted-ca-bundle"
	kmsKeyID              = "kmsKeyId"
)

func GetAWSEBSConfig() (*merge.CSIDriverAssetConfig, *config.OperatorConfig, error) {
	assetCfg := &merge.CSIDriverAssetConfig{
		AssetPrefix:      "aws-ebs-csi-driver",
		AssetShortPrefix: "ebs",
		DriverName:       "ebs.csi.aws.com",

		ControllerConfig: &merge.ControlPlaneConfig{
			DeploymentTemplateAssetName: "drivers/aws-ebs/controller.yaml",
			LivenessProbePort:           10301,
			MetricsPorts: []merge.MetricsPort{
				{
					LocalPort:           8201,
					InjectKubeRBACProxy: true,
					ExposedPort:         9201,
					Name:                "driver-m",
				},
			},
			SidecarLocalMetricsPortStart:   8202,
			SidecarExposedMetricsPortStart: 9202,
			Sidecars: []merge.SidecarConfig{
				merge.DefaultProvisionerWithSnapshots.WithExtraArguments(
					"--default-fstype=ext4",
					"--feature-gates=Topology=true",
					"--extra-create-metadata=true",
					"--timeout=60s",
				),
				merge.DefaultAttacher.WithExtraArguments(
					"--timeout=60s",
				),
				merge.DefaultResizer.WithExtraArguments(
					"--timeout=300s",
				),
				merge.DefaultSnapshotter.WithExtraArguments(
					"--timeout=300s",
					"--extra-create-metadata",
				),
				merge.DefaultLivenessProbe.WithExtraArguments(
					"--probe-timeout=3s",
				),
			},
			StaticAssets: merge.DefaultControllerAssets,
			AssetPatches: merge.DefaultAssetPatches.WithPatches(merge.HyperShiftOnly,
				"controller.yaml", "drivers/aws-ebs/patches/controller_minter.yaml",
			),
		},

		GuestConfig: &merge.GuestConfig{
			DaemonSetTemplateAssetName: "drivers/aws-ebs/node.yaml",
			LivenessProbePort:          10300,
			Sidecars: []merge.SidecarConfig{
				merge.DefaultNodeDriverRegistrar,
				merge.DefaultLivenessProbe.WithExtraArguments(
					"--probe-timeout=3s",
				),
			},
			StaticAssets: merge.DefaultNodeAssets.WithAssets(merge.AllFlavours,
				"drivers/aws-ebs/csidriver.yaml",
				// TODO: volumesnapshotclass must be conditional - when snapshot capability is enabled
				"drivers/aws-ebs/volumesnapshotclass.yaml",
			),
			StorageClassAssetNames: []string{
				"drivers/aws-ebs/storageclass_gp2.yaml",
				"drivers/aws-ebs/storageclass_gp3.yaml",
			},
		},
	}

	opCfg := &config.OperatorConfig{
		ControlPlaneDeploymentHooks: config.DefaultControllerHooks.WithHooks(merge.AllFlavours,
			withAWSRegion,
			withCustomTags,
			withCustomEndPoint,
			withCABundleDeploymentHook,
		).WithHooks(merge.StandaloneOnly,
			getCustomAWSCABundleBuilder(cloudConfigName),
		).WithHooks(merge.HyperShiftOnly,
			getCustomAWSCABundleBuilder("user-ca-bundle"),
		),

		ControlPlaneWatchedSecretNames: []string{
			cloudCredSecretName,
			metricsCertSecretName,
		},

		ExtraControlPlaneControllers: config.NewControllerBuilders(merge.StandaloneOnly,
			newCustomAWSBundleSyncer),

		GuestDaemonSetHooks: config.DefaultDaemonSetHooks.WithHooks(merge.AllFlavours,
			withCABundleDaemonSetHook,
		),

		StorageClassHooks: []config.StorageClassHookBuilder{
			getKMSKeyHook,
		},
	}
	return assetCfg, opCfg, nil
}

// withCustomAWSCABundle executes the asset as a template to fill out the parts required when using a custom CA bundle.
// The `caBundleConfigMap` parameter specifies the name of the ConfigMap containing the custom CA bundle. If the
// argument supplied is empty, then no custom CA bundle will be used.
func getCustomAWSCABundleBuilder(cmName string) config.DeploymentHookBuilder {
	return func(c *clients.Clients) (dc.DeploymentHookFunc, []factory.Informer) {
		hook := func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
			cloudConfigLister := c.GetControlPlaneConfigMapInformer(c.ControlPlaneNamespace).Lister().ConfigMaps(c.ControlPlaneNamespace)
			configName, err := customAWSCABundle(cmName, cloudConfigLister)
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
		informers := []factory.Informer{
			c.GetControlPlaneConfigMapInformer(c.ControlPlaneNamespace).Informer(),
		}
		return hook, informers
	}
}

func withCustomEndPoint(c *clients.Clients) (dc.DeploymentHookFunc, []factory.Informer) {
	hook := func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		infraLister := c.GetGuestInfraInformer().Lister()
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
	informers := []factory.Informer{
		c.GetGuestInfraInformer().Informer(),
	}
	return hook, informers
}

func newCustomAWSBundleSyncer(c *clients.Clients) (factory.Controller, error) {
	// sync config map with additional trust bundle to the operator namespace,
	// so the operator can get it as a ConfigMap volume.
	srcConfigMap := resourcesynccontroller.ResourceLocation{
		Namespace: cloudConfigNamespace,
		Name:      cloudConfigName,
	}
	dstConfigMap := resourcesynccontroller.ResourceLocation{
		Namespace: clients.CSIDriverNamespace,
		Name:      cloudConfigName,
	}
	certController := resourcesynccontroller.NewResourceSyncController(
		c.OperatorClient,
		c.GuestKubeInformers,
		c.GuestKubeClient.CoreV1(),
		c.GuestKubeClient.CoreV1(),
		c.EventRecorder)
	err := certController.SyncConfigMap(dstConfigMap, srcConfigMap)
	if err != nil {
		return nil, err
	}
	return certController, nil
}

// customAWSCABundle returns true if the cloud config ConfigMap exists and contains a custom CA bundle.
func customAWSCABundle(configName string, cloudConfigLister corev1listers.ConfigMapNamespaceLister) (string, error) {
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
func withCustomTags(c *clients.Clients) (dc.DeploymentHookFunc, []factory.Informer) {
	hook := func(spec *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		infraLister := c.GetGuestInfraInformer().Lister()
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
	informers := []factory.Informer{
		c.GetGuestInfraInformer().Informer(),
	}
	return hook, informers
}

func withAWSRegion(c *clients.Clients) (dc.DeploymentHookFunc, []factory.Informer) {
	hook := func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		infraLister := c.GetGuestInfraInformer().Lister()
		infra, err := infraLister.Get(infrastructureName)
		if err != nil {
			return err
		}

		if infra.Status.PlatformStatus == nil || infra.Status.PlatformStatus.AWS == nil {
			return nil
		}

		region := infra.Status.PlatformStatus.AWS.Region
		if region == "" {
			return nil
		}

		for i := range deployment.Spec.Template.Spec.Containers {
			container := &deployment.Spec.Template.Spec.Containers[i]
			if container.Name != "csi-driver" {
				continue
			}
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  "AWS_REGION",
				Value: region,
			})
		}
		return nil
	}
	informers := []factory.Informer{
		c.GetGuestInfraInformer().Informer(),
	}
	return hook, informers
}

// getKMSKeyHook checks for AWSCSIDriverConfigSpec in the ClusterCSIDriver object.
// If it contains KMSKeyARN, it sets the corresponding parameter in the StorageClass.
// This allows the admin to specify a customer managed key to be used by default.
func getKMSKeyHook(c *clients.Clients) (csistorageclasscontroller.StorageClassHookFunc, []factory.Informer) {
	hook := func(_ *opv1.OperatorSpec, class *storagev1.StorageClass) error {
		ccdLister := c.GuestOperatorInformers.Operator().V1().ClusterCSIDrivers().Lister()
		ccd, err := ccdLister.Get(class.Provisioner)
		if err != nil {
			return err
		}

		driverConfig := ccd.Spec.DriverConfig
		if driverConfig.DriverType != opv1.AWSDriverType || driverConfig.AWS == nil {
			klog.V(4).Infof("No AWSCSIDriverConfigSpec defined for %s", class.Provisioner)
			return nil
		}

		arn := driverConfig.AWS.KMSKeyARN
		if arn == "" {
			klog.V(4).Infof("Not setting empty %s parameter in StorageClass %s", kmsKeyID, class.Name)
			return nil
		}

		if class.Parameters == nil {
			class.Parameters = map[string]string{}
		}
		klog.V(4).Infof("Setting %s = %s in StorageClass %s", kmsKeyID, arn, class.Name)
		class.Parameters[kmsKeyID] = arn
		return nil
	}
	informers := []factory.Informer{
		c.GuestOperatorInformers.Operator().V1().ClusterCSIDrivers().Informer(),
	}
	return hook, informers
}

func withCABundleDeploymentHook(c *clients.Clients) (dc.DeploymentHookFunc, []factory.Informer) {
	hook := csidrivercontrollerservicecontroller.WithCABundleDeploymentHook(
		c.ControlPlaneNamespace,
		trustedCAConfigMap,
		c.GetControlPlaneConfigMapInformer(c.ControlPlaneNamespace),
	)
	informers := []factory.Informer{
		c.GetControlPlaneConfigMapInformer(c.ControlPlaneNamespace).Informer(),
	}
	return hook, informers
}

func withCABundleDaemonSetHook(c *clients.Clients) (csidrivernodeservicecontroller.DaemonSetHookFunc, []factory.Informer) {
	hook := csidrivernodeservicecontroller.WithCABundleDaemonSetHook(
		clients.CSIDriverNamespace,
		trustedCAConfigMap,
		c.GetGuestConfigMapInformer(clients.CSIDriverNamespace),
	)
	informers := []factory.Informer{
		c.GetGuestConfigMapInformer(clients.CSIDriverNamespace).Informer(),
	}
	return hook, informers
}
