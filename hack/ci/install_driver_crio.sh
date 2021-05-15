#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -x
set -e

IMG=docker.io/warmmetal/csi-image:$(git rev-parse --short HEAD)
minikube image -p csi-image-test load ${IMG}
lib::install_driver_for_crio ${IMG}

set +e
set +x