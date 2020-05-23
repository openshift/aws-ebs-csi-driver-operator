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

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	v1alpha1 "github.com/openshift/aws-ebs-csi-driver-operator/pkg/apis/operator/v1alpha1"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated"
)

var log = logf.Log.WithName("aws_ebs_csi_driver_operator")

const (
	operandName      = "aws-ebs-csi-driver"
	operandNamespace = "openshift-aws-ebs-csi-driver"

	operatorFinalizer = "operator.csi.openshift.io"
	operatorNamespace = "openshift-aws-ebs-csi-driver-operator"

	driverImageEnvName              = "RELATED_IMAGE_DRIVER"
	provisionerImageEnvName         = "RELATED_IMAGE_PROVISIONER"
	attacherImageEnvName            = "RELATED_IMAGE_ATTACHER"
	resizerImageEnvName             = "RELATED_IMAGE_RESIZER"
	snapshotterImageEnvName         = "RELATED_IMAGE_SNAPSHOTTER"
	nodeDriverRegistrarImageEnvName = "RELATED_IMAGE_NODE_DRIVER_REGISTRAR"
	livenessProbeImageEnvName       = "RELATED_IMAGE_LIVENESS_PROBE"

	// Index of a container in assets/controller_deployment.yaml and assets/node_daemonset.yaml
	csiDriverContainerIndex           = 0 // Both Deployment and DaemonSet
	provisionerContainerIndex         = 1
	attacherContainerIndex            = 2
	resizerContainerIndex             = 3
	snapshottterContainerIndex        = 4
	nodeDriverRegistrarContainerIndex = 1
	livenessProbeContainerIndex       = 2 // Only in DaemonSet

	// Reasons for condition types
	reasonUnsupportedPlatform  = "UnsupportedPlatform"
	reasonOtherDriverInstalled = "OtherDriverInstalled"

	infraConfigName     = "cluster"
	globalConfigName    = "cluster"
	managedByAnnotation = "csi.openshift.io/managed"

	maxRetries = 15
)

type csiDriverOperator struct {
	client             OperatorClient
	kubeClient         kubernetes.Interface
	dynamicClient      dynamic.Interface
	infraInformer      configinformersv1.InfrastructureInformer
	pvInformer         coreinformersv1.PersistentVolumeInformer
	secretInformer     coreinformersv1.SecretInformer
	deploymentInformer appsinformersv1.DeploymentInformer
	dsSetInformer      appsinformersv1.DaemonSetInformer
	csiDriverInformer  storageinformersv1beta1.CSIDriverInformer
	csiNodeInformer    storageinformersv1beta1.CSINodeInformer
	namespaceInformer  coreinformersv1.NamespaceInformer
	eventRecorder      events.Recorder
	informersSynced    []cache.InformerSynced

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
	kubeClient kubernetes.Interface,
	dynamicClient dynamic.Interface,
	infraInformer configinformersv1.InfrastructureInformer,
	pvInformer coreinformersv1.PersistentVolumeInformer,
	namespaceInformer coreinformersv1.NamespaceInformer,
	csiDriverInformer storageinformersv1beta1.CSIDriverInformer,
	csiNodeInformer storageinformersv1beta1.CSINodeInformer,
	serviceAccountInformer coreinformersv1.ServiceAccountInformer,
	clusterRoleInformer rbacinformersv1.ClusterRoleInformer,
	clusterRoleBindingInformer rbacinformersv1.ClusterRoleBindingInformer,
	deployInformer appsinformersv1.DeploymentInformer,
	dsInformer appsinformersv1.DaemonSetInformer,
	storageClassInformer storageinformersv1.StorageClassInformer,
	secretInformer coreinformersv1.SecretInformer,
	eventRecorder events.Recorder,
	images images,
) *csiDriverOperator {
	csiOperator := &csiDriverOperator{
		client:             client,
		kubeClient:         kubeClient,
		dynamicClient:      dynamicClient,
		infraInformer:      infraInformer,
		pvInformer:         pvInformer,
		secretInformer:     secretInformer,
		deploymentInformer: deployInformer,
		dsSetInformer:      dsInformer,
		namespaceInformer:  namespaceInformer,
		csiDriverInformer:  csiDriverInformer,
		csiNodeInformer:    csiNodeInformer,
		eventRecorder:      eventRecorder,
		queue:              workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "aws-ebs-csi-driver"),
		images:             images,
	}

	csiOperator.informersSynced = append(csiOperator.informersSynced, infraInformer.Informer().HasSynced)
	csiOperator.informersSynced = append(csiOperator.informersSynced, pvInformer.Informer().HasSynced)

	namespaceInformer.Informer().AddEventHandler(csiOperator.eventHandler("namespace"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, namespaceInformer.Informer().HasSynced)

	csiDriverInformer.Informer().AddEventHandler(csiOperator.eventHandler("csidriver"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, csiDriverInformer.Informer().HasSynced)

	csiNodeInformer.Informer().AddEventHandler(csiOperator.eventHandler("csinode"))
	csiOperator.informersSynced = append(csiOperator.informersSynced, csiNodeInformer.Informer().HasSynced)

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

	client.Informer().AddEventHandler(csiOperator.eventHandler("driver"))
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
			return nil
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
		return
	}

	// No error
	v1helpers.SetOperatorCondition(&status.Conditions,
		operatorv1.OperatorCondition{
			Type:   operatorv1.OperatorStatusTypeDegraded,
			Status: operatorv1.ConditionFalse,
		})
	// Progressing condition was set in c.handleSync().
}

