package v1alpha1

import (
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
	DeploymentName string `json:"deploymentName"`
	Replicas       *int32 `json:"replicas"`
}

// EBSCSIDriverStatus is the status for a EBSCSIDriver resource
type EBSCSIDriverStatus struct {
	AvailableReplicas int32 `json:"availableReplicas"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// EBSCSIDriverList is a list of EBSCSIDriver resources
type EBSCSIDriverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []EBSCSIDriver `json:"items"`
}
