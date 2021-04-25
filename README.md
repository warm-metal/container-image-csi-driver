# csi-driver-image

This is a CSI driver for mounting images as PVs or ephemeral volumes.

Our first aim is resource-saving. The driver shares the image store with the container runtime.
It uses CRI to pull images, then mounts them via the snapshot service of the runtime.  
Every **read-only** volume of the same image will share the same snapshot.
It doesn't duplicate any images or containers already exist in the runtime.

What we can do further may be building images through the snapshot capability of CSI.
This allows users to quickly ship images after making minor changes.

## Installation

It currently supports only **containerd** and **docker** with CRI enabled.

Until Docker migrates its [image and snapshot store](https://github.com/moby/moby/issues/38043) to containerd,
I recommend you use containerd instead. Or, the driver can't use images managed by Docker daemon.

If your container runtime can't be migrated, you can enable the CRI plugin by clearing the containerd config file `/etc/containerd/config.toml`, then restarting the containerd. 

```shell script
kubectl apply -f https://raw.githubusercontent.com/warm-metal/csi-driver-image/master/install/cri-containerd.yaml
```

### Cluster with custom configuration

For clusters installed with custom configurations, say microk8s,
the provided manifests are also available after modifying some hostpaths.

In the `volumes` section of the manifest, 
1. Replace `/var/lib/kubelet` with `root-dir` of kubelet,
2. Replace `/run/containerd/containerd.sock` with your containerd socket path.

```yaml
      ...
      volumes:
        - hostPath:
            path: /var/lib/kubelet/plugins/csi-image.warm-metal.tech
            type: DirectoryOrCreate
          name: socket-dir
        - hostPath:
            path: /var/lib/kubelet/pods
            type: DirectoryOrCreate
          name: mountpoint-dir
        - hostPath:
            path: /var/lib/kubelet/plugins_registry
            type: Directory
          name: registration-dir
        - hostPath:
            path: /
            type: Directory
            name: host-rootfs
        - hostPath:
            path: /run/containerd/containerd.sock
            type: Socket
          name: runtime-socket
```

## Usage

Provided manifests will install a CSIDriver `csi-image.warm-metal.tech` and a DaemonSet.
You can mount images as either pre-provisioned PVs or ephemeral volumes.

As a ephemeral volume, `volumeAttributes` are **image**(required), **secret**, and **pullAlways**.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: ephemeral-volume
spec:
  template:
    metadata:
      name: ephemeral-volume
    spec:
      containers:
        - name: ephemeral-volume
          image: docker.io/warmmetal/csi-image-test:check-fs
          env:
            - name: TARGET
              value: /target
          volumeMounts:
            - mountPath: /target
              name: target
      restartPolicy: Never
      volumes:
        - name: target
          csi:
            driver: csi-image.warm-metal.tech
            volumeAttributes:
              image: "docker.io/warmmetal/csi-image-test:simple-fs"
              # set pullAlways if you want to ignore local images
              # pullAlways: "true"
              # set secret if the image is private
              # secret: "pull image secret name"
  backoffLimit: 0
```

For a PV, the `volumeHandle` instead the attribute **image**, specify the target image.

```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-test-csi-image-test-simple-fs
spec:
  storageClassName: csi-image.warm-metal.tech
  capacity:
    storage: 5Gi
  accessModes:
    - ReadOnlyMany
  persistentVolumeReclaimPolicy: Retain
  csi:
    driver: csi-image.warm-metal.tech
    volumeHandle: "docker.io/warmmetal/csi-image-test:simple-fs"
    # volumeAttributes:
      # set pullAlways if you want to ignore local images
      # pullAlways: "true"
      # set secret if the image is private
      # secret: "pull image secret name"
```

See all [examples](https://github.com/warm-metal/csi-driver-image/tree/master/test/integration/manifests).
