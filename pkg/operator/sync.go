package operator

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/apis/operator/v1alpha1"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated"
)

var (
	csiDriver       = "csidriver.yaml"
	namespace       = "namespace.yaml"
	serviceAccount  = "serviceaccount.yaml"
	storageClass    = "storageclass.yaml"
	daemonSet       = "node_daemonset.yaml"
	deployment      = "controller_deployment.yaml"
	serviceAccounts = []string{
		"node_sa.yaml",
		"controller_sa.yaml",
	}
	clusterRoles = []string{
		"rbac/provisioner_role.yaml",
		"rbac/attacher_role.yaml",
		"rbac/resizer_role.yaml",
		"rbac/snapshotter_role.yaml",
		"rbac/privileged_role.yaml",
	}
	clusterRoleBindings = []string{
		"rbac/provisioner_binding.yaml",
		"rbac/attacher_binding.yaml",
		"rbac/resizer_binding.yaml",
		"rbac/snapshotter_binding.yaml",
		"rbac/controller_privileged_binding.yaml",
		"rbac/node_privileged_binding.yaml",
	}
)

func (c *csiDriverOperator) syncDeployment(instance *v1alpha1.EBSCSIDriver) (*appsv1.Deployment, error) {
	deploy := c.getExpectedDeployment(instance)

	// Update the deployment when something updated EBSCSIDriver.Spec.LogLevel.
	// The easiest check is for Generation update (i.e. redeploy on any EBSCSIDriver.Spec change).
	// This may update the Deployment more than it is strictly necessary, but the overhead is not that big.
	forceRollout := false
	if instance.Generation != instance.Status.ObservedGeneration {
		forceRollout = true
	}

	if c.versionChanged("operator", c.operatorVersion) {
		// Operator version changed. The new one _may_ have updated Deployment -> we should deploy it.
		forceRollout = true
	}

	if c.versionChanged("aws-ebs-csi-driver", c.operandVersion) {
		// Operand version changed. Update the deployment with a new image.
		forceRollout = true
	}

	deploy, _, err := resourceapply.ApplyDeployment(
		c.kubeClient.AppsV1(),
		c.eventRecorder,
		deploy,
		resourcemerge.ExpectedDeploymentGeneration(deploy, instance.Status.Generations),
		forceRollout)
	if err != nil {
		return nil, err
	}

	return deploy, nil
}

func (c *csiDriverOperator) syncDaemonSet(instance *v1alpha1.EBSCSIDriver) (*appsv1.DaemonSet, error) {
	daemonSet := c.getExpectedDaemonSet(instance)

	// Update the daemonSet when something updated EBSCSIDriver.Spec.LogLevel.
	// The easiest check is for Generation update (i.e. redeploy on any EBSCSIDriver.Spec change).
	// This may update the DaemonSet more than it is strictly necessary, but the overhead is not that big.
	forceRollout := false
	if instance.Generation != instance.Status.ObservedGeneration {
		forceRollout = true
	}

	if c.versionChanged("operator", c.operatorVersion) {
		// Operator version changed. The new one _may_ have updated DaemonSet -> we should deploy it.
		forceRollout = true
	}

	if c.versionChanged("aws-ebs-csi-driver", c.operandVersion) {
		// Operand version changed. Update the deployment with a new image.
		forceRollout = true
	}

	daemonSet, _, err := resourceapply.ApplyDaemonSet(
		c.kubeClient.AppsV1(),
		c.eventRecorder,
		daemonSet,
		resourcemerge.ExpectedDaemonSetGeneration(daemonSet, instance.Status.Generations),
		forceRollout)
	if err != nil {
		return nil, err
	}

	return daemonSet, nil
}

func (c *csiDriverOperator) syncCSIDriver(instance *v1alpha1.EBSCSIDriver) error {
	csiDriver := resourceread.ReadCSIDriverV1Beta1OrDie(generated.MustAsset(csiDriver))

	_, _, err := resourceapply.ApplyCSIDriverV1Beta1(
		c.kubeClient.StorageV1beta1(),
		c.eventRecorder,
		csiDriver)
	if err != nil {
		return err
	}

	return nil
}

