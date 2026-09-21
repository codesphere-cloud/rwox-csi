package kube

import (
	storagev1alpha1 "github.com/rwox-csi/rwox-csi/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func Scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(s)
	_ = storagev1alpha1.AddToScheme(s)
	return s
}
