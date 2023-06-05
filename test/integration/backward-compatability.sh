#!/usr/bin/env bash

set -e
source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh
lib::install_driver_from_manifest_file 'https://raw.githubusercontent.com/warm-metal/csi-driver-image/v0.4.2/install/cri-containerd.yaml'

TestBase=$(dirname "${BASH_SOURCE[0]}")
kubectl apply -f "${TestBase}/compatible-manifests/ephemeral-volume.yaml"
kubectlwait default compatible-ephemeral-volume

kubectl apply -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"
kubectlwait default compatible-pre-provisioned-pv

export VALUE_FILE=$(dirname "${BASH_SOURCE[0]}")/../../charts/warm-metal-csi-driver/values.yaml
export IMAGE_TAG=$(git rev-parse --short HEAD)
export HELM_NAME="wm-csi-integration-tests"

echo "Install the new verson of the driver using image ${IMG}"
helm install ${HELM_NAME} charts/warm-metal-csi-driver -n kube-system \
  -f ${VALUE_FILE} \
  --set csiPlugin.image.tag=${IMAGE_TAG} \
  --set csiNodeDriverRegistrar.image.repository=${REGISTRAR_IMAGE} \
  --set csiLivenessProbe.image.repository=${LIVENESSPROBE_IMAGE} \
  --set csiExternalProvisioner.image.repository=${PROVISIONER_IMAGE} \
  --set pullImageSecretForDaemonset=warmmetal \
  --wait

lib::run_test_job "${TestBase}/manifests/ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/readonly-ephemeral-volume.yaml"
lib::run_test_job "${TestBase}/manifests/pre-provisioned-pv.yaml"

kubectl delete -f "${TestBase}/compatible-manifests/ephemeral-volume.yaml"
kubectl delete --ignore-not-found -f "${TestBase}/compatible-manifests/pre-provisioned-pv.yaml"

helm uninstall -n kube-system ${HELM_NAME} --wait
set +e