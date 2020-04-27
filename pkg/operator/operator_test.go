package operator

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	coreinformers "k8s.io/client-go/informers"
	fakecore "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/status"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/apis/operator/v1alpha1"
	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated"
	fakeop "github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated/clientset/versioned/fake"
	opinformers "github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated/informers/externalversions"
)

type operatorTest struct {
	name            string
	images          images
	initialObjects  testObjects
	expectedObjects testObjects
	reactors        testReactors
	expectErr       bool
}

type testObjects struct {
	deployment         *appsv1.Deployment
	daemonSet          *appsv1.DaemonSet
	credentialsRequest *unstructured.Unstructured
	ebsCSIDriver       *v1alpha1.AWSEBSDriver
	credentialsSecret  *v1.Secret
}

type testContext struct {
	operator          *csiDriverOperator
	coreClient        *fakecore.Clientset
	coreInformers     coreinformers.SharedInformerFactory
	operatorClient    *fakeop.Clientset
	operatorInformers opinformers.SharedInformerFactory
	dynamicClient     *fakeDynamicClient
}

type addCoreReactors func(*fakecore.Clientset, coreinformers.SharedInformerFactory)
type addOperatorReactors func(*fakeop.Clientset, opinformers.SharedInformerFactory)

type testReactors struct {
	deployments   addCoreReactors
	daemonSets    addCoreReactors
	ebsCSIDrivers addOperatorReactors
}

const testVersion = "0.0.1" // Version of the operator for testing purposes (instead of getenv)

func newOperator(test operatorTest) *testContext {
	// Convert to []runtime.Object
	var initialObjects []runtime.Object
	if test.initialObjects.deployment != nil {
		addDeploymentHash(test.initialObjects.deployment)
		initialObjects = append(initialObjects, test.initialObjects.deployment)
	}

	if test.initialObjects.daemonSet != nil {
		addDaemonSetHash(test.initialObjects.daemonSet)
		initialObjects = append(initialObjects, test.initialObjects.daemonSet)
	}

	if test.initialObjects.credentialsSecret != nil {
		initialObjects = append(initialObjects, test.initialObjects.credentialsSecret)
	}

	coreClient := fakecore.NewSimpleClientset(initialObjects...)
	coreInformerFactory := coreinformers.NewSharedInformerFactory(coreClient, 0 /*no resync */)

	// Fill the informer
	if test.initialObjects.deployment != nil {
		coreInformerFactory.Apps().V1().Deployments().Informer().GetIndexer().Add(test.initialObjects.deployment)
	}
	if test.initialObjects.daemonSet != nil {
		coreInformerFactory.Apps().V1().DaemonSets().Informer().GetIndexer().Add(test.initialObjects.daemonSet)
	}
	if test.initialObjects.credentialsSecret != nil {
		coreInformerFactory.Core().V1().Secrets().Informer().GetIndexer().Add(test.initialObjects.credentialsSecret)
	}
	if test.reactors.deployments != nil {
		test.reactors.deployments(coreClient, coreInformerFactory)
	}
	if test.reactors.daemonSets != nil {
		test.reactors.daemonSets(coreClient, coreInformerFactory)
	}

	// Convert to []runtime.Object
	var initialDrivers []runtime.Object
	if test.initialObjects.ebsCSIDriver != nil {
		initialDrivers = []runtime.Object{test.initialObjects.ebsCSIDriver}
	}
	operatorClient := fakeop.NewSimpleClientset(initialDrivers...)
	operatorInformerFactory := opinformers.NewSharedInformerFactory(operatorClient, 0)

	// Fill the informer
	if test.initialObjects.ebsCSIDriver != nil {
		operatorInformerFactory.Csi().V1alpha1().AWSEBSDrivers().Informer().GetIndexer().Add(test.initialObjects.ebsCSIDriver)
	}
	if test.reactors.ebsCSIDrivers != nil {
		test.reactors.ebsCSIDrivers(operatorClient, operatorInformerFactory)
	}

	// Add global reactors
	addGenerationReactor(coreClient)

	client := OperatorClient{
		Client:    operatorClient.CsiV1alpha1(),
		Informers: operatorInformerFactory,
	}

	versionGetter := status.NewVersionGetter()
	versionGetter.SetVersion("operator", testVersion)
	versionGetter.SetVersion("aws-ebs-csi-driver", testVersion)

	dynamicClient := &fakeDynamicClient{}
	if test.initialObjects.credentialsRequest != nil {
		addCredentialsRequestHash(test.initialObjects.credentialsRequest)
		dynamicClient.credentialRequest = test.initialObjects.credentialsRequest
	}

	recorder := events.NewInMemoryRecorder("operator")
	op := NewCSIDriverOperator(client,
		coreInformerFactory.Core().V1().PersistentVolumes(),
		coreInformerFactory.Core().V1().Namespaces(),
		coreInformerFactory.Storage().V1beta1().CSIDrivers(),
		coreInformerFactory.Core().V1().ServiceAccounts(),
		coreInformerFactory.Rbac().V1().ClusterRoles(),
		coreInformerFactory.Rbac().V1().ClusterRoleBindings(),
		coreInformerFactory.Apps().V1().Deployments(),
		coreInformerFactory.Apps().V1().DaemonSets(),
		coreInformerFactory.Storage().V1().StorageClasses(),
		coreInformerFactory.Core().V1().Secrets(),
		coreClient,
		dynamicClient,
		versionGetter,
		recorder,
		testVersion,
		testVersion,
		test.images,
	)

	return &testContext{
		operator:          op,
		coreClient:        coreClient,
		coreInformers:     coreInformerFactory,
		operatorClient:    operatorClient,
		operatorInformers: operatorInformerFactory,
		dynamicClient:     dynamicClient,
	}
}

