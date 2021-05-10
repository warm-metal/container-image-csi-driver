#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/../../hack/lib/utils.sh

TestBase=$(dirname "${BASH_SOURCE[0]}")

function InstallPrivateRegistry() {
  minikube addons -p csi-image-test enable registry
  # kubectl -n kube-system patch ds registry-proxy --patch '{"spec": {"template": {"spec": {"containers": [{"name": "registry-proxy", "image": "gcr.io/google_containers/kube-registry-proxy:0.4"}]}}}}'
  kubectl -n kube-system apply -f ${TestBase}/registry-svc.yaml
  kubectl -n kube-system create cm registry-htpasswd --from-file=${TestBase}/htpasswd
  kubectl -n kube-system patch rc registry --patch '{"spec": {"template": {"spec": {"containers": [{"name": "registry", "env": [{"name": "REGISTRY_AUTH", "value": "htpasswd"}, {"name": "REGISTRY_AUTH_HTPASSWD_REALM", "value": "Registry Realm"}, {"name": "REGISTRY_AUTH_HTPASSWD_PATH", "value": "/auth/htpasswd"}], "volumeMounts": [{"name": "htpasswd", "mountPath": "/auth"}]}], "volumes": [{"name": "htpasswd", "configMap": {"name": "registry-htpasswd"}}]}}}}'
  kubectl -n kube-system delete po -l kubernetes.io/minikube-addons=registry -l actual-registry=true
  kubectlwait kube-system -l kubernetes.io/minikube-addons=registry -l actual-registry=true

  minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i pull docker.io/warmmetal/csi-image-test:simple-fs
  minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i tag --force docker.io/warmmetal/csi-image-test:simple-fs localhost:31000/warmmetal/csi-image-test:simple-fs
  minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i push localhost:31000/warmmetal/csi-image-test:simple-fs --plain-http --user warmmetal:warmmetal
}

set -e
echo "Install a private registry"
InstallPrivateRegistry
export REGISTRY_USERNAME=warmmetal
export REGISTRY_PASSWORD=warmmetal

echo "Install private secret and SA"
kubectl create secret docker-registry warmmetal \
  --docker-server=http://localhost:31000/ \
  --docker-username=${REGISTRY_USERNAME} \
  --docker-password="${REGISTRY_PASSWORD}"
kubectl create sa warmmetal
kubectl patch sa warmmetal -p '{"imagePullSecrets": [{"name": "warmmetal"}]}'

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
set +e