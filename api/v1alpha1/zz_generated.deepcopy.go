// Code generated manually for the PoC. DO NOT EDIT.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func (in *RWXVolume) DeepCopyInto(out *RWXVolume) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec.Capacity = in.Spec.Capacity.DeepCopy()
	if in.Status.Capacity != nil {
		q := in.Status.Capacity.DeepCopy()
		out.Status.Capacity = &q
	}
	if in.Status.Failure != nil {
		f := *in.Status.Failure
		out.Status.Failure = &f
	}
	if in.Status.Conditions != nil {
		out.Status.Conditions = make([]metav1.Condition, len(in.Status.Conditions))
		copy(out.Status.Conditions, in.Status.Conditions)
	}
}
func (in *RWXVolume) DeepCopy() *RWXVolume {
	if in == nil {
		return nil
	}
	out := new(RWXVolume)
	in.DeepCopyInto(out)
	return out
}
func (in *RWXVolume) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
func (in *RWXVolumeList) DeepCopyInto(out *RWXVolumeList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]RWXVolume, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}
func (in *RWXVolumeList) DeepCopy() *RWXVolumeList {
	if in == nil {
		return nil
	}
	out := new(RWXVolumeList)
	in.DeepCopyInto(out)
	return out
}
func (in *RWXVolumeList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
