package v1alpha1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Driver is a specification for a Driver resource
type Driver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DriverSpec   `json:"spec"`
	Status DriverStatus `json:"status"`
}

// DriverSpec is the spec for a Driver resource
type DriverSpec struct {
	operatorv1.OperatorSpec `json:",inline"`
}

// DriverStatus is the status for a Driver resource
type DriverStatus struct {
	operatorv1.OperatorStatus `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DriverList is a list of Driver resources
type DriverList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Driver `json:"items"`
}
