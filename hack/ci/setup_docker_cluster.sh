#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

lib::start_cluster_docker $@

set -e
echo "Install a private registry"
lib::install_private_registry
minikube ssh -p container-image-csi-driver-test -- sudo ctr -n k8s.io i pull docker.io/warmmetal/container-image-csi-driver-test:simple-fs
minikube ssh -p container-image-csi-driver-test -- sudo ctr -n k8s.io i tag --force docker.io/warmmetal/container-image-csi-driver-test:simple-fs localhost:31000/warmmetal/container-image-csi-driver-test:simple-fs
minikube ssh -p container-image-csi-driver-test -- sudo ctr -n k8s.io i push localhost:31000/warmmetal/container-image-csi-driver-test:simple-fs --plain-http --user warmmetal:warmmetal
set +e
