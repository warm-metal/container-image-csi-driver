#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::start_cluster_containerd ${K8S_VERSION}
minikube node -p csi-image-test add
minikube cache reload -p csi-image-test
lib::install_driver

minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo mkdir -p /mnt/vda1/var/lib/boot2docker/etc/containerd
minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo cp -rp /etc/containerd/* /var/lib/boot2docker/etc/containerd/

source $(dirname "${BASH_SOURCE[0]}")/restart-runtime.sh 1

lib::uninstall_driver
echo "Destroying cluster"
minikube delete -p csi-image-test
set +e