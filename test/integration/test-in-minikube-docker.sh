#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh

echo "Testing on docker"
lib::start_cluster_docker

set -e
lib::install_driver

echo "Install a private registry"
lib::install_private_registry
minikube ssh -p docker.io/warmmetal/container-image-csi-driver-test -- sudo ctr -n k8s.io i pull docker.io/warmmetal/container-image-csi-driver-test:simple-fs
minikube ssh -p docker.io/warmmetal/container-image-csi-driver-test -- sudo ctr -n k8s.io i tag --force docker.io/warmmetal/container-image-csi-driver-test:simple-fs localhost:31000/warmmetal/docker.io/warmmetal/container-image-csi-driver-test:simple-fs
minikube ssh -p docker.io/warmmetal/container-image-csi-driver-test -- sudo ctr -n k8s.io i push localhost:31000/warmmetal/docker.io/warmmetal/container-image-csi-driver-test:simple-fs --plain-http --user warmmetal:warmmetal

source $(dirname "${BASH_SOURCE[0]}")/cases.sh

lib::uninstall_driver
echo "Destroying cluster"
minikube delete -p docker.io/warmmetal/container-image-csi-driver-test
set +e
