#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::install_driver_from_manifest_file 'https://raw.githubusercontent.com/warm-metal/container-image-csi-driver/v0.4.2/install/cri-containerd.yaml'

TestBase=$(dirname "${BASH_SOURCE[0]}")
kubectl apply -f "${TestBase}/compatible-manifests/ephemeral-volume.yaml"
kubectlwait default compatible-ephemeral-volume

kubectl apply -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"
kubectlwait default compatible-pre-provisioned-pv

kubectl delete --ignore-not-found -f 'https://raw.githubusercontent.com/warm-metal/container-image-csi-driver/v0.4.2/install/cri-containerd.yaml'

export VALUE_FILE=$(dirname "${BASH_SOURCE[0]}")/../../charts/warm-metal-csi-driver/values.yaml
export IMAGE_TAG=$(git rev-parse --short HEAD)
export HELM_NAME="wm-csi-integration-tests"

trap "kubectl -n kube-system describe po" ERR

echo "Install the new verson of the driver using image ${IMAGE_TAG}"
helm install ${HELM_NAME} charts/warm-metal-csi-driver -n kube-system \
  -f ${VALUE_FILE} \
  --set csiPlugin.image.tag=${IMAGE_TAG} \
  --set pullImageSecretForDaemonset=warmmetal \
  --wait \
  --debug

lib::run_test_job "${TestBase}/manifests/ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/readonly-ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/pre-provisioned-pv.yaml"

kubectl delete -f "${TestBase}/compatible-manifests/ephemeral-volume.yaml"
kubectl delete --ignore-not-found -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"

helm uninstall -n kube-system ${HELM_NAME} --wait
set +e