func (c *csiDriverOperator) handleSync(instance *v1alpha1.AWSEBSDriver) error {
	err := c.checkPrereq(instance)
	if err != nil {
		return err
	}

	c.setCondition(instance, operatorv1.OperatorStatusTypePrereqsSatisfied, true, "", "")
	// Progressing: true until proven otherwise in syncStatus() below.
	c.setCondition(instance, operatorv1.OperatorStatusTypeProgressing, true, "", "")

	err = c.syncCSIDriver(instance)
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
			// Get the error message from credentialsRequest first
			if msg := getCredentialsRequestFailure(credentialsRequest); msg != "" {
				return fmt.Errorf("failed to obtain cloud credentials: %s", msg)
			}
			// Fall back to a generic message. Use something better than "secret XYZ not found".
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

func (c *csiDriverOperator) enqueue(obj interface{}) {
	// we're filtering out config maps that are "leader" based and we don't have logic around them
	// resyncing on these causes the operator to sync every 14s for no good reason
	if cm, ok := obj.(*corev1.ConfigMap); ok && cm.GetAnnotations() != nil && cm.GetAnnotations()[resourcelock.LeaderElectionRecordAnnotationKey] != "" {
		return
	}
	// Sync corresponding AWSEBSDriver instance. Since there is only one, sync that one.
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

func (c *csiDriverOperator) checkPrereq(instance *v1alpha1.AWSEBSDriver) error {
	if err := c.checkInfra(); err != nil {
		msg := "AWS EBS CSI driver requires an AWS cluster."
		c.setCondition(instance, operatorv1.OperatorStatusTypeProgressing, false, reasonUnsupportedPlatform, msg)
		c.setCondition(instance, operatorv1.OperatorStatusTypePrereqsSatisfied, false, reasonUnsupportedPlatform, msg)
		return err
	}

	if err := c.checkPreviousInstall(instance); err != nil {
		msg := "AWS EBS CSI driver is already installed on the cluster."
		c.setCondition(instance, operatorv1.OperatorStatusTypePrereqsSatisfied, false, reasonOtherDriverInstalled, msg)
		c.setCondition(instance, operatorv1.OperatorStatusTypeProgressing, false, reasonOtherDriverInstalled, msg)
		return err
	}

	return nil
}

func (c *csiDriverOperator) checkInfra() error {
	infra, err := c.infraInformer.Lister().Get(infraConfigName)
	if err != nil {
		return err
	}

	if infra.Status.Platform != configv1.AWSPlatformType {
		return fmt.Errorf("platform %q is not supported by the AWS EBS CSI driver", infra.Status.Platform)
	}

	return nil
}

func (c *csiDriverOperator) checkPreviousInstall(instance *v1alpha1.AWSEBSDriver) error {
	// Check that there is no other CSI driver installation.
	// Get the driver name
	expectedDriver := resourceread.ReadCSIDriverV1Beta1OrDie(generated.MustAsset(csiDriver))
	driverName := expectedDriver.Name
	if err := c.checkPreviousCSIDriver(driverName); err != nil {
		return err
	}

	// CSIDriver does not exist. Check if the operand namespace exists.
	_, err := c.namespaceInformer.Lister().Get(operandNamespace)
	if err == nil {
		klog.V(4).Infof("Namespace %s exists", operandNamespace)

		// OCP operand namespace exists. If there is a CSI driver installed, it must be the OCP one.
		return nil
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("error getting Namespace %s: %s", operandNamespace, err)
	}
	klog.V(4).Infof("Namespace %s does not exist", operandNamespace)

	// OCP operand namespace does not exist. Check if the driver is installed by inspecting all CSINodes
	// (that's why we saved this loop to the last possible moment).
	csiNodes, err := c.csiNodeInformer.Lister().List(labels.Everything())
	if err != nil {
		return fmt.Errorf("error listing CSINodes: %s", err)
	}

	for _, csiNode := range csiNodes {
		for i := range csiNode.Spec.Drivers {
			if csiNode.Spec.Drivers[i].Name == driverName {
				klog.V(4).Infof("Node %s has CSI driver %s installed", csiNode.Name, driverName)
				return fmt.Errorf("CSI driver %q is already installed, please uninstall it first before using this operator", driverName)
			}
		}
	}
	// No CSINode has the driver, the driver is not installed.
	klog.V(4).Infof("No CSINode the CSI driver %s installed", driverName)
	return nil
}

func (c *csiDriverOperator) checkPreviousCSIDriver(driverName string) error {
	csiDriver, err := c.csiDriverInformer.Lister().Get(driverName)
	if err != nil {
		if errors.IsNotFound(err) {
			// CSIDriver is missing, that's OK
			klog.V(4).Infof("CSIDriver %s does not exist", driverName)
			return nil
		}
		return fmt.Errorf("error getting CSIDriver: %s", err)
	}

	if csiDriver.Annotations != nil {
		if _, found := csiDriver.Annotations[managedByAnnotation]; found {
			// CSIDriver exists && has OCP annotation: continue managing the driver
			klog.V(4).Infof("CSIDriver %s exist and has annotation %s", driverName, managedByAnnotation)
			return nil
		}
	}
	// CSIDriver exists && does not have OCP annotation: refuse to manage this driver.
	klog.V(4).Infof("CSIDriver %s exist, but has no annotation %s", driverName, managedByAnnotation)
	return fmt.Errorf("CSIDriver %q is already installed, please uninstall it first before using this operator", driverName)
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

func addFinalizer(instance *v1alpha1.AWSEBSDriver, f string) bool {
	for _, item := range instance.ObjectMeta.Finalizers {
		if item == f {
			return false
		}
	}
	instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, f)
	return true
}

func removeFinalizer(instance *v1alpha1.AWSEBSDriver, f string) {
	var result []string
	for _, item := range instance.ObjectMeta.Finalizers {
		if item == f {
			continue
		}
		result = append(result, item)
	}
	instance.ObjectMeta.Finalizers = result
}
