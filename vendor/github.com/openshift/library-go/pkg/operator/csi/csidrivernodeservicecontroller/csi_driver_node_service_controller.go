package csidrivernodeservicecontroller

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

const (
	driverImageEnvName              = "DRIVER_IMAGE"
	nodeDriverRegistrarImageEnvName = "NODE_DRIVER_REGISTRAR_IMAGE"
	livenessProbeImageEnvName       = "LIVENESS_PROBE_IMAGE"
)

type CSIDriverNodeServiceController struct {
	name           string
	manifest       []byte
	operatorClient v1helpers.OperatorClient
	kubeClient     kubernetes.Interface
	dsInformer     appsinformersv1.DaemonSetInformer
}

func NewCSIDriverNodeServiceController(
	name string,
	manifest []byte,
	operatorClient v1helpers.OperatorClient,
	kubeClient kubernetes.Interface,
	dsInformer appsinformersv1.DaemonSetInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &CSIDriverNodeServiceController{
		name:           name,
		manifest:       manifest,
		operatorClient: operatorClient,
		kubeClient:     kubeClient,
		dsInformer:     dsInformer,
	}

	return factory.New().WithInformers(
		operatorClient.Informer(),
		dsInformer.Informer(),
	).WithSync(
		c.sync,
	).ResyncEvery(
		time.Minute,
	).WithSyncDegradedOnError(
		operatorClient,
	).ToController(
		c.name,
		recorder.WithComponentSuffix("csi-driver-node-service_"+strings.ToLower(name)),
	)
}

func (c *CSIDriverNodeServiceController) Name() string {
	return c.name
}

func (c *CSIDriverNodeServiceController) sync(ctx context.Context, syncContext factory.SyncContext) error {
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
	required := resourceread.ReadDaemonSetV1OrDie(manifest)

	daemonSet, _, err := resourceapply.ApplyDaemonSet(
		c.kubeClient.AppsV1(),
		syncContext.Recorder(),
		required,
		resourcemerge.ExpectedDaemonSetGeneration(required, opStatus.Generations),
	)
	if err != nil {
		return err
	}

	availableCondition := opv1.OperatorCondition{
		Type:   c.name + opv1.OperatorStatusTypeAvailable,
		Status: opv1.ConditionTrue,
	}

	if daemonSet.Status.NumberAvailable > 0 {
		availableCondition.Status = opv1.ConditionTrue
	} else {
		availableCondition.Status = opv1.ConditionFalse
		availableCondition.Message = "Waiting for the DaemonSet to deploy the CSI Node Service"
		availableCondition.Reason = "AsExpected"
	}

	progressingCondition := opv1.OperatorCondition{
		Type:   c.name + opv1.OperatorStatusTypeProgressing,
		Status: opv1.ConditionFalse,
	}

	if ok, msg := isProgressing(opStatus, daemonSet); ok {
		progressingCondition.Status = opv1.ConditionTrue
		progressingCondition.Message = msg
		progressingCondition.Reason = "AsExpected"
	}

	updateStatusFn := func(newStatus *opv1.OperatorStatus) error {
		// TODO: should I update ObservedGeneration?
		// TODO: what about readyreplicas?
		resourcemerge.SetDaemonSetGeneration(&newStatus.Generations, daemonSet)
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

func isProgressing(status *opv1.OperatorStatus, daemonSet *appsv1.DaemonSet) (bool, string) {
	switch {
	case daemonSet.Generation != daemonSet.Status.ObservedGeneration:
		return true, "Waiting for DaemonSet to act on changes"
	case daemonSet.Status.NumberUnavailable > 0:
		return true, "Waiting for DaemonSet to deploy node pods"
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

	nodeDriverRegistrar := os.Getenv(nodeDriverRegistrarImageEnvName)
	if nodeDriverRegistrar != "" {
		pairs = append(pairs, []string{"${NODE_DRIVER_REGISTRAR_IMAGE}", nodeDriverRegistrar}...)

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
