#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh

TestBase=$(dirname "${BASH_SOURCE[0]}")

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

echo "Install secret for daemon and enable secret cache"
IMG=docker.io/warmmetal/csi-image:$(git rev-parse --short HEAD)
lib::install_driver "${IMG}" "warmmetal"

for i in ${TestBase}/daemon-dependent-manifests/*.yaml; do
  echo "start job $(basename $i)"
  lib::run_test_job $i
done

echo "Install secret for daemon and disable secret cache"
lib::install_driver "${IMG}" "warmmetal" "disable"

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

echo "Testing Done!"
set +e