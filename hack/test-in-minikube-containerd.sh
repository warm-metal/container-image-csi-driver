#!/usr/bin/env bash

set -e
set -x

echo "Testing on containerd"
minikube start -p csi-image-test --container-runtime=containerd

echo "Installing csi-driver-image"
kubectl apply --wait -f install/cri-containerd.yaml

echo "Clear dangling Jobs"
kubectl delete --ignore-not-found -f test/integration/manifest.yaml

echo "Installing Jobs of ephemeral volume testing"
kubectl delete --ignore-not-found -f test/integration/manifests/ephemeral-volume.yaml
kubectl apply --wait -f test/integration/manifests/ephemeral-volume.yaml
kubectl wait --timeout=30m --for=condition=complete job/ephemeral-volume
succeeded=$(kubectl get job/ephemeral-volume --no-headers -ocustom-columns=:.status.succeeded)
[[ succeeded -eq 1 ]]

echo "Installing Jobs of pre-provisioined volumetesting"
kubectl delete --ignore-not-found -f test/integration/manifests/pre-provisioned-pv.yaml
kubectl apply --wait -f test/integration/manifests/pre-provisioned-pv.yaml
kubectl wait --timeout=30m --for=condition=complete job/pre-provisioned-pv
succeeded=$(kubectl get job/pre-provisioned-pv --no-headers -ocustom-columns=:.status.succeeded)
[[ succeeded -eq 1 ]]

echo "Destroying cluster"
minikube delete -p csi-image-test

echo "Testing Done!"

set +x
set +e