// Drivers

type ebsCSIDriverModifier func(*v1alpha1.AWSEBSDriver) *v1alpha1.AWSEBSDriver

func ebsCSIDriver(modifiers ...ebsCSIDriverModifier) *v1alpha1.AWSEBSDriver {
	instance := &v1alpha1.AWSEBSDriver{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "cluster",
			Generation: 0,
			Finalizers: []string{operatorFinalizer},
		},
		Spec: v1alpha1.AWSEBSDriverSpec{
			OperatorSpec: opv1.OperatorSpec{
				ManagementState: opv1.Managed,
			},
		},
		Status: v1alpha1.AWSEBSDriverStatus{},
	}
	for _, modifier := range modifiers {
		instance = modifier(instance)
	}
	return instance
}

func withStatus(readyReplicas int32) ebsCSIDriverModifier {
	return func(i *v1alpha1.AWSEBSDriver) *v1alpha1.AWSEBSDriver {
		i.Status = v1alpha1.AWSEBSDriverStatus{
			OperatorStatus: opv1.OperatorStatus{
				ReadyReplicas: readyReplicas,
			},
		}
		return i
	}
}

func withLogLevel(logLevel opv1.LogLevel) ebsCSIDriverModifier {
	return func(i *v1alpha1.AWSEBSDriver) *v1alpha1.AWSEBSDriver {
		i.Spec.LogLevel = logLevel
		return i
	}
}

func withGeneration(generations ...int64) ebsCSIDriverModifier {
	return func(i *v1alpha1.AWSEBSDriver) *v1alpha1.AWSEBSDriver {
		i.Generation = generations[0]
		if len(generations) > 1 {
			i.Status.ObservedGeneration = generations[1]
		}
		return i
	}
}

func withGenerations(deployment, daemonset, credentialsRequest int64) ebsCSIDriverModifier {
	return func(i *v1alpha1.AWSEBSDriver) *v1alpha1.AWSEBSDriver {
		i.Status.Generations = []opv1.GenerationStatus{
			{
				Group:          appsv1.GroupName,
				LastGeneration: deployment,
				Name:           "aws-ebs-csi-driver-controller",
				Namespace:      operandNamespace,
				Resource:       "deployments",
			},
			{
				Group:          appsv1.GroupName,
				LastGeneration: daemonset,
				Name:           "aws-ebs-csi-driver-node",
				Namespace:      operandNamespace,
				Resource:       "daemonsets",
			},
			{
				Group:          credentialsRequestGroup,
				LastGeneration: credentialsRequest,
				Name:           "openshift-aws-ebs-csi-driver",
				Namespace:      credentialRequestNamespace,
				Resource:       credentialsRequestResource,
			},
		}
		return i
	}
}

func withTrueConditions(conditions ...string) ebsCSIDriverModifier {
	return func(i *v1alpha1.AWSEBSDriver) *v1alpha1.AWSEBSDriver {
		if i.Status.Conditions == nil {
			i.Status.Conditions = []opv1.OperatorCondition{}
		}
		for _, c := range conditions {
			i.Status.Conditions = append(i.Status.Conditions, opv1.OperatorCondition{
				Type:   c,
				Status: opv1.ConditionTrue,
			})
		}
		return i
	}
}

