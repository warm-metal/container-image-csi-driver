#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/utils.sh

TestBase=$(dirname "${BASH_SOURCE[0]}")

set -e
export REGISTRY_USERNAME=warmmetal
export REGISTRY_PASSWORD=warmmetal

echo "Install private secret and SA"
kubectl create secret docker-registry warmmetal \
  --docker-server=http://localhost:31000/ \
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

echo "Attatch secret to the daemon SA"
kubectl -n kube-system create secret docker-registry warmmetal \
  --docker-server=http://localhost:31000/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}"
kubectl -n kube-system patch sa csi-image-warm-metal -p '{"imagePullSecrets": [{"name": "warmmetal"}]}'
kubectl -n kube-system delete po $(kubectl get po -n kube-system -o=custom-columns=:metadata.name --no-headers -l=app=csi-image-warm-metal)
kubectlwait kube-system -l=app=csi-image-warm-metal

for i in ${TestBase}/daemon-dependent-manifests/*.yaml; do
  echo "start job $(basename $i)"
  lib::run_test_job $i
done

echo "Testing Done!"
set +e