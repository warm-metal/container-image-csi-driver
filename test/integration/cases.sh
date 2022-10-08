#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/cluster.sh

TestBase=$(dirname "${BASH_SOURCE[0]}")

set -e
export REGISTRY_USERNAME=warmmetal
export REGISTRY_PASSWORD=warmmetal

echo "Install private secret and SA"
kubectl create secret docker-registry warmmetal \
  --docker-server=http://private-registry:5000/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}"
kubectl -n kube-system create secret docker-registry warmmetal \
  --docker-server=http://private-registry:5000/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}"
kubectl create sa warmmetal
kubectl patch sa warmmetal -p '{"imagePullSecrets": [{"name": "warmmetal"}]}'

for i in ${TestBase}/manifests/*.yaml; do
  lib::run_test_job $i
done

for i in ${TestBase}/failed-manifests/*.yaml; do
  lib::run_failed_test_job $i
done

echo "Install secret for daemon"
lib::install_driver "${IMG}" "warmmetal"

for i in ${TestBase}/daemon-dependent-manifests/*.yaml; do
  echo "start job $(basename $i)"
  lib::run_test_job $i
done

echo "Testing Done!"
set +e