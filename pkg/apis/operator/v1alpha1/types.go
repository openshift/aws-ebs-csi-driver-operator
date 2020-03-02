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
	// managementState indicates whether and how the operator should manage the component
	// +optional
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// logLevel is an intent based logging for an overall component.  It does not give fine grained control, but it is a
	// simple way to manage coarse grained logging choices that operators have to interpret for their operands.
	// +optional
	LogLevel operatorv1.LogLevel `json:"logLevel,omitempty"`
}

// EBSCSIDriverStatus is the status for a EBSCSIDriver resource
type EBSCSIDriverStatus struct {
	// ObservedGeneration is the last generation of this object that
	// the operator has acted on.
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// state indicates what the operator has observed to be its current operational status.
	// State operatorv1.ManagementState `json:"managementState,omitempty"`

	// Conditions is a list of conditions and their status.
	Conditions []operatorv1.OperatorCondition `json:"conditions,omitempty"`

	// readyReplicas indicates how many replicas are ready and at the desired state
	ReadyReplicas int32 `json:"readyReplicas"`

	// generations are used to determine when an item needs to be reconciled or has changed in a way that needs a reaction.
	// +optional
	Generations []operatorv1.GenerationStatus `json:"generations,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// EBSCSIDriverList is a list of EBSCSIDriver resources
type EBSCSIDriverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []EBSCSIDriver `json:"items"`
}
