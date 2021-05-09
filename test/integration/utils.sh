#!/usr/bin/env bash

function wait() {
  local out=$($@)
  while [ "$out" == "" ]; do
    sleep 1
    out=$($@)
  done
}

# kubectl wait doesn't exit if labels are provided instead a pod name.
# I have to write a substitution.
# kubectlwait namespace pod-selector(can be name, -l label selector, or --all)
function kubectlwait() {
  set +e
  wait kubectl get po -n $@ -o=custom-columns=:metadata.name --no-headers
  local pods=$(kubectl get po -n $@ -o=custom-columns=:metadata.name --no-headers)
  while IFS= read -r pod; do
    kubectl wait -n $1 --timeout=30m --for=condition=ready po $pod
  done <<< "$pods"
  set -e
}

function runTestJob() {
  local job=$1
  local manifest=$2
  echo "Start job $job: $manifest"

  kubectl delete --ignore-not-found -f "$manifest"
  kubectl apply -f "$manifest"
  kubectl wait --timeout=30m --for=condition=complete job/$job
  succeeded=$(kubectl get job/$job --no-headers -ocustom-columns=:.status.succeeded)
  [[ succeeded -eq 1 ]]
}

function installPrivateRegistry() {
  minikube addons -p csi-image-test enable registry
  kubectl -n kube-system apply -f $(dirname "$0")/registry-svc.yaml
  kubectl -n kube-system create cm registry-htpasswd --from-file=$(dirname "$0")/htpasswd
  kubectl -n kube-system patch rc registry --patch '{"spec": {"template": {"spec": {"containers": [{"name": "registry", "env": [{"name": "REGISTRY_AUTH", "value": "htpasswd"}, {"name": "REGISTRY_AUTH_HTPASSWD_REALM", "value": "Registry Realm"}, {"name": "REGISTRY_AUTH_HTPASSWD_PATH", "value": "/auth/htpasswd"}], "volumeMounts": [{"name": "htpasswd", "mountPath": "/auth"}]}], "volumes": [{"name": "htpasswd", "configMap": {"name": "registry-htpasswd"}}]}}}}'
  kubectl -n kube-system delete po -l kubernetes.io/minikube-addons=registry -l actual-registry=true
  kubectlwait kube-system -l kubernetes.io/minikube-addons=registry -l actual-registry=true

  minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i pull docker.io/warmmetal/csi-image-test:simple-fs
  minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i tag --force docker.io/warmmetal/csi-image-test:simple-fs localhost:31000/warmmetal/csi-image-test:simple-fs
  minikube ssh -p csi-image-test -- sudo ctr -n k8s.io i push localhost:31000/warmmetal/csi-image-test:simple-fs --plain-http --user ${REGISTRY_USERNAME}:${REGISTRY_PASSWORD}
}
