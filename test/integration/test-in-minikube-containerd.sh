#!/usr/bin/env bash

set -e

source $(dirname "${BASH_SOURCE[0]}")/../../hack/helper/prepare_containerd_cluster.sh
source $(dirname "${BASH_SOURCE[0]}")/cases.sh
lib::uninstall_driver
echo "Destroying cluster"
minikube delete -p csi-image-test

set +e