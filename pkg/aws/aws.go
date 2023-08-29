package aws

import (
	"fmt"
	"strings"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/clients"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/merge"
	v1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivercontrollerservicecontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kubeclient "k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

const (
	cloudCredSecretName   = "ebs-cloud-credentials"
	metricsCertSecretName = "aws-ebs-csi-driver-controller-metrics-serving-cert"
	infrastructureName    = "cluster"
	cloudConfigNamespace  = "openshift-config-managed"
	cloudConfigName       = "kube-cloud-config"
	caBundleKey           = "ca-bundle.pem"
	trustedCAConfigMap    = "aws-ebs-csi-driver-trusted-ca-bundle"
)

func GetAWSEBSConfig(c *clients.Clients) (*merge.CSIDriverOperatorConfig, error) {
	cfg := &merge.CSIDriverOperatorConfig{
		AssetPrefix:      "aws-ebs-csi-driver",
		AssetShortPrefix: "ebs",

		ControllerConfig: &merge.ControllPlaneConfig{
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
			WatchedSecretNames: []string{
				cloudCredSecretName,
				metricsCertSecretName,
			},
			DeploymentHooks: merge.DefaultControllerHooks.WithHooks(merge.AllFlavours,
				withAWSRegion(c.GetGuestInfraInformer().Lister()),
				withCustomTags(c.GetGuestInfraInformer().Lister()),
				withCustomEndPoint(c.GetGuestInfraInformer().Lister()),
				csidrivercontrollerservicecontroller.WithCABundleDeploymentHook(
					c.ControlPlaneNamespace,
					trustedCAConfigMap,
					c.GetControlPlaneConfigMapInformer(c.ControlPlaneNamespace),
				),
			).WithHooks(merge.StandaloneOnly,
				withCustomAWSCABundle(cloudConfigName, c.GetGuestConfigMapInformer(c.ControlPlaneNamespace).Lister().ConfigMaps(c.ControlPlaneNamespace)),
			).WithHooks(merge.HyperShiftOnly,
				withCustomAWSCABundle("user-ca-bundle", c.GetGuestConfigMapInformer(c.ControlPlaneNamespace).Lister().ConfigMaps(c.ControlPlaneNamespace)),
			),
		},

		GuestConfig: &merge.GuestConfig{
			MetricsPorts:               nil,
			LivenessProbePort:          10301,
			DaemonSetTemplateAssetName: "drivers/aws-ebs/node.yaml",
			StaticAssets: merge.DefaultNodeAssets.WithAssets(merge.AllFlavours,
				"drivers/aws-ebs/csidriver.yaml",
				"drivers/aws-ebs/volumesnapshotclass.yaml",
			),
			StorageClassAssetNames: []string{
				"drivers/aws-ebs/storageclass_gp2.yaml",
				"drivers/aws-ebs/storageclass_gp3.yaml",
			},
		},
	}
	return cfg, nil
}

// withCustomAWSCABundle executes the asset as a template to fill out the parts required when using a custom CA bundle.
// The `caBundleConfigMap` parameter specifies the name of the ConfigMap containing the custom CA bundle. If the
// argument supplied is empty, then no custom CA bundle will be used.
func withCustomAWSCABundle(cmName string, cloudConfigLister corev1listers.ConfigMapNamespaceLister) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
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

func withAWSRegion(infraLister v1.InfrastructureLister) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
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
}
