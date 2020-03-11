package operator

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	v1alpha1 "github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/apis/operator/v1alpha1"
)

var log = logf.Log.WithName("aws_ebs_csi_driver_operator")

// var deploymentVersionHashKey = operatorv1.GroupName + "/rvs-hash"

const (
	targetName        = "aws-ebs-csi-driver"
	targetNamespace   = "openshift-aws-ebs-csi-driver"
	operatorNamespace = "openshift-aws-ebs-csi-driver-operator"
	globalConfigName  = "cluster"

	operatorVersionEnvName = "OPERATOR_IMAGE_VERSION"
	operandVersionEnvName  = "OPERAND_IMAGE_VERSION"
	operandImageEnvName    = "OPERAND_IMAGE"

	maxRetries = 15
)

// static environment variables from operator deployment
// var (
// crdNames = []string{"ebscsidrivers.csi.ebs.aws.com"}
// )

type csiDriverOperator struct {
	client        OperatorClient
	kubeClient    kubernetes.Interface
	versionGetter status.VersionGetter
	eventRecorder events.Recorder

	syncHandler func() error

	queue workqueue.RateLimitingInterface

	stopCh <-chan struct{}

	operatorVersion string
	operandVersion  string
	csiDriverImage  string
}

func NewCSIDriverOperator(
	client OperatorClient,
	deployInformer appsinformersv1.DeploymentInformer,
	kubeClient kubernetes.Interface,
	versionGetter status.VersionGetter,
	eventRecorder events.Recorder,
	operatorVersion string,
	operandVersion string,
	csiDriverImage string,
) *csiDriverOperator {
	csiOperator := &csiDriverOperator{
		client:          client,
		kubeClient:      kubeClient,
		versionGetter:   versionGetter,
		eventRecorder:   eventRecorder,
		queue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "aws-ebs-csi-driver"),
		operatorVersion: operatorVersion,
		operandVersion:  operandVersion,
		csiDriverImage:  csiDriverImage,
	}

	deployInformer.Informer().AddEventHandler(csiOperator.eventHandler("deployment"))
	client.Informer().AddEventHandler(csiOperator.eventHandler("ebscsidriver"))

	csiOperator.syncHandler = csiOperator.sync

	return csiOperator
}

func (c *csiDriverOperator) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.stopCh = stopCh

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}
	<-stopCh
}

func (c *csiDriverOperator) sync() error {
	instance, err := c.client.GetOperatorInstance()
	if err != nil {
		return err
	}

	if instance.Spec.ManagementState != operatorv1.Managed {
		return nil // TODO do something better for all states
	}

	instanceCopy := instance.DeepCopy()

	// Ensure the deployment exists and matches the default
	// If it doesn't exist, create it.
	// If it does exist and doesn't match, overwrite it
	startTime := time.Now()
	klog.Info("Starting syncing operator at ", startTime)
	defer func() {
		klog.Info("Finished syncing operator at ", time.Since(startTime))
	}()

	syncErr := c.handleSync(instanceCopy)
	c.updateSyncError(&instanceCopy.Status.OperatorStatus, syncErr)

	if _, _, err := v1helpers.UpdateStatus(c.client, func(status *operatorv1.OperatorStatus) error {
		// store a copy of our starting conditions, we need to preserve last transition time
		originalConditions := status.DeepCopy().Conditions

		// copy over everything else
		instanceCopy.Status.OperatorStatus.DeepCopyInto(status)

		// restore the starting conditions
		status.Conditions = originalConditions

		// manually update the conditions while preserving last transition time
		for _, condition := range instanceCopy.Status.Conditions {
			v1helpers.SetOperatorCondition(&status.Conditions, condition)
		}
		return nil
	}); err != nil {
		klog.Errorf("failed to update status: %v", err)
		if syncErr == nil {
			syncErr = err
		}
	}

	return syncErr
}

func (c *csiDriverOperator) updateSyncError(status *operatorv1.OperatorStatus, err error) {
	if err != nil {
		v1helpers.SetOperatorCondition(&status.Conditions,
			operatorv1.OperatorCondition{
				Type:    operatorv1.OperatorStatusTypeDegraded,
				Status:  operatorv1.ConditionTrue,
				Reason:  "OperatorSync",
				Message: err.Error(),
			})
	} else {
		v1helpers.SetOperatorCondition(&status.Conditions,
			operatorv1.OperatorCondition{
				Type:   operatorv1.OperatorStatusTypeDegraded,
				Status: operatorv1.ConditionFalse,
			})
	}
}

func (c *csiDriverOperator) handleSync(instance *v1alpha1.EBSCSIDriver) error {
	deployment, err := c.syncDeployment(instance)
	if err != nil {
		return fmt.Errorf("failed to sync Deployments: %s", err)
	}
	if err := c.syncStatus(instance, deployment); err != nil {
		return fmt.Errorf("failed to sync status: %s", err)
	}
	return nil
}

func (c *csiDriverOperator) setVersion(operandName, version string) {
	if c.versionGetter.GetVersions()[operandName] != version {
		c.versionGetter.SetVersion(operandName, version)
	}
}

func (c *csiDriverOperator) versionChanged(operandName, version string) bool {
	return c.versionGetter.GetVersions()[operandName] != version
}

func (c *csiDriverOperator) enqueue(obj interface{}) {
	// we're filtering out config maps that are "leader" based and we don't have logic around them
	// resyncing on these causes the operator to sync every 14s for no good reason
	if cm, ok := obj.(*corev1.ConfigMap); ok && cm.GetAnnotations() != nil && cm.GetAnnotations()[resourcelock.LeaderElectionRecordAnnotationKey] != "" {
		return
	}
	// Sync corresponding EBSCSIDriver instance. Since there is only one, sync that one.
	// It will check all other objects (Deployment, DaemonSet) and update/overwrite them as needed.
	c.queue.Add(globalConfigName)
}

func (c *csiDriverOperator) eventHandler(kind string) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			logInformerEvent(kind, obj, "added")
			c.enqueue(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			logInformerEvent(kind, new, "updated")
			c.enqueue(new)
		},
		DeleteFunc: func(obj interface{}) {
			logInformerEvent(kind, obj, "deleted")
			c.enqueue(obj)
		},
	}
}

func logInformerEvent(kind, obj interface{}, message string) {
	if klog.V(6) {
		objMeta, err := meta.Accessor(obj)
		if err != nil {
			return
		}
		klog.V(6).Infof("Received event: %s %s %s", kind, objMeta.GetName(), message)
	}
}

func (c *csiDriverOperator) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *csiDriverOperator) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler()
	c.handleErr(err, key)

	return true
}

func (c *csiDriverOperator) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		klog.V(2).Infof("Error syncing operator %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	klog.V(2).Infof("Dropping operator %q out of the queue: %v", key, err)
	c.queue.Forget(key)
	c.queue.AddAfter(key, 1*time.Minute)
}
