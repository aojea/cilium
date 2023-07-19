package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// IPRouteSpec defines the desired state of IPRoute.
type IPRouteSpec struct {
	gatewayv1beta1.CommonRouteSpec `json:",inline"`

	// Network defines Pods network interface the prefixes will be attracted to.
	// If not specified, we will use the Primary Network of the Pod.
	//
	// +optional
	Network *string `json:"network,omitempty"`

	// PodSelector defines to which Pod the prefixes will be attracted to.
	// When selecting multiple, using the newest Pod that is Ready.
	// Empty selector is not allowed.
	// +kubebuilder:validation:Required
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// Prefixes hold a list of all the CIDRs to attract.
	//
	// +kubebuilder:validation:MinItems=1
	Addresses []gatewayv1beta1.GatewayAddress `json:"addresses"`
}

// IPRouteStatus defines the observed state of IPRoute.
type IPRouteStatus struct {
	// Pod holds the name of the Pod the PodSelector specifies.
	// If PodSelector returns multiple items, only the first one is used.
	//
	// +optional
	Pods []string `json:"pods,omitempty"`

	// Conditions describe the current conditions of the IPRoute.
	//
	// Known condition types are:
	//
	// * "Accepted"
	// * "Ready"
	// * "DPv2Ready"
	// * "DPv2Removed"
	//
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// IPRoute is the Schema for the iproutes API
type IPRoute struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPRouteSpec   `json:"spec,omitempty"`
	Status IPRouteStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IPRouteList contains a list of IPRoute
type IPRouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPRoute `json:"items"`
}