func withFalseConditions(conditions ...string) ebsCSIDriverModifier {
	return func(i *v1alpha1.AWSEBSDriver) *v1alpha1.AWSEBSDriver {
		if i.Status.Conditions == nil {
			i.Status.Conditions = []opv1.OperatorCondition{}
		}
		for _, c := range conditions {
			i.Status.Conditions = append(i.Status.Conditions, opv1.OperatorCondition{
				Type:   c,
				Status: opv1.ConditionFalse,
			})
		}
		return i
	}
}

// Deployments

type deploymentModifier func(*appsv1.Deployment) *appsv1.Deployment

func getDeployment(logLevel int, images images, modifiers ...deploymentModifier) *appsv1.Deployment {
	dep := resourceread.ReadDeploymentV1OrDie(generated.MustAsset(deployment))
	dep.Spec.Template.Spec.Containers[csiDriverContainerIndex].Image = images.csiDriver
	dep.Spec.Template.Spec.Containers[provisionerContainerIndex].Image = images.provisioner
	dep.Spec.Template.Spec.Containers[attacherContainerIndex].Image = images.attacher
	dep.Spec.Template.Spec.Containers[resizerContainerIndex].Image = images.resizer
	dep.Spec.Template.Spec.Containers[snapshottterContainerIndex].Image = images.snapshotter

	var one int32 = 1
	dep.Spec.Replicas = &one

	for i, container := range dep.Spec.Template.Spec.Containers {
		for j, arg := range container.Args {
			if strings.HasPrefix(arg, "--v=") {
				dep.Spec.Template.Spec.Containers[i].Args[j] = fmt.Sprintf("--v=%d", logLevel)
			}
		}
	}

	// Set by ApplyDeployment()
	if dep.Annotations == nil {
		dep.Annotations = map[string]string{}
	}
	dep.Annotations["operator.openshift.io/pull-spec"] = images.csiDriver

	for _, modifier := range modifiers {
		dep = modifier(dep)
	}

	return dep
}

func withDeploymentStatus(readyReplicas, availableReplicas, updatedReplicas int32) deploymentModifier {
	return func(instance *appsv1.Deployment) *appsv1.Deployment {
		instance.Status.ReadyReplicas = readyReplicas
		instance.Status.AvailableReplicas = availableReplicas
		instance.Status.UpdatedReplicas = updatedReplicas
		return instance
	}
}

func withDeploymentReplicas(replicas int32) deploymentModifier {
	return func(instance *appsv1.Deployment) *appsv1.Deployment {
		instance.Spec.Replicas = &replicas
		return instance
	}
}

func withDeploymentGeneration(generations ...int64) deploymentModifier {
	return func(instance *appsv1.Deployment) *appsv1.Deployment {
		instance.Generation = generations[0]
		if len(generations) > 1 {
			instance.Status.ObservedGeneration = generations[1]
		}
		return instance
	}
}

// DaemonSets

type daemonSetModifier func(*appsv1.DaemonSet) *appsv1.DaemonSet

func getDaemonSet(logLevel int, images images, modifiers ...daemonSetModifier) *appsv1.DaemonSet {
	ds := resourceread.ReadDaemonSetV1OrDie(generated.MustAsset(daemonSet))
	ds.Spec.Template.Spec.Containers[csiDriverContainerIndex].Image = images.csiDriver
	ds.Spec.Template.Spec.Containers[nodeDriverRegistrarContainerIndex].Image = images.nodeDriverRegistrar
	ds.Spec.Template.Spec.Containers[livenessProbeContainerIndex].Image = images.livenessProbe

	for i, container := range ds.Spec.Template.Spec.Containers {
		for j, arg := range container.Args {
			if strings.HasPrefix(arg, "--v=") {
				ds.Spec.Template.Spec.Containers[i].Args[j] = fmt.Sprintf("--v=%d", logLevel)
			}
		}
	}

	// Set by ApplyDaemonSet()
	if ds.Annotations == nil {
		ds.Annotations = map[string]string{}
	}
	ds.Annotations["operator.openshift.io/pull-spec"] = images.csiDriver

	for _, modifier := range modifiers {
		ds = modifier(ds)
	}

	return ds
}

