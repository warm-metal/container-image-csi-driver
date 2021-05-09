#!/usr/bin/env bash

set -e
set -x

source $(dirname "$0")/utils.sh

K8S_VERSION=${K8S_VERSION:-stable}

echo "Testing on containerd"
minikube start -p csi-image-test --kubernetes-version=${K8S_VERSION} --container-runtime=containerd --insecure-registry=localhost:31000

echo "Install a private registry"
export REGISTRY_USERNAME=warmmetal
export REGISTRY_PASSWORD=warmmetal
installPrivateRegistry

echo "Installing csi-driver-image"
kubectl delete --ignore-not-found -f install/cri-containerd.yaml
kubectl apply  -f install/cri-containerd.yaml
kubectlwait kube-system -l=app=csi-image-warm-metal

echo "Install private secret and SA"
kubectl create secret docker-registry warmmetal \
  --docker-server=http://localhost:31000/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}"
kubectl create sa warmmetal
kubectl patch sa warmmetal -p '{"imagePullSecrets": [{"name": "warmmetal"}]}'

TestBase=$(dirname "$0")
echo "Run Jobs of ephemeral volume testing"
runTestJob ephemeral-volume ${TestBase}/manifests/ephemeral-volume.yaml
runTestJob ephemeral-volume-private-with-given-secret ${TestBase}/manifests/ephemeral-volume-private-with-given-secret.yaml
runTestJob ephemeral-volume-private-with-embedded-secret ${TestBase}/manifests/ephemeral-volume-private-with-embedded-secret.yaml

echo "Run Jobs of pre-provisioined volumetesting"
runTestJob pre-provisioned-pv ${TestBase}/manifests/pre-provisioned-pv.yaml
runTestJob pre-provisioned-pv-private-with-given-secret ${TestBase}/manifests/pre-provisioned-pv-private-with-given-secret.yaml
runTestJob pre-provisioned-pv-private-with-embedded-secret ${TestBase}/manifests/pre-provisioned-pv-private-with-embedded-secret.yaml

echo "Attatch secret to the daemon SA"
kubectl -n kube-system create secret docker-registry warmmetal \
  --docker-server=http://localhost:31000/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}"
kubectl -n kube-system patch sa csi-image-warm-metal -p '{"imagePullSecrets": [{"name": "warmmetal"}]}'
kubectl -n kube-system delete po $(kubectl get po -n kube-system -o=custom-columns=:metadata.name --no-headers -l=app=csi-image-warm-metal)
kubectlwait kube-system -l=app=csi-image-warm-metal

echo "Run Jobs of ephemeral volume testing"
runTestJob ephemeral-volume-private-with-daemon-secret ${TestBase}/manifests/ephemeral-volume-private-with-daemon-secret.yaml

echo "Run Jobs of pre-provisioined volumetesting"
runTestJob pre-provisioned-pv-private-with-daemon-secret ${TestBase}/manifests/pre-provisioned-pv-private-with-daemon-secret.yaml

echo "Destroying cluster"
minikube delete -p csi-image-test

echo "Testing Done!"

set +x
set +e