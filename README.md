# rwox-csi

`rwox-csi` is a Kubernetes 1.31+ proof of concept that presents an arbitrary filesystem-mode RWO StorageClass as an RWX CSI StorageClass. Each logical volume gets an isolated NFS-Ganesha gateway and one backing RWO PVC.

> **PoC warning:** this is not production-ready storage. A gateway is a single data-path process; recovery depends on Kubernetes and the backing CSI driver's detach/reattach behavior. There is no explicit fencing, HA gateway, Kerberos, snapshot/clone support, raw block support, fsGroup handling, or tracing.

## Architecture

```text
RWX PVC -> external-provisioner -> rwox CSI controller -> cluster-scoped RWXVolume
                                                           |
                                                        operator
                                                           |
             +------------------------+--------------------+------------------+
             |                        |                    |                  |
      backing RWO PVC       Ganesha ConfigMap     1-replica StatefulSet  ClusterIP Service
                                                           |
application pod -> CSI node staging NFSv4.1 mount -> service DNS -> Ganesha -> backing filesystem
                  CSI node per-pod bind mount
```

The gateway StatefulSet is created immediately and consumes the backing PVC, so a backing StorageClass using `WaitForFirstConsumer` provisions for the gateway's scheduled topology. The frontend advertises no topology and is reachable from any node that can reach cluster Services.

The CSI controller is deliberately asynchronous: create and expansion write the desired `RWXVolume` state and return a retryable gRPC status until the operator reports the matching generation ready. Deletion removes the CR; its finalizer removes the StatefulSet, Service, ConfigMap, and backing PVC before allowing deletion to finish.

## Supported contract

- CSI `MULTI_NODE_MULTI_WRITER` and filesystem volumes only
- `CreateVolume`, `DeleteVolume`, `ValidateVolumeCapabilities`, and online `ControllerExpandVolume`
- `NodeStageVolume` performs one NFSv4.1 mount per volume/node
- `NodePublishVolume` bind-mounts the staging path and honors a read-only consumer
- `NodeGetVolumeStats` reports filesystem bytes and inodes
- NFSv4.1, TCP, `AUTH_SYS`, and `No_Root_Squash`
- one frontend StorageClass per chart installation; `backingStorageClass` is fixed by that class
- Prometheus metrics on `:8080` and JSON/structured logs
- reclaim policy `Delete` only

## Build and test

```sh
make verify
docker build -t rwox-csi:dev .
```

The single image contains `/csi-controller`, `/csi-node`, and `/operator`; the Helm workload selects the appropriate command.

GitHub Actions runs formatting, vet, tests, binary builds, chart linting, and a multi-architecture container build for every pull request. Pushes to `main` publish `main` and `sha-*` image tags to `ghcr.io/codesphere-cloud/rwox-csi`. A semantic version tag such as `v0.1.0` additionally publishes `0.1.0`, `0.1`, and `latest` image tags plus an OCI Helm chart with the same version.

## Install

For a tagged release, install the OCI chart from GHCR and select the backing RWO StorageClass:

```sh
helm upgrade --install rwox oci://ghcr.io/codesphere-cloud/charts/rwox-csi \
  --version 0.1.0 \
  --namespace rwx-csi-system --create-namespace \
  --set storageClass.backingStorageClass=standard-rwo
```

GHCR packages are private by default until their package visibility is changed. For a private package, run `helm registry login ghcr.io` first and configure `imagePullSecrets` for the cluster.

For local development, install `./chart/rwox-csi` and override `image.repository` and `image.tag`. The NFS-Ganesha image is configurable with `images.ganesha` and must include `sh` and `rpcinfo` for the NFSv4 readiness probe. Nodes must have the NFS client mount helper installed. The chart creates the `rwox` StorageClass by default; edit `storageClass.name`, `mountOptions`, and `backingStorageClass` as needed. Only use `reclaimPolicy: Delete`.

## PoC acceptance

Use a cluster with at least two schedulable worker nodes.

1. Install the chart and confirm controller, operator, and node pods are Ready.
2. Apply `examples/acceptance.yaml`.
3. Confirm the PVC binds and exactly one of each backing object exists:

   ```sh
   kubectl get rwxvolume
   kubectl -n rwx-csi-system get pvc,configmap,statefulset,service -l storage.example.io/volume
   ```

4. Confirm the two acceptance pods run on different nodes, then verify each can see both files:

   ```sh
   kubectl get pod -l app=rwox-acceptance -o wide
   for p in $(kubectl get pod -l app=rwox-acceptance -o name); do kubectl exec "$p" -- ls -l /data; done
   ```

5. Expand `rwox-acceptance` to `2Gi`; wait for both the frontend and backing PVC capacities to update and confirm existing mounts stay usable.
6. Mount the claim read-only in a test pod and confirm reads succeed while writes fail.
7. Delete the acceptance Deployment and PVC. Confirm the `RWXVolume` and all four backing objects disappear.

For a `WaitForFirstConsumer` backing class, also verify the gateway pod is scheduled before the backing claim binds. Snapshot, clone, raw block, alternate CSI access modes, frontend topology constraints, Kerberos, fsGroup, and explicit failover fencing are intentionally rejected or absent.
