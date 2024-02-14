#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::start_cluster_containerd ${K8S_VERSION}
minikube cache reload -p container-image-csi-driver-test

source $(dirname "${BASH_SOURCE[0]}")/backward-compatability.sh

echo "Destroying cluster"
minikube delete -p container-image-csi-driver-test
set +e
