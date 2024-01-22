#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::start_cluster_crio ${K8S_VERSION}
minikube cache reload -p docker.io/warmmetal/container-image-csi-driver-test
lib::install_driver

source $(dirname "${BASH_SOURCE[0]}")/restart-ds.sh

lib::uninstall_driver
echo "Destroying cluster"
minikube delete -p docker.io/warmmetal/container-image-csi-driver-test
set +e
