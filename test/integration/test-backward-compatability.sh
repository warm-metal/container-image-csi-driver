#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::start_cluster_containerd ${K8S_VERSION}
minikube cache reload -p csi-image-test

source $(dirname "${BASH_SOURCE[0]}")/backward-compatability.sh

echo "Destroying cluster"
minikube delete -p csi-image-test
set +e