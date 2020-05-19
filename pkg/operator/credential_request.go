package operator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/openshift/client-go/config/clientset/versioned/scheme"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/dynamic"
)

const (
	credentialsRequestGroup    = "cloudcredential.openshift.io"
	credentialsRequestVersion  = "v1"
	credentialsRequestResource = "credentialsrequests"
	credentialsRequestKind     = "CredentialsRequest"
	credentialRequestNamespace = "openshift-cloud-credential-operator"
)

var (
	credentialsRequestResourceGVR schema.GroupVersionResource = schema.GroupVersionResource{
		Group:    credentialsRequestGroup,
		Version:  credentialsRequestVersion,
		Resource: credentialsRequestResource,
	}
)

func readCredentialRequestsOrDie(objBytes []byte) *unstructured.Unstructured {
	udi, _, err := scheme.Codecs.UniversalDecoder().Decode(objBytes, nil, &unstructured.Unstructured{})
	if err != nil {
		panic(err)
	}
	return udi.(*unstructured.Unstructured)
}

func applyCredentialsRequest(client dynamic.Interface, recorder events.Recorder, required *unstructured.Unstructured, expectedGeneration int64) (*unstructured.Unstructured, bool, error) {
	if required.GetName() == "" {
		return nil, false, fmt.Errorf("invalid object: name cannot be empty")
	}

	if err := addCredentialsRequestHash(required); err != nil {
		return nil, false, err
	}

	crClient := client.Resource(credentialsRequestResourceGVR).Namespace(required.GetNamespace())
	existing, err := crClient.Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err := crClient.Create(context.TODO(), required, metav1.CreateOptions{})
		if err == nil {
			recorder.Eventf(fmt.Sprintf("%sCreated", required.GetKind()), "Created %s because it was missing", resourcehelper.FormatResourceForCLIWithNamespace(required))
			return actual, true, err
		}
		recorder.Warningf(fmt.Sprintf("%sCreateFailed", required.GetKind()), "Failed to create %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(required), err)
		return nil, false, err
	}
	if err != nil {
		return nil, false, err
	}

	// Check CredentialRequest.Generation.
	needApply := false
	if existing.GetGeneration() != expectedGeneration {
		needApply = true
	}

	// Check specHashAnnotation
	existingAnnotations := existing.GetAnnotations()
	if existingAnnotations == nil || existingAnnotations[specHashAnnotation] != required.GetAnnotations()[specHashAnnotation] {
		needApply = true
	}

	if !needApply {
		return existing, false, nil
	}

	requiredCopy := required.DeepCopy()
	existing.Object["spec"] = requiredCopy.Object["spec"]
	actual, err := crClient.Update(context.TODO(), existing, metav1.UpdateOptions{})
	if err != nil {
		return nil, false, err
	}
	return actual, existing.GetResourceVersion() != actual.GetResourceVersion(), nil
}

func addCredentialsRequestHash(cr *unstructured.Unstructured) error {
	jsonBytes, err := json.Marshal(cr.Object["spec"])
	if err != nil {
		return err
	}
	specHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	annotations := cr.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[specHashAnnotation] = specHash
	cr.SetAnnotations(annotations)
	return nil
}

// getCredentialsRequestFailure finds all true conditions in CredentialsRequest
// and composes an error message from them.
func getCredentialsRequestFailure(cr *unstructured.Unstructured) string {
	// Parse Unstructured CredentialsRequest. Ignore all errors and not found conditions
	// - in the worst case, there is no message why the CredentialsRequest is stuck.
	var msgs []string
	conditions, found, err := unstructured.NestedFieldNoCopy(cr.Object, "status", "conditions")
	if err != nil {
		return ""
	}
	if !found {
		return ""
	}
	conditionArray, ok := conditions.([]interface{})
	if !ok {
		return ""
	}
	for _, c := range conditionArray {
		condition, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		t, found, err := unstructured.NestedString(condition, "type")
		if err != nil {
			continue
		}
		if !found {
			continue
		}
		status, found, err := unstructured.NestedString(condition, "status")
		if err != nil {
			continue
		}
		if !found {
			continue
		}
		message, found, err := unstructured.NestedString(condition, "message")
		if err != nil {
			continue
		}
		if !found {
			continue
		}
		if status == "True" {
			msgs = append(msgs, fmt.Sprintf("%s: %s", t, message))
		}
	}
	if len(msgs) == 0 {
		return ""
	}
	return strings.Join(msgs, ", ")
}
