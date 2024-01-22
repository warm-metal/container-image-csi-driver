#!/usr/bin/env bash

set -e

export K8S_VERSION=v1.25.2
source $(dirname "${BASH_SOURCE[0]}")/../../hack/helper/prepare_containerd_cluster.sh
source $(dirname "${BASH_SOURCE[0]}")/cases.sh
lib::uninstall_driver
echo "Destroying cluster"
minikube delete -p docker.io/warmmetal/container-image-csi-driver-test

set +e
