package csidrivercontrollerservicecontroller

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	appsv1 "k8s.io/api/apps/v1"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	driverImageEnvName        = "DRIVER_IMAGE"
	provisionerImageEnvName   = "PROVISIONER_IMAGE"
	attacherImageEnvName      = "ATTACHER_IMAGE"
	resizerImageEnvName       = "RESIZER_IMAGE"
	snapshotterImageEnvName   = "SNAPSHOTTER_IMAGE"
	livenessProbeImageEnvName = "LIVENESS_PROBE_IMAGE"
)

type CSIDriverControllerServiceController struct {
	name           string
	manifest       []byte
	operatorClient v1helpers.OperatorClient
	kubeClient     kubernetes.Interface
	deployInformer appsinformersv1.DeploymentInformer
}

func NewCSIDriverControllerServiceController(
	name string,
	manifest []byte,
	operatorClient v1helpers.OperatorClient,
	kubeClient kubernetes.Interface,
	deployInformer appsinformersv1.DeploymentInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &CSIDriverControllerServiceController{
		name:           name,
		manifest:       manifest,
		operatorClient: operatorClient,
		kubeClient:     kubeClient,
		deployInformer: deployInformer,
	}

	return factory.New().WithInformers(
		operatorClient.Informer(),
		deployInformer.Informer(),
	).WithSync(
		c.sync,
	).ResyncEvery(
		time.Minute,
	).WithSyncDegradedOnError(
		operatorClient,
	).ToController(
		c.name,
		recorder.WithComponentSuffix("csi-driver-controller-service_"+strings.ToLower(name)),
	)
}

func (c *CSIDriverControllerServiceController) Name() string {
	return c.name
}

func (c *CSIDriverControllerServiceController) sync(ctx context.Context, syncContext factory.SyncContext) error {
	opSpec, opStatus, _, err := c.operatorClient.GetOperatorState()
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if opSpec.ManagementState != opv1.Managed {
		return nil
	}

	manifest := replacePlaceHolders(c.manifest, opSpec)
	required := resourceread.ReadDeploymentV1OrDie(manifest)

	deployment, _, err := resourceapply.ApplyDeployment(
		c.kubeClient.AppsV1(),
		syncContext.Recorder(),
		required,
		resourcemerge.ExpectedDeploymentGeneration(required, opStatus.Generations),
	)
	if err != nil {
		return err
	}

	availableCondition := opv1.OperatorCondition{
		Type:   c.name + opv1.OperatorStatusTypeAvailable,
		Status: opv1.ConditionTrue,
	}

	if deployment.Status.AvailableReplicas > 0 {
		availableCondition.Status = opv1.ConditionTrue
	} else {
		availableCondition.Status = opv1.ConditionFalse
		availableCondition.Message = "Waiting for Deployment to deploy the CSI Controller Service"
		availableCondition.Reason = "AsExpected"
	}

	progressingCondition := opv1.OperatorCondition{
		Type:   c.name + opv1.OperatorStatusTypeProgressing,
		Status: opv1.ConditionFalse,
	}

	if ok, msg := isProgressing(opStatus, deployment); ok {
		progressingCondition.Status = opv1.ConditionTrue
		progressingCondition.Message = msg
		progressingCondition.Reason = "AsExpected"
	}

	updateStatusFn := func(newStatus *opv1.OperatorStatus) error {
		resourcemerge.SetDeploymentGeneration(&newStatus.Generations, deployment)
		return nil
	}

	_, _, err = v1helpers.UpdateStatus(
		c.operatorClient,
		updateStatusFn,
		v1helpers.UpdateConditionFn(availableCondition),
		v1helpers.UpdateConditionFn(progressingCondition),
	)

	return err
}

func isProgressing(status *opv1.OperatorStatus, deployment *appsv1.Deployment) (bool, string) {
	var deploymentExpectedReplicas int32
	if deployment.Spec.Replicas != nil {
		deploymentExpectedReplicas = *deployment.Spec.Replicas
	}

	switch {
	case deployment.Generation != deployment.Status.ObservedGeneration:
		return true, "Waiting for Deployment to act on changes"
	case deployment.Status.UnavailableReplicas > 0:
		return true, "Waiting for Deployment to deploy pods"
	case deployment.Status.UpdatedReplicas < deploymentExpectedReplicas:
		return true, "Waiting for Deployment to update pods"
	case deployment.Status.AvailableReplicas < deploymentExpectedReplicas:
		return true, "Waiting for Deployment to deploy pods"
	}
	return false, ""
}

func replacePlaceHolders(manifest []byte, spec *opv1.OperatorSpec) []byte {
	pairs := []string{}

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

	// Log level
	logLevel := getLogLevel(spec.LogLevel)
	pairs = append(pairs, []string{"${LOG_LEVEL}", strconv.Itoa(logLevel)}...)

	replaced := strings.NewReplacer(pairs...).Replace(string(manifest))
	return []byte(replaced)
}

func getLogLevel(logLevel opv1.LogLevel) int {
	switch logLevel {
	case opv1.Normal, "":
		return 2
	case opv1.Debug:
		return 4
	case opv1.Trace:
		return 6
	case opv1.TraceAll:
		return 100
	default:
		return 2
	}
}