func (c *csiDriverOperator) syncNamespace(instance *v1alpha1.EBSCSIDriver) error {
	namespace := resourceread.ReadNamespaceV1OrDie(generated.MustAsset(namespace))

	if namespace.Name != operandNamespace {
		return fmt.Errorf("namespace names mismatch: %q and %q", namespace.Name, operandNamespace)
	}

	_, _, err := resourceapply.ApplyNamespace(
		c.kubeClient.CoreV1(),
		c.eventRecorder,
		namespace)
	if err != nil {
		return err
	}

	return nil
}

func (c *csiDriverOperator) syncServiceAccounts(instance *v1alpha1.EBSCSIDriver) error {
	for _, s := range serviceAccounts {
		serviceAccount := resourceread.ReadServiceAccountV1OrDie(generated.MustAsset(s))

		// Make sure it's created in the correct namespace
		serviceAccount.Namespace = operandNamespace

		_, _, err := resourceapply.ApplyServiceAccount(
			c.kubeClient.CoreV1(),
			c.eventRecorder,
			serviceAccount)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *csiDriverOperator) syncRBAC(instance *v1alpha1.EBSCSIDriver) error {
	for _, r := range clusterRoles {
		role := resourceread.ReadClusterRoleV1OrDie(generated.MustAsset(r))
		_, _, err := resourceapply.ApplyClusterRole(c.kubeClient.RbacV1(), c.eventRecorder, role)
		if err != nil {
			return err
		}
	}

	for _, b := range clusterRoleBindings {
		binding := resourceread.ReadClusterRoleBindingV1OrDie(generated.MustAsset(b))
		_, _, err := resourceapply.ApplyClusterRoleBinding(c.kubeClient.RbacV1(), c.eventRecorder, binding)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *csiDriverOperator) syncStorageClass(instance *v1alpha1.EBSCSIDriver) error {
	storageClass := resourceread.ReadStorageClassV1OrDie(generated.MustAsset(storageClass))

	_, _, err := resourceapply.ApplyStorageClass(
		c.kubeClient.StorageV1(),
		c.eventRecorder,
		storageClass)
	if err != nil {
		return err
	}

	return nil
}

func (c *csiDriverOperator) getExpectedDeployment(instance *v1alpha1.EBSCSIDriver) *appsv1.Deployment {
	deployment := resourceread.ReadDeploymentV1OrDie(generated.MustAsset(deployment))

	if c.csiDriverImage != "" {
		deployment.Spec.Template.Spec.Containers[0].Image = c.csiDriverImage
	}

	logLevel := getLogLevel(instance.Spec.LogLevel)
	for i, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
		if strings.HasPrefix(arg, "--v=") {
			deployment.Spec.Template.Spec.Containers[0].Args[i] = fmt.Sprintf("--v=%d", logLevel)
		}
	}

	return deployment
}

func (c *csiDriverOperator) getExpectedDaemonSet(instance *v1alpha1.EBSCSIDriver) *appsv1.DaemonSet {
	daemonSet := resourceread.ReadDaemonSetV1OrDie(generated.MustAsset(daemonSet))

	if c.csiDriverImage != "" {
		daemonSet.Spec.Template.Spec.Containers[0].Image = c.csiDriverImage
	}

	logLevel := getLogLevel(instance.Spec.LogLevel)
	for i, arg := range daemonSet.Spec.Template.Spec.Containers[0].Args {
		if strings.HasPrefix(arg, "--v=") {
			daemonSet.Spec.Template.Spec.Containers[0].Args[i] = fmt.Sprintf("--v=%d", logLevel)
		}
	}

	return daemonSet
}

func getLogLevel(logLevel operatorv1.LogLevel) int {
	switch logLevel {
	case operatorv1.Normal, "":
		return 2
	case operatorv1.Debug:
		return 4
	case operatorv1.Trace:
		return 6
	case operatorv1.TraceAll:
		return 100
	default:
		return 2
	}
}

func (c *csiDriverOperator) syncStatus(instance *v1alpha1.EBSCSIDriver, deployment *appsv1.Deployment, daemonSet *appsv1.DaemonSet) error {
	c.syncConditions(instance, deployment, daemonSet)

	resourcemerge.SetDeploymentGeneration(&instance.Status.Generations, deployment)
	resourcemerge.SetDaemonSetGeneration(&instance.Status.Generations, daemonSet)

	instance.Status.ObservedGeneration = instance.Generation

	// TODO: what should be the number of replicas? Right now the formula is:
	if deployment != nil && daemonSet != nil {
		if deployment.Status.UnavailableReplicas == 0 && daemonSet.Status.NumberUnavailable == 0 {
			instance.Status.ReadyReplicas = deployment.Status.UpdatedReplicas + daemonSet.Status.UpdatedNumberScheduled
		}
	}

	c.setVersion("operator", c.operatorVersion)
	c.setVersion("aws-ebs-csi-driver", c.operandVersion)

	return nil
}

func (c *csiDriverOperator) syncConditions(instance *v1alpha1.EBSCSIDriver, deployment *appsv1.Deployment, daemonSet *appsv1.DaemonSet) {
	// The operator does not have any prerequisites (at least now)
	v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
		operatorv1.OperatorCondition{
			Type:   operatorv1.OperatorStatusTypePrereqsSatisfied,
			Status: operatorv1.ConditionTrue,
		})
	// The operator is always upgradeable (at least now)
	v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
		operatorv1.OperatorCondition{
			Type:   operatorv1.OperatorStatusTypeUpgradeable,
			Status: operatorv1.ConditionTrue,
		})
	c.syncProgressingCondition(instance, deployment, daemonSet)
	c.syncAvailableCondition(instance, deployment, daemonSet)
}

