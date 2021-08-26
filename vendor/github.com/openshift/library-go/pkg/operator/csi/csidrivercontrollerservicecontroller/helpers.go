package csidrivercontrollerservicecontroller

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	corev1 "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"

	configv1 "github.com/openshift/api/config/v1"
	opv1 "github.com/openshift/api/operator/v1"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/csi/csiconfigobservercontroller"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/loglevel"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehash"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

const (
	driverImageEnvName        = "DRIVER_IMAGE"
	provisionerImageEnvName   = "PROVISIONER_IMAGE"
	attacherImageEnvName      = "ATTACHER_IMAGE"
	resizerImageEnvName       = "RESIZER_IMAGE"
	snapshotterImageEnvName   = "SNAPSHOTTER_IMAGE"
	livenessProbeImageEnvName = "LIVENESS_PROBE_IMAGE"
	kubeRBACProxyImageEnvName = "KUBE_RBAC_PROXY_IMAGE"

	infraConfigName = "cluster"
)

// WithObservedProxyDeploymentHook creates a deployment hook that injects into the deployment's containers the observed proxy config.
func WithObservedProxyDeploymentHook() dc.DeploymentHookFunc {
	return func(opSpec *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		containerNamesString := deployment.Annotations["config.openshift.io/inject-proxy"]
		err := v1helpers.InjectObservedProxyIntoContainers(
			&deployment.Spec.Template.Spec,
			strings.Split(containerNamesString, ","),
			opSpec.ObservedConfig.Raw,
			csiconfigobservercontroller.ProxyConfigPath()...,
		)
		return err
	}
}

// WithSecretHashAnnotationHook creates a deployment hook that annotates a Deployment with a secret's hash.
func WithSecretHashAnnotationHook(
	namespace string,
	secretName string,
	secretInformer corev1.SecretInformer,
) dc.DeploymentHookFunc {
	return func(opSpec *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		inputHashes, err := resourcehash.MultipleObjectHashStringMapForObjectReferenceFromLister(
			nil,
			secretInformer.Lister(),
			resourcehash.NewObjectRef().ForSecret().InNamespace(namespace).Named(secretName),
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
			if len(annotationKey) > 63 {
				hash := sha256.Sum256([]byte(k))
				annotationKey = fmt.Sprintf("operator.openshift.io/dep-%x", hash)
				annotationKey = annotationKey[:63]
			}
			deployment.Annotations[annotationKey] = v
			deployment.Spec.Template.Annotations[annotationKey] = v
		}
		return nil
	}
}

// WithReplicasHook sets the deployment.Spec.Replicas field according to the number
// of available nodes. If the number of available nodes is bigger than one, then the
// number of replicas will be two. The number of nodes is determined by the node
// selector specified in the field deployment.Spec.Templates.NodeSelector.
// When node ports or hostNetwork are used, maxSurge=0 should be set in the
// Deployment RollingUpdate strategy to prevent the new pod from getting stuck
// waiting for a node with free ports.
func WithReplicasHook(nodeLister corev1listers.NodeLister) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		nodeSelector := deployment.Spec.Template.Spec.NodeSelector
		nodes, err := nodeLister.List(labels.SelectorFromSet(nodeSelector))
		if err != nil {
			return err
		}
		replicas := int32(1)
		if len(nodes) > 1 {
			replicas = int32(2)
		}
		deployment.Spec.Replicas = &replicas
		return nil
	}
}

// WithPlaceholdersHook is a manifest hook which replaces the variable with appropriate values set
func WithPlaceholdersHook(configInformer configinformers.SharedInformerFactory) dc.ManifestHookFunc {
	return func(spec *opv1.OperatorSpec, manifest []byte) ([]byte, error) {
		pairs := []string{}
		infra, err := configInformer.Config().V1().Infrastructures().Lister().Get(infraConfigName)
		if err != nil {
			return nil, err
		}
		clusterID := infra.Status.InfrastructureName
		// Replace container images by env vars if they are set
		csiDriver := os.Getenv(driverImageEnvName)
		if csiDriver != "" {
			pairs = append(pairs, []string{"${DRIVER_IMAGE}", csiDriver}...)
		}

		provisioner := os.Getenv(provisionerImageEnvName)
		if provisioner != "" {
			pairs = append(pairs, []string{"${PROVISIONER_IMAGE}", provisioner}...)
		}

		attacher := os.Getenv(attacherImageEnvName)
		if attacher != "" {
			pairs = append(pairs, []string{"${ATTACHER_IMAGE}", attacher}...)
		}

		resizer := os.Getenv(resizerImageEnvName)
		if resizer != "" {
			pairs = append(pairs, []string{"${RESIZER_IMAGE}", resizer}...)
		}

		snapshotter := os.Getenv(snapshotterImageEnvName)
		if snapshotter != "" {
			pairs = append(pairs, []string{"${SNAPSHOTTER_IMAGE}", snapshotter}...)
		}

		livenessProbe := os.Getenv(livenessProbeImageEnvName)
		if livenessProbe != "" {
			pairs = append(pairs, []string{"${LIVENESS_PROBE_IMAGE}", livenessProbe}...)
		}

		kubeRBACProxy := os.Getenv(kubeRBACProxyImageEnvName)
		if kubeRBACProxy != "" {
			pairs = append(pairs, []string{"${KUBE_RBAC_PROXY_IMAGE}", kubeRBACProxy}...)
		}

		// Cluster ID
		pairs = append(pairs, []string{"${CLUSTER_ID}", clusterID}...)

		// Log level
		logLevel := loglevel.LogLevelToVerbosity(spec.LogLevel)
		pairs = append(pairs, []string{"${LOG_LEVEL}", strconv.Itoa(logLevel)}...)

		replaced := strings.NewReplacer(pairs...).Replace(string(manifest))
		return []byte(replaced), nil
	}
}

// WithControlPlaneTopologyHook modifies the nodeSelector of the deployment
// based on the control plane topology reported in Infrastructure.Status.ControlPlaneTopology.
// If running with an External control plane, the nodeSelector should not include
// master nodes.
func WithControlPlaneTopologyHook(configInformer configinformers.SharedInformerFactory) dc.DeploymentHookFunc {
	return func(_ *opv1.OperatorSpec, deployment *appsv1.Deployment) error {
		infra, err := configInformer.Config().V1().Infrastructures().Lister().Get(infraConfigName)
		if err != nil {
			return err
		}
		if infra.Status.ControlPlaneTopology == configv1.ExternalTopologyMode {
			deployment.Spec.Template.Spec.NodeSelector = map[string]string{}
		}
		return nil
	}
}
