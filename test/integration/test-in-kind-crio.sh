#!/usr/bin/env bash

set -e

export IMAGE_TAG=v0.7.0
export GITHUB_RUN_ID=1
export NODE_IMAGE=docker.io/warmmetal/kindest-node-crio:v1.25.2-container-image-csi-driver
export VALUE_FILE=$(dirname "${BASH_SOURCE[0]}")/crio-helm-values.yaml

HELM_NAME='wm-csi-integration-tests'

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh

$(dirname "${BASH_SOURCE[0]}")/../../hack/helper/kind_bed.sh 'k8s'
trap "docker rm -f kind-${GITHUB_RUN_ID}-control-plane" ERR EXIT INT TERM

$(dirname "${BASH_SOURCE[0]}")/../../hack/ci/setup_private_registry.sh
trap "docker rm -f private-registry; docker rmi localhost:5000/warmmetal/docker.io/warmmetal/container-image-csi-driver-test:simple-fs" ERR EXIT INT TERM

helm install ${HELM_NAME} $(dirname "${BASH_SOURCE[0]}")/../../charts/warm-metal-csi-driver -n kube-system \
  -f ${VALUE_FILE} \
  --set csiPlugin.image.tag=${IMAGE_TAG} \
  --set csiNodeDriverRegistrar.image.repository=${REGISTRAR_IMAGE} \
  --set csiLivenessProbe.image.repository=${LIVENESSPROBE_IMAGE} \
  --set csiExternalProvisioner.image.repository=${PROVISIONER_IMAGE} \
  --wait

$(dirname "${BASH_SOURCE[0]}")/../../hack/ci/test.sh

helm uninstall -n kube-system ${HELM_NAME} --wait

set +e
