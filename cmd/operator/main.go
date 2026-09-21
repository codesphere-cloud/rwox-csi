package main

import (
	"flag"
	"os"

	"github.com/rwox-csi/rwox-csi/internal/kube"
	operatorcontroller "github.com/rwox-csi/rwox-csi/internal/operator"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	var metricsAddress, probeAddress, namespace, clusterDomain, image string
	var leaderElect bool
	flag.StringVar(&metricsAddress, "metrics-address", ":8080", "Prometheus metrics address")
	flag.StringVar(&probeAddress, "health-probe-address", ":8081", "health probe address")
	flag.StringVar(&namespace, "namespace", "rwx-csi-system", "gateway resource namespace")
	flag.StringVar(&clusterDomain, "cluster-domain", "cluster.local", "Kubernetes cluster DNS domain")
	flag.StringVar(&image, "ganesha-image", "quay.io/nfs-ganesha/nfs-ganesha:latest", "NFS-Ganesha image")
	flag.BoolVar(&leaderElect, "leader-elect", true, "enable leader election")
	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: kube.Scheme(), Metrics: metricsserver.Options{BindAddress: metricsAddress}, HealthProbeBindAddress: probeAddress, LeaderElection: leaderElect, LeaderElectionID: "rwox-csi-operator.storage.example.io"})
	if err != nil {
		ctrl.Log.Error(err, "create manager")
		os.Exit(1)
	}
	r := &operatorcontroller.Reconciler{Client: mgr.GetClient(), Namespace: namespace, ClusterDomain: clusterDomain, GaneshaImage: image}
	if err := r.SetupWithManager(mgr); err != nil {
		ctrl.Log.Error(err, "create controller")
		os.Exit(1)
	}
	_ = mgr.AddHealthzCheck("healthz", healthz.Ping)
	_ = mgr.AddReadyzCheck("readyz", healthz.Ping)
	ctrl.Log.Info("starting operator", "namespace", namespace)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		ctrl.Log.Error(err, "manager stopped")
		os.Exit(1)
	}
}