func (c *csiDriverOperator) syncAvailableCondition(instance *v1alpha1.EBSCSIDriver, deployment *appsv1.Deployment, daemonSet *appsv1.DaemonSet) {
	// TODO: is it enough to check if these values are >0? Or should be more strict and check against the exact desired value?
	isDeploymentAvailable := deployment != nil && deployment.Status.AvailableReplicas > 0
	isDaemonSetAvailable := daemonSet != nil && daemonSet.Status.NumberAvailable > 0
	if isDeploymentAvailable && isDaemonSetAvailable {
		v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
			operatorv1.OperatorCondition{
				Type:   operatorv1.OperatorStatusTypeAvailable,
				Status: operatorv1.ConditionTrue,
			})
	} else {
		v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
			operatorv1.OperatorCondition{
				Type:    operatorv1.OperatorStatusTypeAvailable,
				Status:  operatorv1.ConditionFalse,
				Message: "Waiting for Deployment and DaemonSet to deploy aws-ebs-csi-driver pods",
				Reason:  "AsExpected",
			})
	}
}

func (c *csiDriverOperator) syncProgressingCondition(instance *v1alpha1.EBSCSIDriver, deployment *appsv1.Deployment, daemonSet *appsv1.DaemonSet) {
	// Progressing: true when Deployment or DaemonSet have some work to do
	// (false: when all replicas are updated to the latest release and available)/
	var progressing operatorv1.ConditionStatus
	var progressingMessage string
	var deploymentExpectedReplicas int32
	if deployment != nil && deployment.Spec.Replicas != nil {
		deploymentExpectedReplicas = *deployment.Spec.Replicas
	}
	switch {
	// Controller
	case deployment == nil:
		// Not reachable in theory, but better to be on the safe side...
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment to be created"

	case deployment.Generation != deployment.Status.ObservedGeneration:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment to act on changes"

	case deployment.Status.UnavailableReplicas > 0:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment to deploy controller pods"

	case deployment.Status.UpdatedReplicas < deploymentExpectedReplicas:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment to update pods"

	case deployment.Status.AvailableReplicas < deploymentExpectedReplicas:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment to deploy pods"
	// Node
	case daemonSet == nil:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for DaemonSet to be created"

	case daemonSet.Generation != daemonSet.Status.ObservedGeneration:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for DaemonSet to act on changes"

	case daemonSet.Status.NumberUnavailable > 0:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for DaemonSet to deploy node pods"

	// TODO: the following seems redundant. Remove if that's not the case.

	// case daemonSet.Status.UpdatedNumberScheduled < daemonSet.Status.DesiredNumberScheduled:
	// 	progressing = operatorv1.ConditionTrue
	// 	progressingMessage = "Waiting for DaemonSet to update pods"

	// case daemonSet.Status.NumberAvailable < 1:
	// 	progressing = operatorv1.ConditionTrue
	// 	progressingMessage = "Waiting for DaemonSet to deploy pods"

	default:
		progressing = operatorv1.ConditionFalse
	}
	v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
		operatorv1.OperatorCondition{
			Type:    operatorv1.OperatorStatusTypeProgressing,
			Status:  progressing,
			Message: progressingMessage,
			Reason:  "AsExpected",
		})
}

