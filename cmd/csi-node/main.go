package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	csipb "github.com/container-storage-interface/spec/lib/go/csi"
	rwxcsi "github.com/rwox-csi/rwox-csi/internal/csi"
	"github.com/rwox-csi/rwox-csi/internal/kube"
	"github.com/rwox-csi/rwox-csi/internal/metrics"
	"google.golang.org/grpc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var version = "dev"

func main() {
	endpoint := flag.String("endpoint", "unix:///csi/csi.sock", "CSI endpoint")
	nodeID := flag.String("node-id", os.Getenv("NODE_NAME"), "Kubernetes node name")
	metricsAddress := flag.String("metrics-address", ":8080", "Prometheus metrics address")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k8s, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: kube.Scheme()})
	if err != nil {
		logger.Error("create Kubernetes client", "error", err)
		os.Exit(1)
	}
	go func() {
		if err := metrics.Serve(ctx, *metricsAddress); err != nil {
			logger.Error("metrics server stopped", "error", err)
			cancel()
		}
	}()
	identity := &rwxcsi.IdentityServer{Name: "rwox.storage.example.io", Version: version}
	node := &rwxcsi.NodeServer{Client: k8s, NodeID: *nodeID}
	logger.Info("starting CSI node service", "endpoint", *endpoint, "node_id", *nodeID, "version", version)
	if err := rwxcsi.Serve(*endpoint, func(s *grpc.Server) { csipb.RegisterIdentityServer(s, identity); csipb.RegisterNodeServer(s, node) }); err != nil {
		logger.Error("CSI server stopped", "error", err)
		os.Exit(1)
	}
}
