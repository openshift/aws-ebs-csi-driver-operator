package operator

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"
	coreinformersv1 "k8s.io/client-go/informers/core/v1"
	rbacinformersv1 "k8s.io/client-go/informers/rbac/v1"
	storageinformersv1 "k8s.io/client-go/informers/storage/v1"
	storageinformersv1beta1 "k8s.io/client-go/informers/storage/v1beta1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	v1alpha1 "github.com/openshift/aws-ebs-csi-driver-operator/pkg/apis/operator/v1alpha1"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated"
)

var log = logf.Log.WithName("aws_ebs_csi_driver_operator")

const (
	operandName      = "aws-ebs-csi-driver"
	operandNamespace = "openshift-aws-ebs-csi-driver"

	operatorFinalizer = "csi.storage.operator.openshift.io"
	operatorNamespace = "openshift-aws-ebs-csi-driver-operator"

	operatorVersionEnvName = "OPERATOR_IMAGE_VERSION"
	operandVersionEnvName  = "OPERAND_IMAGE_VERSION"

	driverImageEnvName              = "DRIVER_IMAGE"
	provisionerImageEnvName         = "PROVISIONER_IMAGE"
	attacherImageEnvName            = "ATTACHER_IMAGE"
	resizerImageEnvName             = "RESIZER_IMAGE"
	snapshotterImageEnvName         = "SNAPSHOTTER_IMAGE"
	nodeDriverRegistrarImageEnvName = "NODE_DRIVER_REGISTRAR_IMAGE"
	livenessProbeImageEnvName       = "LIVENESS_PROBE_IMAGE"

	// Index of a container in assets/controller_deployment.yaml and assets/node_daemonset.yaml
	csiDriverContainerIndex           = 0 // Both Deployment and DaemonSet
	provisionerContainerIndex         = 1
	attacherContainerIndex            = 2
	resizerContainerIndex             = 3
	snapshottterContainerIndex        = 4
	nodeDriverRegistrarContainerIndex = 1
	livenessProbeContainerIndex       = 2 // Only in DaemonSet

	globalConfigName = "cluster"

	maxRetries = 15
)

type csiDriverOperator struct {
	client          OperatorClient
	kubeClient      kubernetes.Interface
	dynamicClient   dynamic.Interface
	pvInformer      coreinformersv1.PersistentVolumeInformer
	secretInformer  coreinformersv1.SecretInformer
	versionGetter   status.VersionGetter
	eventRecorder   events.Recorder
	informersSynced []cache.InformerSynced

	syncHandler func() error

	queue workqueue.RateLimitingInterface

	stopCh <-chan struct{}

	operatorVersion string
	operandVersion  string
	images          images
}

type images struct {
	csiDriver           string
	attacher            string
	provisioner         string
	resizer             string
	snapshotter         string
	nodeDriverRegistrar string
	livenessProbe       string
}

