#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::start_cluster_crio ${K8S_VERSION}

minikube -p csi-image-test node add
minikube cache reload -p csi-image-test
lib::install_driver_for_crio

minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo mkdir -p /mnt/vda1/var/lib/boot2docker/etc/crio
minikube ssh -p csi-image-test -n csi-image-test-m02 -- sudo cp -rp /etc/crio/* /var/lib/boot2docker/etc/crio/

source $(dirname "${BASH_SOURCE[0]}")/restart-runtime.sh 0

lib::uninstall_driver_for_crio
echo "Destroying cluster"
minikube delete -p csi-image-test
set +e