#!/usr/bin/env bash

set -x

source $(dirname "$0")/utils.sh

REGISTRY_USERNAME=${REGISTRY_USERNAME:-}
REGISTRY_PASSWORD=${REGISTRY_PASSWORD:-}
REGISTRY_EMAIL=${REGISTRY_EMAIL:-}

echo "Testing on docker"
minikube start -p csi-image-test --container-runtime=docker

echo "Install kube-systemd to enable CRI"
kubectl apply -f https://raw.githubusercontent.com/warm-metal/kube-systemd/master/config/samples/install.yaml
kubectlwait kube-systemd-system -l=control-plane=controller-manager

set +e
kubectl apply -f $(dirname "$0")/kube-systemd-containerd.yaml
kubectlwait kube-system

set +e
executed=$(kubectl get unit systemd-containerd.service -o custom-columns=:status.execTimestamp --no-headers)
while [ $? -ne 0 ] || [ "$executed" == "" ]; do
  sleep 1
  executed=$(kubectl get unit systemd-containerd.service -o custom-columns=:status.execTimestamp --no-headers)
done

error=$(kubectl get unit systemd-containerd.service -o custom-columns=:status.error --no-headers)
if [ "${error}" != "<none>" ]; then
  echo "${error}"
  exit 1
fi

echo "containerd updated"

set -e

echo "Installing csi-driver-image"
kubectl delete --ignore-not-found -f install/cri-containerd.yaml
kubectl apply -f install/cri-containerd.yaml
kubectlwait kube-system -l=app=csi-image-warm-metal

echo "Install private secret and SA"
kubectl create secret docker-registry warmmetal \
  --docker-server=https://index.docker.io/v1/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}" \
  --docker-email="${REGISTRY_EMAIL}"
kubectl create sa warmmetal
kubectl patch sa warmmetal -p '{"imagePullSecrets": [{"name": "warmmetal"}]}'
kubectl -n kube-system delete po $(kubectl get po -n kube-system -o=custom-columns=:metadata.name --no-headers -l=app=csi-image-warm-metal)

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
  --docker-server=https://index.docker.io/v1/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}" \
  --docker-email="${REGISTRY_EMAIL}"
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