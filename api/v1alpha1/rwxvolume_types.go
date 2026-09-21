package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	FinalizerName            = "storage.example.io/cleanup"
	ConditionReady           = "Ready"
	ConditionBackingPVCReady = "BackingPVCReady"
	ConditionGatewayReady    = "GatewayReady"
	ConditionExportReady     = "ExportReady"
)

type RWXVolumeSpec struct {
	Capacity resource.Quantity `json:"capacity"`
	Backing  BackingSpec       `json:"backing"`
	NFS      NFSSpec           `json:"nfs,omitempty"`
}

type BackingSpec struct {
	StorageClassName string `json:"storageClassName"`
}

type NFSSpec struct {
	Version    string `json:"version,omitempty"`
	ExportPath string `json:"exportPath,omitempty"`
}

type ObjectReference struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

type GatewayStatus struct {
	ServiceName     string `json:"serviceName,omitempty"`
	DNSName         string `json:"dnsName,omitempty"`
	StatefulSetName string `json:"statefulSetName,omitempty"`
}

type FailureStatus struct {
	Class   string `json:"class,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type RWXVolumeStatus struct {
	Phase              string             `json:"phase,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	BackingPVC         ObjectReference    `json:"backingPVC,omitempty"`
	Gateway            GatewayStatus      `json:"gateway,omitempty"`
	Capacity           *resource.Quantity `json:"capacity,omitempty"`
	Failure            *FailureStatus     `json:"failure,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=rwxv
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Capacity",type=string,JSONPath=`.status.capacity`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type RWXVolume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              RWXVolumeSpec   `json:"spec,omitempty"`
	Status            RWXVolumeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type RWXVolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RWXVolume `json:"items"`
}

func (v *RWXVolume) Ready() bool {
	for _, c := range v.Status.Conditions {
		if c.Type == ConditionReady {
			return c.Status == metav1.ConditionTrue && c.ObservedGeneration == v.Generation
		}
	}
	return false
}
