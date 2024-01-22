[![containerd](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/containerd.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/containerd.yaml)
[![docker-containerd](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/docker-containerd.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/docker-containerd.yaml)
[![cri-o](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/cri-o.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/cri-o.yaml)
![Docker Pulls](https://img.shields.io/docker/pulls/warmmetal/csi-image?color=brightgreen&logo=docker&logoColor=lightgrey&labelColor=black)

# :construction_worker_man: :wrench: :construction: RENAMING THE REPOSITORY :construction: :wrench: :construction_worker_man:

We are currently in the process of [changing the repository name](https://github.com/warm-metal/container-image-csi-driver/issues/105). This alteration may potentially introduce issues during Continuous Integration (CI) runs or while building packages locally. If you encounter any problems, we encourage you to promptly create an issue so that we can assist you in resolving them.

### Note for Forked Repositories:
If you have forked this repository before January 21, 2024, we kindly request that you follow the steps outlined in the [GitHub documentation](https://docs.github.com/en/repositories/creating-and-managing-repositories/renaming-a-repository) to update your remote. This ensures that your fork remains synchronized with the latest changes and avoids any disruption to your workflow.

Also the default branch has been updated to `main` from `master`. Please run below commands for updating your local setup.
```
git branch -m master main
git fetch origin
git branch -u main main
git remote set-head origin -a
```

We appreciate your cooperation and understanding as we work to improve our repository.

# container-image-csi-driver (previously csi-driver-image)

This is a CSI driver for mounting images as PVs or ephemeral volumes.

It pulls images via CRI and shares the image store with the container runtime,
then mounts images via the snapshot/storage service of the runtime.
**Read-Only** volumes of the same image share the same snapshot.
**Read-Write** volumes keep their own snapshot and changes until pod deletion.

- [Installation](#installation)
- [Usage](#usage)
    * [Ephemeral Volume](#ephemeral-volume)
    * [Pre-provisioned PV](#pre-provisioned-pv)
    * [Private Image](#private-image)

## Compatibility matrix
Tested changes on below mentioned versions -

| warm-metal | k8s version | containerd | crio    |
|------------|-------------|------------|---------|
| 0.6.x      | v1.25       | 1.6.8      | v1.20.9 |
| 0.7.x      | v1.25       | 1.6.8      | v1.20.9 |
| 0.8.x      | v1.25       | 1.6.8      | v1.20.9 |
| 1.0.x      | v1.25       | 1.6.8      | v1.25.2 |

#### References:
* containerd [releases](https://containerd.io/releases/#kubernetes-support)
* cri-o [releases](https://github.com/cri-o/cri-o/releases)

## Installation

The driver requires to mount various host paths for different container runtimes.
So, I build a binary utility, `warm-metal-csi-image-install`, to reduce the installation complexity.
It supports kubernetes, microk8s and k3s clusters with container runtime **cri-o**, **containerd** or **docker**.
Users can run this utility on any nodes in their clusters to generate proper manifests to install the driver.
The download link is available on the [release page](https://github.com/warm-metal/container-image-csi-driver/releases).

```shell script
# To print manifests
warm-metal-csi-image-install

# To show the detected configuration
warm-metal-csi-image-install --print-detected-instead

# To change the default namespace that the driver to be installed in
warm-metal-csi-image-install --namespace=foo

# To set a Secret as the imagepullsecret
warm-metal-csi-image-install --pull-image-secret-for-daemonset=foo

# To disable the memroy cache for imagepullsecrets if Secrets are short-lived.
warm-metal-csi-image-install --pull-image-secret-for-daemonset=foo --enable-daemon-image-credential-cache=false
```

You can found some installation manifests as samples in [examples](https://github.com/warm-metal/container-image-csi-driver/tree/master/sample).

#### Notice for docker
Until Docker migrates its [image and snapshot store](https://github.com/moby/moby/issues/38043) to containerd,
I recommend you use containerd instead. Otherwise, the driver can't use images managed by Docker daemon.

If your container runtime can't be migrated, you can enable the CRI plugin by clearing
the containerd config file `/etc/containerd/config.toml`,
then restarting the containerd.

## Usage

Users can mount images as either pre-provisioned PVs or ephemeral volumes.
PVs can only be mounted in access mode **ReadOnlyMany**, while ephemeral volumes will be writable.
Any changes in ephemeral volumes will be discarded after unmounting.

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
            # nodePublishSecretRef:
            #  name: "ImagePullSecret name in the same namespace"
            volumeAttributes:
              image: "docker.io/warmmetal/csi-image-test:simple-fs"
              # # set pullAlways if you want to ignore local images
              # pullAlways: "true"
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
    # nodePublishSecretRef:
    #  name: "name of the ImagePullSecret"
    #  namespace: "namespace of the secret"
    # volumeAttributes:
      # # set pullAlways if you want to ignore local images
      # pullAlways: "true"
```

See all [examples](https://github.com/warm-metal/container-image-csi-driver/tree/master/sample).

#### Private Image

There are several ways to configure credentials for private image pulling.

If your clusters are in cloud, the credential provider are enabled automatically.
If your cloud provider provides a credential provider plugin instead, you can enable it by adding
both `--image-credential-provider-config` and `--image-credential-provider-bin-dir` flags to the driver.
See [credential provider plugin](https://kubernetes.io/docs/tasks/kubelet-credential-provider/kubelet-credential-provider/).

Otherwise, you need ImagePullSecrets to store your credential. The following links may help.
- [https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/).
- [https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#add-imagepullsecrets-to-a-service-account](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#add-imagepullsecrets-to-a-service-account)

If the secret is desired to be shared by all volumes, you can add it to the ServiceAccount of the driver,
`csi-image-warm-metal` by default, and update the Role `csi-image-warm-metal` to make sure the daemon has permissions to fetch the secret,
then restart the driver daemon pod. Users can run `warm-metal-csi-image-install` to generate new manifests and apply them to update.

```shell script
# use secrets foo and bar
warm-metal-csi-image-install --pull-image-secret-for-daemonset=foo --pull-image-secret-for-daemonset=bar
```

If the secret works only for particular workloads, you can  set via the `nodePublishSecretRef` attribute of ephemeral volumes.
See the above sample manifest, and notice that secrets and workloads must in the same namespace.
(Since version v0.5.1, pulling private images using the ImagePullSecrets which attached to workload service accounts is no longer supported for security reasons.)

You can also set the secret to a PV, then share the PV with multiple workloads. See the sample above.

## Tests

### Sanity test

See [test/sanity](https://github.com/warm-metal/container-image-csi-driver/tree/master/test/sanity).

### E2E test

See [test/e2e](https://github.com/warm-metal/container-image-csi-driver/tree/master/test/e2e).

## Note on logging image size
Image sizes are logged after they finish pulling. We've noticed that for smaller images, usually under 1KiB, containerd may report an incorrect image size. An issue has been raised in the containerd github repository: https://github.com/containerd/containerd/issues/9641.

## Community meetings
We conduct online meetings every 1st, 3rd, and 5th week of the month on Thursdays at 15:30 UTC.

Feel free to join us through [this Zoom link](https://acquia.zoom.us/j/94346685583) and refer to the [Google document](https://docs.google.com/document/d/1nDiRtj85ZpWMH57joUmbtGG3aLkfeqFt6OWdkf8_Aaw/edit?usp=sharing) for Minutes of Meetings (MoMs).

Note: If you are unable to attend the meeting but still interested, you can initiate a discussion under Discussions/Queries/Suggestions in the aforementioned Google document.
