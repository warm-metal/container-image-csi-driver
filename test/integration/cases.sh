#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh

TestBase=$(dirname "${BASH_SOURCE[0]}")
IMAGE_TAG=${IMAGE_TAG:=$(git rev-parse --short HEAD)}

set -e
export REGISTRY_USERNAME=warmmetal
export REGISTRY_PASSWORD=warmmetal

export REGISTRY_USERNAME2=warmmetal2
export REGISTRY_PASSWORD2=warmmetal2

echo "Install private secret and SA"
kubectl create secret docker-registry warmmetal \
  --docker-server=http://private-registry:5000/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}"
kubectl -n kube-system create secret docker-registry warmmetal \
  --docker-server=http://private-registry:5000/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}"

kubectl create secret docker-registry warmmetal2 \
  --docker-server=http://private-registry:5000/ \
  --docker-username=${REGISTRY_USERNAME2} \
  --docker-password="${REGISTRY_PASSWORD2}"
kubectl -n kube-system create secret docker-registry warmmetal2 \
  --docker-server=http://private-registry:5000/ \
  --docker-username=${REGISTRY_USERNAME2} \
  --docker-password="${REGISTRY_PASSWORD2}"

kubectl create sa warmmetal
kubectl patch sa warmmetal -p '{"imagePullSecrets": [{"name": "warmmetal"}]}'

for i in ${TestBase}/manifests/*.yaml; do
  lib::run_test_job $i
done

for i in ${TestBase}/failed-manifests/*.yaml; do
  lib::run_failed_test_job $i
done

trap "kubectl -n kube-system describe po" ERR

echo "Install secret for daemon and enable secret cache"

helm uninstall -n kube-system ${HELM_NAME} --wait
helm install ${HELM_NAME} charts/warm-metal-csi-driver -n kube-system \
  -f ${VALUE_FILE} \
  --set csiPlugin.image.tag=${IMAGE_TAG} \
  --set pullImageSecretForDaemonset=warmmetal \
  --wait

for i in ${TestBase}/daemon-dependent-manifests/*.yaml; do
  echo "start job $(basename $i)"
  lib::run_test_job $i
done

echo "Install secret for daemon and disable secret cache"

helm uninstall -n kube-system ${HELM_NAME} --wait
helm install ${HELM_NAME} charts/warm-metal-csi-driver -n kube-system \
  -f ${VALUE_FILE} \
  --set csiPlugin.image.tag=${IMAGE_TAG} \
  --set pullImageSecretForDaemonset=warmmetal \
  --set enableDaemonImageCredentialCache=true \
  --wait

for i in ${TestBase}/daemon-dependent-manifests/*.yaml; do
  echo "start job $(basename $i)"
  lib::run_test_job $i
done

echo "Use image pull secret warmmetal2"
kubectl patch sa warmmetal -p '{"imagePullSecrets": [{"name": "warmmetal2"}]}'
kubectl -n kube-system delete secret warmmetal

for i in ${TestBase}/daemon-dependent-manifests/*.yaml; do
  echo "start job $(basename $i)"
  lib::run_test_job $i
done

echo "Test metrics"
./test-metrics.sh

echo "Testing Done!"
set +e