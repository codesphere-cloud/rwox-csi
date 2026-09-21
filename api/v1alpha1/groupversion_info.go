// Package v1alpha1 contains the storage.example.io API.
// +kubebuilder:object:generate=true
// +groupName=storage.example.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "storage.example.io", Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)

func init() { SchemeBuilder.Register(&RWXVolume{}, &RWXVolumeList{}) }

var _ runtime.Object = &RWXVolume{}