// TODO: move this to resourceapply package and delete reportDeleteEvent()
func (c *csiDriverOperator) deleteAll() error {
	// Delete all namespaced resources at once by deleting the namespace
	namespace := resourceread.ReadNamespaceV1OrDie(generated.MustAsset(namespace))
	err := c.kubeClient.CoreV1().Namespaces().Delete(context.TODO(), namespace.Name, metav1.DeleteOptions{})
	reportDeleteEvent(c.eventRecorder, namespace, err)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Then delete all non-namespaced ones
	storageClass := resourceread.ReadStorageClassV1OrDie(generated.MustAsset(storageClass))
	err = c.kubeClient.StorageV1().StorageClasses().Delete(context.TODO(), storageClass.Name, metav1.DeleteOptions{})
	reportDeleteEvent(c.eventRecorder, storageClass, err)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	csiDriver := resourceread.ReadCSIDriverV1Beta1OrDie(generated.MustAsset(csiDriver))
	err = c.kubeClient.StorageV1beta1().CSIDrivers().Delete(context.TODO(), csiDriver.Name, metav1.DeleteOptions{})
	reportDeleteEvent(c.eventRecorder, csiDriver, err)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	for _, r := range clusterRoles {
		role := resourceread.ReadClusterRoleV1OrDie(generated.MustAsset(r))
		err := c.kubeClient.RbacV1().ClusterRoles().Delete(context.TODO(), role.Name, metav1.DeleteOptions{})
		reportDeleteEvent(c.eventRecorder, role, err)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	for _, b := range clusterRoleBindings {
		binding := resourceread.ReadClusterRoleBindingV1OrDie(generated.MustAsset(b))
		err := c.kubeClient.RbacV1().ClusterRoleBindings().Delete(context.TODO(), binding.Name, metav1.DeleteOptions{})
		reportDeleteEvent(c.eventRecorder, binding, err)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

func reportDeleteEvent(recorder events.Recorder, obj runtime.Object, originalErr error, details ...string) {
	gvk := resourcehelper.GuessObjectGroupVersionKind(obj)
	switch {
	case originalErr != nil && !apierrors.IsNotFound(originalErr):
		recorder.Warningf(fmt.Sprintf("%sDeleteFailed", gvk.Kind), "Failed to delete %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(obj), originalErr)
	case len(details) == 0:
		recorder.Eventf(fmt.Sprintf("%sDeleted", gvk.Kind), "Deleted %s", resourcehelper.FormatResourceForCLIWithNamespace(obj))
	default:
		recorder.Eventf(fmt.Sprintf("%sDeleted", gvk.Kind), "Deleted %s:\n%s", resourcehelper.FormatResourceForCLIWithNamespace(obj), strings.Join(details, "\n"))
	}
}
