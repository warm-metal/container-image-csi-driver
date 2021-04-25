#!/usr/bin/env bash

set -e
set -x

source $(dirname "$0")/utils.sh

echo "Testing on containerd"
minikube start -p csi-image-test --container-runtime=containerd

echo "Installing csi-driver-image"
kubectl delete --ignore-not-found -f install/cri-containerd.yaml
kubectl apply  -f install/cri-containerd.yaml
kubectlwait kube-system -l=app=csi-image-warm-metal

echo "Clear dangling Jobs"
kubectl delete --ignore-not-found -f test/integration/manifest.yaml

echo "Installing Jobs of ephemeral volume testing"
kubectl delete --ignore-not-found -f test/integration/manifests/ephemeral-volume.yaml
kubectl apply  -f test/integration/manifests/ephemeral-volume.yaml
kubectl wait --timeout=30m --for=condition=complete job/ephemeral-volume
succeeded=$(kubectl get job/ephemeral-volume --no-headers -ocustom-columns=:.status.succeeded)
[[ succeeded -eq 1 ]]

echo "Installing Jobs of pre-provisioined volumetesting"
kubectl delete --ignore-not-found -f test/integration/manifests/pre-provisioned-pv.yaml
kubectl apply  -f test/integration/manifests/pre-provisioned-pv.yaml
kubectl wait --timeout=30m --for=condition=complete job/pre-provisioned-pv
succeeded=$(kubectl get job/pre-provisioned-pv --no-headers -ocustom-columns=:.status.succeeded)
[[ succeeded -eq 1 ]]

echo "Destroying cluster"
minikube delete -p csi-image-test

echo "Testing Done!"

set +x
set +e