func NewCSIDriverOperator(
	client OperatorClient,
	pvInformer coreinformersv1.PersistentVolumeInformer,
	namespaceInformer coreinformersv1.NamespaceInformer,
	csiDriverInformer storageinformersv1beta1.CSIDriverInformer,
	serviceAccountInformer coreinformersv1.ServiceAccountInformer,
	clusterRoleInformer rbacinformersv1.ClusterRoleInformer,
	clusterRoleBindingInformer rbacinformersv1.ClusterRoleBindingInformer,
	deployInformer appsinformersv1.DeploymentInformer,
	dsInformer appsinformersv1.DaemonSetInformer,
	storageClassInformer storageinformersv1.StorageClassInformer,
	secretInformer coreinformersv1.SecretInformer,
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	versionGetter status.VersionGetter,
	eventRecorder events.Recorder,
	operatorVersion string,
	operandVersion string,
	images images,
) *csiDriverOperator {
	csiOperator := &csiDriverOperator{
		client:          client,
		kubeClient:      kubeClient,
		dynamicClient:   dynamicClient,
		pvInformer:      pvInformer,
		secretInformer:  secretInformer,
		versionGetter:   versionGetter,
		eventRecorder:   eventRecorder,
		queue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "aws-ebs-csi-driver"),
		operatorVersion: operatorVersion,
		operandVersion:  operandVersion,
		images:          images,
	}

	csiOperator.informersSynced = append(csiOperator.informersSynced, pvInformer.Informer().HasSynced)

	namespaceInformer.Informer().AddEventHandler(csiOperator.eventHandler("namespace"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, namespaceInformer.Informer().HasSynced)

	csiDriverInformer.Informer().AddEventHandler(csiOperator.eventHandler("csidriver"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, csiDriverInformer.Informer().HasSynced)

	serviceAccountInformer.Informer().AddEventHandler(csiOperator.eventHandler("serviceaccount"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, csiDriverInformer.Informer().HasSynced)

	clusterRoleInformer.Informer().AddEventHandler(csiOperator.eventHandler("clusterrole"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, clusterRoleInformer.Informer().HasSynced)

	clusterRoleBindingInformer.Informer().AddEventHandler(csiOperator.eventHandler("clusterrolebinding"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, clusterRoleBindingInformer.Informer().HasSynced)

	deployInformer.Informer().AddEventHandler(csiOperator.eventHandler("deployment"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, deployInformer.Informer().HasSynced)

	dsInformer.Informer().AddEventHandler(csiOperator.eventHandler("daemonset"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, dsInformer.Informer().HasSynced)

	storageClassInformer.Informer().AddEventHandler(csiOperator.eventHandler("storageclass"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, storageClassInformer.Informer().HasSynced)

	client.Informer().AddEventHandler(csiOperator.eventHandler("ebscsidriver"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, client.Informer().HasSynced)

	secretInformer.Informer().AddEventHandler(csiOperator.eventHandler("secret"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, secretInformer.Informer().HasSynced)

	csiOperator.syncHandler = csiOperator.sync

	return csiOperator
}

func (c *csiDriverOperator) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.stopCh = stopCh

	if !cache.WaitForCacheSync(stopCh, c.informersSynced...) {
		return
	}
	klog.Infof("Caches synced, running the controller")

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}
	<-stopCh
}

func (c *csiDriverOperator) sync() error {
	instance, err := c.client.GetOperatorInstance()
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Warningf("Operator instance not found: %v", err)
			return c.deleteAll()
		}
		return err
	}

	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		// CR is NOT being deleted, so make sure it has the finalizer
		if addFinalizer(instance, operatorFinalizer) {
			c.client.UpdateFinalizers(instance)
		}
	} else {
		// User tried to delete the CR, let's evaluate if we can
		// delete the operand and remove the finalizer from the CR
		ok, err := c.isCSIDriverInUse()
		if err != nil {
			return err
		}

		if ok {
			// CSI driver is being used but CR has been marked for deletion,
			// so we can't delete or sync the operand nor remove the finalizer
			klog.Warningf("CR is being deleted but there are resources using the CSI driver")
			return nil
		} else {
			// CSI driver is not being used, we can go ahead and remove finalizer and delete the operand
			removeFinalizer(instance, operatorFinalizer)
			c.client.UpdateFinalizers(instance)
			return c.deleteAll()
		}
	}

	// We only support Managed for now
	if instance.Spec.ManagementState != operatorv1.Managed {
		return nil
	}

	instanceCopy := instance.DeepCopy()

	startTime := time.Now()
	klog.Info("Starting syncing operator at ", startTime)
	defer func() {
		klog.Info("Finished syncing operator at ", time.Since(startTime))
	}()

	syncErr := c.handleSync(instanceCopy)
	if syncErr != nil {
		c.eventRecorder.Eventf("SyncError", "Error syncing CSI driver: %s", syncErr)
	}
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
		// Operator is Degraded: could not finish what it was doing
		v1helpers.SetOperatorCondition(&status.Conditions,
			operatorv1.OperatorCondition{
				Type:    operatorv1.OperatorStatusTypeDegraded,
				Status:  operatorv1.ConditionTrue,
				Reason:  "OperatorSync",
				Message: err.Error(),
			})
		// Operator is Progressing: some action failed, will try to progress more after exp. backoff.
		// Do not overwrite existing "Progressing=true" condition to keep its message.
		cnd := v1helpers.FindOperatorCondition(status.Conditions, operatorv1.OperatorStatusTypeProgressing)
		if cnd == nil || cnd.Status == operatorv1.ConditionFalse {
			v1helpers.SetOperatorCondition(&status.Conditions,
				operatorv1.OperatorCondition{
					Type:    operatorv1.OperatorStatusTypeProgressing,
					Status:  operatorv1.ConditionTrue,
					Reason:  "OperatorSync",
					Message: err.Error(),
				})
		}
	} else {
		v1helpers.SetOperatorCondition(&status.Conditions,
			operatorv1.OperatorCondition{
				Type:   operatorv1.OperatorStatusTypeDegraded,
				Status: operatorv1.ConditionFalse,
			})
		// Progressing condition was set in c.handleSync().
	}
}

func (c *csiDriverOperator) handleSync(instance *v1alpha1.Driver) error {
	err := c.syncCSIDriver(instance)
	if err != nil {
		return fmt.Errorf("failed to sync CSIDriver: %v", err)
	}

	err = c.syncNamespace(instance)
	if err != nil {
		return fmt.Errorf("failed to sync namespace: %v", err)
	}

	credentialsRequest, err := c.syncCredentialsRequest(instance)
	if err != nil {
		return fmt.Errorf("failed to sync CredentialsRequest: %v", err)
	}

	err = c.tryCredentialsSecret(instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Have a nice event instead of "secret XYZ not found"
			return fmt.Errorf("waiting for cloud credentials secret provided by cloud-credential-operator")
		}
		return fmt.Errorf("error waiting for cloud credentials:: %v", err)
	}

	err = c.syncServiceAccounts(instance)
	if err != nil {
		return fmt.Errorf("failed to sync ServiceAccount: %v", err)
	}

	err = c.syncRBAC(instance)
	if err != nil {
		return fmt.Errorf("failed to sync RBAC: %v", err)
	}

	deployment, err := c.syncDeployment(instance)
	if err != nil {
		return fmt.Errorf("failed to sync Deployment: %v", err)
	}

	daemonSet, err := c.syncDaemonSet(instance)
	if err != nil {
		return fmt.Errorf("failed to sync DaemonSet: %v", err)
	}

	err = c.syncStorageClass(instance)
	if err != nil {
		return fmt.Errorf("failed to sync StorageClass: %v", err)
	}

	if err := c.syncStatus(instance, deployment, daemonSet, credentialsRequest); err != nil {
		return fmt.Errorf("failed to sync status: %v", err)
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
	// Sync corresponding Driver instance. Since there is only one, sync that one.
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

func (c *csiDriverOperator) isCSIDriverInUse() (bool, error) {
	pvcs, err := c.pvInformer.Lister().List(labels.Everything())
	if err != nil {
		return false, fmt.Errorf("could not get list of pvs: %v", err)
	}

	csiDriver := resourceread.ReadCSIDriverV1Beta1OrDie(generated.MustAsset(csiDriver))

	for i := range pvcs {
		if pvcs[i].Spec.CSI != nil {
			if pvcs[i].Spec.CSI.Driver == csiDriver.Name {
				return true, nil
			}
		}
	}

	return false, nil
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

func addFinalizer(instance *v1alpha1.Driver, f string) bool {
	for _, item := range instance.ObjectMeta.Finalizers {
		if item == f {
			return false
		}
	}
	instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, f)
	return true
}

func removeFinalizer(instance *v1alpha1.Driver, f string) {
	var result []string
	for _, item := range instance.ObjectMeta.Finalizers {
		if item == f {
			continue
		}
		result = append(result, item)
	}
	instance.ObjectMeta.Finalizers = result
}
