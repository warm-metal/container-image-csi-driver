#!/usr/bin/env bash

set -e
set -x

source $(dirname "${BASH_SOURCE[0]}")/../lib/cluster.sh

echo "Testing on containerd"
lib::start_cluster_containerd ${K8S_VERSION}
lib::install_driver

echo "Install a private registry"
lib::install_private_registry
minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i pull docker.io/warmmetal/csi-image-test:simple-fs
minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i tag --force docker.io/warmmetal/csi-image-test:simple-fs localhost:31000/warmmetal/csi-image-test:simple-fs
minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i push localhost:31000/warmmetal/csi-image-test:simple-fs --plain-http --user warmmetal:warmmetal

set +x
set +e