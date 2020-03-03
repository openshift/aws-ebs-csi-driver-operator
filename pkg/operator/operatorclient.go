package operator

import (
	"k8s.io/client-go/tools/cache"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/apis/operator/v1alpha1"
	configset "github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/generated/clientset/versioned/typed/operator/v1alpha1"
	informers "github.com/bertinatto/aws-ebs-csi-driver-operator/pkg/generated/informers/externalversions"
)

type OperatorClient struct {
	Informers informers.SharedInformerFactory
	Client    configset.EBSCSIDriversGetter
}

func (c OperatorClient) Informer() cache.SharedIndexInformer {
	// return c.Informers.Operator().V1().CSISnapshotControllers().Informer()
	return c.Informers.Csi().V1alpha1().EBSCSIDrivers().Informer()
}

func (c OperatorClient) GetOperatorState() (*operatorv1.OperatorSpec, *operatorv1.OperatorStatus, string, error) {
	// instance, err := c.Informers.Operator().V1().CSISnapshotControllers().Lister().Get(globalConfigName)
	instance, err := c.Informers.Csi().V1alpha1().EBSCSIDrivers().Lister().EBSCSIDrivers(targetNamespace).Get(globalConfigName)
	if err != nil {
		return nil, nil, "", err
	}

	return &instance.Spec.OperatorSpec, &instance.Status.OperatorStatus, instance.ResourceVersion, nil
}

func (c OperatorClient) UpdateOperatorSpec(resourceVersion string, spec *operatorv1.OperatorSpec) (*operatorv1.OperatorSpec, string, error) {
	// original, err := c.Informers.Operator().V1().CSISnapshotControllers().Lister().Get(globalConfigName)
	original, err := c.Informers.Csi().V1alpha1().EBSCSIDrivers().Lister().EBSCSIDrivers(targetNamespace).Get(globalConfigName)
	if err != nil {
		return nil, "", err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Spec.OperatorSpec = *spec

	ret, err := c.Client.EBSCSIDrivers(targetNamespace).Update(copy)
	if err != nil {
		return nil, "", err
	}

	return &ret.Spec.OperatorSpec, ret.ResourceVersion, nil
}

func (c OperatorClient) UpdateOperatorStatus(resourceVersion string, status *operatorv1.OperatorStatus) (*operatorv1.OperatorStatus, error) {
	// original, err := c.Informers.Operator().V1().CSISnapshotControllers().Lister().Get(globalConfigName)
	original, err := c.Informers.Csi().V1alpha1().EBSCSIDrivers().Lister().EBSCSIDrivers(targetNamespace).Get(globalConfigName)
	if err != nil {
		return nil, err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Status.OperatorStatus = *status

	// ret, err := c.Client.CSISnapshotControllers().UpdateStatus(copy)
	ret, err := c.Client.EBSCSIDrivers(targetNamespace).UpdateStatus(copy)
	if err != nil {
		return nil, err
	}

	return &ret.Status.OperatorStatus, nil
}

func (c OperatorClient) GetOperatorInstance() (*v1alpha1.EBSCSIDriver, error) {
	// instance, err := c.Informers.Operator().V1().CSISnapshotControllers().Lister().Get(globalConfigName)
	instance, err := c.Informers.Csi().V1alpha1().EBSCSIDrivers().Lister().EBSCSIDrivers(targetNamespace).Get(globalConfigName)
	if err != nil {
		return nil, err
	}
	return instance, nil
}
