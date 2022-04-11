package csidefaultstorageclasscontroller

import (
	"context"
	operatorapi "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/storage/v1"
	"k8s.io/klog/v2"
	"time"
)

const (
	conditionsPrefix       = "CSIDefaultStorageClassController"
	defaultScAnnotationKey = "storageclass.kubernetes.io/is-default-class"
)

// This Controller deploys a StorageClass provided by CSI driver operator
// and decides if this StorageClass should be applied as default - if requested.
// If operator wants to request it's StorageClass to be created as default,
// the asset file provided to this controller must have defaultScAnnotationKey set to "true".
// Based on the current StorageClasses in the cluster the controller can decide not
// to deploy given StorageClass as default if there is already an existing default.
// Also, the controller will emit alerts if more than one StorageClass defined
// as default exists - this is not a recommended setup.
// It produces following Conditions:
// DefaultStorageClassControllerDegraded - failed to apply StorageClass provided
type CSIDefaultStorageClassController struct {
	name               string
	assetFunc          resourceapply.AssetFunc
	file               string
	kubeClient         kubernetes.Interface
	storageClassLister v1.StorageClassLister
	operatorClient     v1helpers.OperatorClient
	eventRecorder      events.Recorder
}

func NewCSIDefaultStorageClassController(
	name string,
	assetFunc resourceapply.AssetFunc,
	file string,
	kubeClient kubernetes.Interface,
	InformerFactory informers.SharedInformerFactory,
	operatorClient v1helpers.OperatorClient,
	eventRecorder events.Recorder) factory.Controller {
	c := &CSIDefaultStorageClassController{
		name:               name,
		assetFunc:          assetFunc,
		file:               file,
		kubeClient:         kubeClient,
		storageClassLister: InformerFactory.Storage().V1().StorageClasses().Lister(),
		operatorClient:     operatorClient,
		eventRecorder:      eventRecorder,
	}

	return factory.New().WithSync(
		c.Sync,
	).ResyncEvery(
		time.Minute,
	).WithSyncDegradedOnError(
		operatorClient,
	).WithInformers(
		operatorClient.Informer(),
		InformerFactory.Storage().V1().StorageClasses().Informer(),
	).ToController(
		"DefaultStorageClassController",
		eventRecorder,
	)
}

func (c *CSIDefaultStorageClassController) Sync(ctx context.Context, syncCtx factory.SyncContext) error {
	klog.V(4).Infof("DefaultStorageClassController sync started")
	defer klog.V(4).Infof("DefaultStorageClassController sync finished")

	opSpec, _, _, err := c.operatorClient.GetOperatorState()
	if err != nil {
		return err
	}
	if opSpec.ManagementState != operatorapi.Managed {
		return nil
	}

	syncErr := c.syncStorageClass(ctx)

	return syncErr
}

func (c *CSIDefaultStorageClassController) syncStorageClass(ctx context.Context) error {
	expectedScBytes, err := c.assetFunc(c.file)
	if err != nil {
		return err
	}

	expectedSC := resourceread.ReadStorageClassV1OrDie(expectedScBytes)

	existingSCs, err := c.storageClassLister.List(labels.Everything())
	if err != nil {
		klog.V(2).Infof("could not list StorageClass objects")
		return err
	}

	defaultScCount := 0
	for _, sc := range existingSCs {
		if sc.Annotations[defaultScAnnotationKey] == "true" && sc.Name != expectedSC.Name {
			defaultScCount++
		}
	}

	if defaultScCount > 0 {
		klog.V(2).Infof("default StorageClass already defined in cluster, %v StorageClass will be applied as non-default", expectedSC.Name)
		expectedSC.Annotations["storageclass.kubernetes.io/is-default-class"] = "false"
	}

	_, _, err = resourceapply.ApplyStorageClass(ctx, c.kubeClient.StorageV1(), c.eventRecorder, expectedSC)

	return err
}

// UpdateConditionFunc returns a func to update a condition.
func removeConditionFn(condType string) v1helpers.UpdateStatusFunc {
	return func(oldStatus *operatorapi.OperatorStatus) error {
		v1helpers.RemoveOperatorCondition(&oldStatus.Conditions, condType)
		return nil
	}
}
