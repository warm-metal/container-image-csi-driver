# csi-driver-image

This is a CSI driver for mounting images as PVs or ephemeral volumes.

Our first aim is resource-saving. The driver shares the image store with the container runtime.
It uses CRI to pull images, then mounts them via the snapshot service of the runtime.  
Every **read-only** volume of the same image will share the same snapshot.
It doesn't duplicate any images or containers already exist in the runtime.

## Installation

The manifest below installs a CSIDriver `csi-image.warm-metal.tech` and a DaemonSet.

```shell script
kubectl apply -f https://raw.githubusercontent.com/warm-metal/csi-driver-image/master/install/cri-containerd.yaml
```

The driver currently supports only **containerd** and **docker** with CRI enabled.

Until Docker migrates its [image and snapshot store](https://github.com/moby/moby/issues/38043) to containerd,
I recommend you use containerd instead. Otherwise, the driver can't use images managed by Docker daemon.

If your container runtime can't be migrated, you can enable the CRI plugin by clearing the containerd config file `/etc/containerd/config.toml`, then restarting the containerd.

#### Cluster with custom configuration

For clusters installed with custom configurations, say microk8s,
the provided manifests are also available after modifying some hostpaths. See below.

In the `volumes` section of the manifest, 
1. Replace `/var/lib/kubelet` with `root-dir` of kubelet,
2. Replace `/run/containerd/containerd.sock` with your containerd socket path.

A tested manifest for microk8s clusters is available [here](https://raw.githubusercontent.com/warm-metal/csi-driver-image/master/install/cri-containerd-microk8s.yaml).

## Usage

Users can mount images as either pre-provisioned PVs or ephemeral volumes.
PVs can only be mounted in access mode **ReadOnlyMany**, while ephemeral volumes will be writable.
Any changes in ephemeral volumes will be discarded after unmounting.

Private images are also supported.
Besides the credential provider, **pullImageSecret settings** in both workload manifests and the driver DaemonSet manifest are also
used to pull private images(See [#16](https://github.com/warm-metal/csi-driver-image/issues/16)). 
Users can add the secret name to workload ServiceAccounts or the driver SA `csi-image-warm-metal`.
If `csi-image-warm-metal` is chosen, the secret will be activated after restarting the driver pod.

#### Ephemeral Volume
For ephemeral volumes, `volumeAttributes` contains **image**(required), **secret**, **secretNamespace**, and **pullAlways**.

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
              # # set pullAlways if you want to ignore local images
              # pullAlways: "true"
              # # set secret if the image is private
              # secret: "name of the ImagePullSecret"
              # secretNamespace: "namespace of the secret"
  backoffLimit: 0
```

#### Pre-provisioned PV
For pre-provisioned PVs, `volumeHandle` instead of the attribute **image**, specify the target image.

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
      # # set pullAlways if you want to ignore local images
      # pullAlways: "true"
      # # set secret if the image is private
      # secret: "name of the ImagePullSecret"
      # secretNamespace: "namespace of the secret"
```

See all [examples](https://github.com/warm-metal/csi-driver-image/tree/master/sample).
