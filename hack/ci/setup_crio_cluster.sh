#!/usr/bin/env bash

source $(dirname "$0")/../lib/cluster.sh

set -e
lib::start_cluster_crio $@

echo "Install a private registry"
lib::install_private_registry
minikube ssh -p csi-image-test -- sudo podman pull docker.io/warmmetal/csi-image-test:simple-fs
minikube ssh -p csi-image-test -- sudo podman image tag docker.io/warmmetal/csi-image-test:simple-fs localhost:31000/warmmetal/csi-image-test:simple-fs
minikube ssh -p csi-image-test -- sudo podman push localhost:31000/warmmetal/csi-image-test:simple-fs --tls-verify=false --creds warmmetal:warmmetal
set +e