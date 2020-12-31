# csi-driver-image

This repo contains a CSI driver for mounting images. 
The driver uses snapshot service of container runtime instead of calling CRI interfaces.
So, it doesn't start a container before mounting.

## Install

### Docker

```shell
kubectl apply install/cri-docker.yaml
```

### Containerd

```shell
kubectl apply install/cri-containerd.yaml
```

## Usage

The plugin supports both pre-provisioned PV and ephemeral volumes.
See [examples](https://github.com/warm-metal/csi-driver-image/tree/master/test/manifests).
