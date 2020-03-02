package v1alpha1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// EBSCSIDriver is a specification for a EBSCSIDriver resource
type EBSCSIDriver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EBSCSIDriverSpec   `json:"spec"`
	Status EBSCSIDriverStatus `json:"status"`
}

// EBSCSIDriverSpec is the spec for a EBSCSIDriver resource
type EBSCSIDriverSpec struct {
	operatorv1.OperatorSpec `json:",inline"`
}

// EBSCSIDriverStatus is the status for a EBSCSIDriver resource
type EBSCSIDriverStatus struct {
	operatorv1.OperatorStatus `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// EBSCSIDriverList is a list of EBSCSIDriver resources
type EBSCSIDriverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []EBSCSIDriver `json:"items"`
}
