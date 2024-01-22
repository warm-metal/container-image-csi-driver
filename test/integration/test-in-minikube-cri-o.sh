#!/usr/bin/env bash

set -e

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh

echo "Testing on containerd"
lib::start_cluster_crio ${K8S_VERSION}
lib::install_driver

echo "Install a private registry"
lib::install_private_registry
minikube ssh -p docker.io/warmmetal/container-image-csi-driver-test -- sudo podman pull docker.io/warmmetal/container-image-csi-driver-test:simple-fs
minikube ssh -p docker.io/warmmetal/container-image-csi-driver-test -- sudo podman image tag docker.io/warmmetal/container-image-csi-driver-test:simple-fs localhost:31000/warmmetal/docker.io/warmmetal/container-image-csi-driver-test:simple-fs
minikube ssh -p docker.io/warmmetal/container-image-csi-driver-test -- sudo podman push localhost:31000/warmmetal/docker.io/warmmetal/container-image-csi-driver-test:simple-fs --tls-verify=false --creds warmmetal:warmmetal

source $(dirname "${BASH_SOURCE[0]}")/cases.sh

lib::uninstall_driver
echo "Destroying cluster"
minikube delete -p docker.io/warmmetal/container-image-csi-driver-test
set +e