func withDaemonSetStatus(numberReady, updatedNumber, numberAvailable int32) daemonSetModifier {
	return func(instance *appsv1.DaemonSet) *appsv1.DaemonSet {
		instance.Status.NumberReady = numberReady
		instance.Status.NumberAvailable = numberAvailable
		instance.Status.UpdatedNumberScheduled = updatedNumber
		return instance
	}
}

func withDaemonSetReplicas(replicas int32) daemonSetModifier {
	return func(instance *appsv1.DaemonSet) *appsv1.DaemonSet {
		instance.Status.DesiredNumberScheduled = replicas
		return instance
	}
}

func withDaemonSetGeneration(generations ...int64) daemonSetModifier {
	return func(instance *appsv1.DaemonSet) *appsv1.DaemonSet {
		instance.Generation = generations[0]
		if len(generations) > 1 {
			instance.Status.ObservedGeneration = generations[1]
		}
		return instance
	}
}

// CredentialsRequest
type credentialsRequestModifier func(*unstructured.Unstructured) *unstructured.Unstructured

func getCredentialsRequest(modifiers ...credentialsRequestModifier) *unstructured.Unstructured {
	cr := readCredentialRequestsOrDie(generated.MustAsset(credentialsRequest))
	for _, modifier := range modifiers {
		cr = modifier(cr)
	}
	return cr
}

func withCredentialsRequestGeneration(generation int64) credentialsRequestModifier {
	return func(cr *unstructured.Unstructured) *unstructured.Unstructured {
		cr.SetGeneration(generation)
		return cr
	}
}

// Secret with cloud credentials
func getCredentialsSecret() *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credentialsSecret,
			Namespace: operandNamespace,
		},
		Data: map[string][]byte{
			"aws_access_key_id":     []byte("foo"),
			"aws_secret_access_key": []byte("bar"),
		},
		Type: "opaque",
	}
}

// This reactor is always enabled and bumps Deployment and DaemonSet generation when they get updated.
func addGenerationReactor(client *fakecore.Clientset) {
	client.PrependReactor("*", "deployments", func(action core.Action) (handled bool, ret runtime.Object, err error) {
		switch a := action.(type) {
		case core.CreateActionImpl:
			object := a.GetObject()
			deployment := object.(*appsv1.Deployment)
			deployment.Generation++
			return false, deployment, nil
		case core.UpdateActionImpl:
			object := a.GetObject()
			deployment := object.(*appsv1.Deployment)
			deployment.Generation++
			return false, deployment, nil
		}
		return false, nil, nil
	})

	client.PrependReactor("*", "daemonsets", func(action core.Action) (handled bool, ret runtime.Object, err error) {
		switch a := action.(type) {
		case core.CreateActionImpl:
			object := a.GetObject()
			ds := object.(*appsv1.DaemonSet)
			ds.Generation++
			return false, ds, nil
		case core.UpdateActionImpl:
			object := a.GetObject()
			ds := object.(*appsv1.DaemonSet)
			ds.Generation++
			return false, ds, nil
		}
		return false, nil, nil
	})
}

