package v1alpha1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSEBSDriver is a specification for a AWSEBSDriver resource
type AWSEBSDriver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AWSEBSDriverSpec   `json:"spec"`
	Status AWSEBSDriverStatus `json:"status"`
}

// AWSEBSDriverSpec is the spec for a AWSEBSDriver resource
type AWSEBSDriverSpec struct {
	operatorv1.OperatorSpec `json:",inline"`
}

// AWSEBSDriverStatus is the status for a AWSEBSDriver resource
type AWSEBSDriverStatus struct {
	operatorv1.OperatorStatus `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AWSEBSDriverList is a list of AWSEBSDriver resources
type AWSEBSDriverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []AWSEBSDriver `json:"items"`
}
