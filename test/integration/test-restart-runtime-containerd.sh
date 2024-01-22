#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::start_cluster_containerd ${K8S_VERSION}
minikube node -p docker.io/warmmetal/container-image-csi-driver-test add
minikube cache reload -p docker.io/warmmetal/container-image-csi-driver-test
lib::install_driver

minikube ssh -p docker.io/warmmetal/container-image-csi-driver-test -n docker.io/warmmetal/container-image-csi-driver-test-m02 -- sudo mkdir -p /mnt/vda1/var/lib/boot2docker/etc/containerd
minikube ssh -p docker.io/warmmetal/container-image-csi-driver-test -n docker.io/warmmetal/container-image-csi-driver-test-m02 -- sudo cp -rp /etc/containerd/* /var/lib/boot2docker/etc/containerd/

source $(dirname "${BASH_SOURCE[0]}")/restart-runtime.sh 1

lib::uninstall_driver
echo "Destroying cluster"
minikube delete -p docker.io/warmmetal/container-image-csi-driver-test
set +e