func TestSync(t *testing.T) {
	const replica0 = 0
	const replica1 = 1
	const replica2 = 2
	var argsLevel2 = 2
	var argsLevel6 = 6

	tests := []operatorTest{
		{
			// Only Driver exists, everything else is created
			name:   "initial sync without cloud Secret",
			images: defaultImages(),
			initialObjects: testObjects{
				ebsCSIDriver: ebsCSIDriver(),
			},
			expectedObjects: testObjects{
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
			},
			expectErr: true,
		},
		{
			// Only Driver exists, everything else is created
			name:   "initial sync with cloud Secret",
			images: defaultImages(),
			initialObjects: testObjects{
				ebsCSIDriver: ebsCSIDriver(),
				// Adding secrets to test Deployment / DaemonSet creation
				credentialsSecret: getCredentialsSecret(),
			},
			expectedObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(1, 0)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 0)),
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica0),
					withGenerations(1, 1, 1),
					withTrueConditions(opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied, opv1.OperatorStatusTypeProgressing),
					withFalseConditions(opv1.OperatorStatusTypeDegraded, opv1.OperatorStatusTypeAvailable)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
		},
		{
			// Deployment is fully deployed and its status is synced to Driver
			name:   "deployment fully deployed",
			images: defaultImages(),
			initialObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver:       ebsCSIDriver(withGenerations(1, 1, 1)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
			expectedObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica2), // 1 deployment + 1 daemonSet
					withGenerations(1, 1, 1),
					withTrueConditions(opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied),
					withFalseConditions(opv1.OperatorStatusTypeDegraded, opv1.OperatorStatusTypeProgressing)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
		},
		{
			// Deployment has wrong nr. of replicas, modified by user, and gets replaced by the operator.
			name:   "deployment modified by user",
			images: defaultImages(),
			initialObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentReplicas(2),      // User changed replicas
					withDeploymentGeneration(2, 1), // ... which changed Generation
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(2, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver:       ebsCSIDriver(withGenerations(1, 1, 1)), // the operator knows the old generation of the Deployment
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
			expectedObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentReplicas(1),      // The operator fixed replica count
					withDeploymentGeneration(3, 1), // ... which bumps generation again
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(3, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica2),     // 1 deployment + 1 daemonSet
					withGenerations(3, 3, 1), // now the operator knows generation 1
					withTrueConditions(opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied, opv1.OperatorStatusTypeProgressing), // Progressing due to Generation change
					withFalseConditions(opv1.OperatorStatusTypeDegraded)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
		},
		{
			// CredentialRequests is modified by user, and gets replaced by the operator.
			name:   "CredentialRequests modified by user",
			images: defaultImages(),
			initialObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentReplicas(1),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver:       ebsCSIDriver(withGenerations(1, 1, 1)),                     // the operator knows the old generation of the Deployment
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(2)), // modified by user
				credentialsSecret:  getCredentialsSecret(),
			},
			expectedObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentReplicas(1),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica2),     // 1 deployment + 1 daemonSet
					withGenerations(1, 1, 3), // now the operator knows generation 3
					withTrueConditions(opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied),
					withFalseConditions(opv1.OperatorStatusTypeDegraded, opv1.OperatorStatusTypeProgressing)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(3)), // Updated by the operator
				credentialsSecret:  getCredentialsSecret(),
			},
		},
		{
			// Deployment gets degraded for some reason
			name:   "deployment degraded",
			images: defaultImages(),
			initialObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(0, 0, 0)), // the Deployment has no pods
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(1, 1, 1)), // the DaemonSet has 1 pod
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica1),
					withGenerations(1, 1, 1),
					withGeneration(1, 1),
					withTrueConditions(opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied),
					withFalseConditions(opv1.OperatorStatusTypeDegraded, opv1.OperatorStatusTypeProgressing)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
			expectedObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(0, 0, 0)), // no change to the Deployment
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(1, 1, 1)),
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica1), // 0 deployments + 1 daemonSet
					withGenerations(1, 1, 1),
					withGeneration(1, 1),
					withTrueConditions(opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied, opv1.OperatorStatusTypeProgressing), // The operator is Progressing
					withFalseConditions(opv1.OperatorStatusTypeDegraded, opv1.OperatorStatusTypeAvailable)),                                             // The operator is not Available (controller not running...)
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
		},
		{
			// Deployment is updating pods
			name:   "update",
			images: defaultImages(),
			initialObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(1 /*ready*/, 1 /*available*/, 0 /*updated*/)), // the Deployment is updating 1 pod
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(1, 1, 1)), // the DaemonSet has 1 pod
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica1),
					withGenerations(1, 1, 1),
					withGeneration(1, 1),
					withTrueConditions(opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied),
					withFalseConditions(opv1.OperatorStatusTypeDegraded, opv1.OperatorStatusTypeProgressing)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
			expectedObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(1, 1, 0)), // no change to the Deployment
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(1, 1, 1)), // no change to the DaemonSet
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica1), // 0 deployments + 1 daemonSet
					withGenerations(1, 1, 1),
					withGeneration(1, 1),
					withTrueConditions(opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied, opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeProgressing), // The operator is Progressing, but still Available
					withFalseConditions(opv1.OperatorStatusTypeDegraded)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
		},
		{
			// User changes log level and it's projected into the Deployment and DaemonSet
			name:   "log level change",
			images: defaultImages(),
			initialObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver: ebsCSIDriver(
					withGenerations(1, 1, 1),
					withLogLevel(opv1.Trace), // User changed the log level...
					withGeneration(2, 1)),    //... which caused the Generation to increase
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
			expectedObjects: testObjects{
				deployment: getDeployment(argsLevel6, defaultImages(), // The operator changed cmdline arguments with a new log level
					withDeploymentGeneration(2, 1), // ... which caused the Generation to increase
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel6, defaultImages(), // And the same goes for the DaemonSet
					withDaemonSetGeneration(2, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica2), // 1 deployment + 1 daemonSet
					withLogLevel(opv1.Trace),
					withGenerations(2, 2, 1),
					withGeneration(2, 2),
					withTrueConditions(opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied, opv1.OperatorStatusTypeProgressing), // Progressing due to Generation change
					withFalseConditions(opv1.OperatorStatusTypeDegraded)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
		},
		{
			// Deployment and DaemonSet update images
			name:   "image change",
			images: defaultImages(),
			initialObjects: testObjects{
				deployment: getDeployment(argsLevel2, oldImages(),
					withDeploymentGeneration(1, 1),
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, oldImages(),
					withDaemonSetGeneration(1, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica2), // 1 deployment + 1 daemonSet
					withGenerations(1, 1, 1),
					withTrueConditions(opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied),
					withFalseConditions(opv1.OperatorStatusTypeDegraded, opv1.OperatorStatusTypeProgressing)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
			expectedObjects: testObjects{
				deployment: getDeployment(argsLevel2, defaultImages(),
					withDeploymentGeneration(2, 1),
					withDeploymentStatus(replica1, replica1, replica1)),
				daemonSet: getDaemonSet(argsLevel2, defaultImages(),
					withDaemonSetGeneration(2, 1),
					withDaemonSetStatus(replica1, replica1, replica1)),
				ebsCSIDriver: ebsCSIDriver(
					withStatus(replica2), // 1 deployment + 1 daemonSet
					withGenerations(2, 2, 1),
					withTrueConditions(opv1.OperatorStatusTypeAvailable, opv1.OperatorStatusTypeUpgradeable, opv1.OperatorStatusTypePrereqsSatisfied, opv1.OperatorStatusTypeProgressing),
					withFalseConditions(opv1.OperatorStatusTypeDegraded)),
				credentialsRequest: getCredentialsRequest(withCredentialsRequestGeneration(1)),
				credentialsSecret:  getCredentialsSecret(),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Initialize
			ctx := newOperator(test)

			// Act
			err := ctx.operator.sync()

			// Assert
			// Check error
			if err != nil && !test.expectErr {
				t.Errorf("sync() returned unexpected error: %v", err)
			}
			if err == nil && test.expectErr {
				t.Error("sync() unexpectedly succeeded when error was expected")
			}

			// Check expectedObjects.deployment
			if test.expectedObjects.deployment != nil {
				deployName := test.expectedObjects.deployment.Name
				actualDeployment, err := ctx.coreClient.AppsV1().Deployments(operandNamespace).Get(context.TODO(), deployName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to get Deployment %s: %v", operandName, err)
				}
				sanitizeDeployment(actualDeployment)
				sanitizeDeployment(test.expectedObjects.deployment)
				if !equality.Semantic.DeepEqual(test.expectedObjects.deployment, actualDeployment) {
					t.Errorf("Unexpected Deployment %+v content:\n%s", operandName, cmp.Diff(test.expectedObjects.deployment, actualDeployment))
				}
			}

			// Check expectedObjects.daemonSet
			if test.expectedObjects.daemonSet != nil {
				dsName := test.expectedObjects.daemonSet.Name
				actualDaemonSet, err := ctx.coreClient.AppsV1().DaemonSets(operandNamespace).Get(context.TODO(), dsName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to get DaemonSet %s: %v", operandName, err)
				}
				sanitizeDaemonSet(actualDaemonSet)
				sanitizeDaemonSet(test.expectedObjects.daemonSet)
				if !equality.Semantic.DeepEqual(test.expectedObjects.daemonSet, actualDaemonSet) {
					t.Errorf("Unexpected DaemonSet %+v content:\n%s", operandName, cmp.Diff(test.expectedObjects.daemonSet, actualDaemonSet))
				}
			}

			// Check expectedObjects.ebsCSIDriver
			if test.expectedObjects.ebsCSIDriver != nil {
				actualEBSCSIDriver, err := ctx.operatorClient.CsiV1alpha1().AWSEBSDrivers().Get(context.TODO(), globalConfigName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to get Driver %s: %v", globalConfigName, err)
				}
				sanitizeEBSCSIDriver(actualEBSCSIDriver)
				sanitizeEBSCSIDriver(test.expectedObjects.ebsCSIDriver)
				if !equality.Semantic.DeepEqual(test.expectedObjects.ebsCSIDriver, actualEBSCSIDriver) {
					t.Errorf("Unexpected Driver %+v content:\n%s", operandName, cmp.Diff(test.expectedObjects.ebsCSIDriver, actualEBSCSIDriver))
				}
			}

			// Check expectedObjects.credentialsRequest
			if test.expectedObjects.credentialsRequest != nil {
				actualRequest := ctx.dynamicClient.credentialRequest
				sanitizeCredentialsRequest(actualRequest)
				sanitizeCredentialsRequest(test.expectedObjects.credentialsRequest)
				if !equality.Semantic.DeepEqual(test.expectedObjects.credentialsRequest, actualRequest) {
					t.Errorf("Unexpected CredentialsRequest %+v content:\n%s", operandName, cmp.Diff(test.expectedObjects.credentialsRequest, actualRequest))
				}
			}
		})
	}
}

func sanitizeDeployment(deployment *appsv1.Deployment) {
	// nil and empty array are the same
	if len(deployment.Labels) == 0 {
		deployment.Labels = nil
	}
	if len(deployment.Annotations) == 0 {
		deployment.Annotations = nil
	}
	// Remove force annotations, they're random
	delete(deployment.Annotations, "operator.openshift.io/force")
	delete(deployment.Annotations, specHashAnnotation)
	delete(deployment.Spec.Template.Annotations, "operator.openshift.io/force")
}

func sanitizeDaemonSet(daemonSet *appsv1.DaemonSet) {
	// nil and empty array are the same
	if len(daemonSet.Labels) == 0 {
		daemonSet.Labels = nil
	}
	if len(daemonSet.Annotations) == 0 {
		daemonSet.Annotations = nil
	}
	// Remove force annotations, they're random
	delete(daemonSet.Annotations, "operator.openshift.io/force")
	delete(daemonSet.Annotations, specHashAnnotation)
	delete(daemonSet.Spec.Template.Annotations, "operator.openshift.io/force")
}

func sanitizeEBSCSIDriver(instance *v1alpha1.AWSEBSDriver) {
	// Remove condition texts
	for i := range instance.Status.Conditions {
		instance.Status.Conditions[i].LastTransitionTime = metav1.Time{}
		instance.Status.Conditions[i].Message = ""
		instance.Status.Conditions[i].Reason = ""
	}
	// Sort the conditions by name to have consistent position in the array
	sort.Slice(instance.Status.Conditions, func(i, j int) bool {
		return instance.Status.Conditions[i].Type < instance.Status.Conditions[j].Type
	})
}

func sanitizeCredentialsRequest(instance *unstructured.Unstructured) {
	// Ignore ResourceVersion
	if instance == nil {
		return
	}
	instance.SetResourceVersion("0")
	annotations := instance.GetAnnotations()
	if annotations != nil {
		delete(annotations, specHashAnnotation)
		if len(annotations) == 0 {
			annotations = nil
		}
		instance.SetAnnotations(annotations)
	}
}

func defaultImages() images {
	return images{
		csiDriver:           "quay.io/openshift/origin-aws-ebs-csi-driver:latest",
		provisioner:         "quay.io/openshift/origin-csi-external-provisioner:latest",
		attacher:            "quay.io/openshift/origin-csi-external-attacher:latest",
		resizer:             "quay.io/openshift/origin-csi-external-resizer:latest",
		snapshotter:         "quay.io/openshift/origin-csi-external-snapshotter:latest",
		nodeDriverRegistrar: "quay.io/openshift/origin-csi-node-driver-registrar:latest",
		livenessProbe:       "quay.io/openshift/origin-csi-livenessprobe:latest",
	}
}

func oldImages() images {
	return images{
		csiDriver:           "quay.io/openshift/origin-aws-ebs-csi-driver:old",
		provisioner:         "quay.io/openshift/origin-csi-external-provisioner:old",
		attacher:            "quay.io/openshift/origin-csi-external-attacher:old",
		resizer:             "quay.io/openshift/origin-csi-external-resizer:old",
		snapshotter:         "quay.io/openshift/origin-csi-external-snapshotter:old",
		nodeDriverRegistrar: "quay.io/openshift/origin-csi-node-driver-registrar:old",
		livenessProbe:       "quay.io/openshift/origin-csi-livenessprobe:old",
	}
}
