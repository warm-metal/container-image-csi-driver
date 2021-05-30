#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::install_driver_from_manifest_file 'https://raw.githubusercontent.com/warm-metal/csi-driver-image/v0.4.2/install/cri-containerd.yaml'

TestBase=$(dirname "${BASH_SOURCE[0]}")
kubectl apply -f "${TestBase}/compatible-manifests/ephemeral-volume.yaml"
kubectlwait default compatible-ephemeral-volume

kubectl apply -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"
kubectlwait default compatible-pre-provisioned-pv

IMG=${IMG:-warmmetal/csi-image:$(git rev-parse --short HEAD)}
echo "Install the new verson of the driver using image ${IMG}"
lib::install_driver "${IMG}"

lib::run_test_job "${TestBase}/manifests/ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/readonly-ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/pre-provisioned-pv.yaml"

kubectl delete -f "${TestBase}/compatible-manifests/ephemeral-volume.yaml"
kubectl delete --ignore-not-found -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"

lib::uninstall_driver
set +e