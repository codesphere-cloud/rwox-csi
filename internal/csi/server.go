package csi

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/grpc"
)

func Serve(endpoint string, register func(*grpc.Server)) error {
	path := strings.TrimPrefix(endpoint, "unix://")
	if path == endpoint {
		return fmt.Errorf("only unix:// endpoints are supported")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	lis, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer lis.Close()
	s := grpc.NewServer(grpc.UnaryInterceptor(UnaryMetrics))
	register(s)
	return s.Serve(lis)
}
