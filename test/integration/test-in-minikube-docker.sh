#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh

echo "Testing on docker"
lib::start_cluster_docker

set -e
lib::install_driver_for_containerd

echo "Install a private registry"
lib::install_private_registry
minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i pull docker.io/warmmetal/csi-image-test:simple-fs
minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i tag --force docker.io/warmmetal/csi-image-test:simple-fs localhost:31000/warmmetal/csi-image-test:simple-fs
minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i push localhost:31000/warmmetal/csi-image-test:simple-fs --plain-http --user warmmetal:warmmetal

source $(dirname "${BASH_SOURCE[0]}")/cases.sh

lib::uninstall_driver_for_containerd
echo "Destroying cluster"
minikube delete -p csi-image-test
set +e