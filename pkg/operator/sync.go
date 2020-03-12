package operator

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	storagev1 "k8s.io/api/storage/v1"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/apis/operator/v1alpha1"
	"github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/generated"
)

const (
	deployment   = "controller_deployment.yaml"
	daemonSet    = "node_daemonset.yaml"
	storageClass = "storageclass.yaml"
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

func (c *csiDriverOperator) syncStorageClass(instance *v1alpha1.EBSCSIDriver) (*storagev1.StorageClass, error) {
	storageClass := resourceread.ReadStorageClassV1OrDie(generated.MustAsset(storageClass))

	storageClass, _, err := resourceapply.ApplyStorageClass(
		c.kubeClient.StorageV1(),
		c.eventRecorder,
		storageClass)

	if err != nil {
		return nil, err
	}

	return storageClass, nil
}

func (c *csiDriverOperator) getExpectedDeployment(instance *v1alpha1.EBSCSIDriver) *appsv1.Deployment {
	deployment := resourceread.ReadDeploymentV1OrDie(generated.MustAsset(deployment))

	// FIXME
	// deployment.Spec.Template.Spec.Containers[0].Image = c.csiDriverImage

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

	// FIXME
	// daemonSet.Spec.Template.Spec.Containers[0].Image = c.csiDriverImage

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
	if deployment != nil {
		instance.Status.ReadyReplicas = deployment.Status.UpdatedReplicas
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
	// Available: deployment and daemonset are available, regardless at which version
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
	case deployment == nil || daemonSet == nil:
		// Not reachable in theory, but better to be on the safe side...
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment and DaemonSet to be created"

	case (deployment.Generation != deployment.Status.ObservedGeneration) || (daemonSet.Generation != daemonSet.Status.ObservedGeneration):
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment and DaemonSet to act on changes"

	case deployment.Status.UnavailableReplicas > 0:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment to deploy csi-ebs-controller pods"

	case deployment.Status.UpdatedReplicas < deploymentExpectedReplicas:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment to update pods"

	case deployment.Status.AvailableReplicas < deploymentExpectedReplicas:
		progressing = operatorv1.ConditionTrue
		progressingMessage = "Waiting for Deployment to deploy pods"

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
