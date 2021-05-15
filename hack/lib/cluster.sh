#!/usr/bin/env bash

source $(dirname "${BASH_SOURCE[0]}")/utils.sh

function lib::start_cluster_containerd() {
  local version=${1:-stable}
  minikube start -p csi-image-test \
    --kubernetes-version=${version} \
    --container-runtime=containerd \
    --insecure-registry=localhost:31000
}

function lib::start_cluster_crio() {
  local version=${1:-stable}
  minikube start -p csi-image-test \
    --kubernetes-version=${version} \
    --container-runtime=cri-o \
    --insecure-registry=localhost:31000
}

function lib::start_cluster_docker() {
  local version=${1:-stable}
  minikube start -p csi-image-test \
    --kubernetes-version=${version} \
    --container-runtime=docker \
    --insecure-registry=localhost:31000

  kubectl apply -f https://raw.githubusercontent.com/warm-metal/kube-systemd/master/config/samples/install.yaml
  kubectlwait kube-systemd-system -l=control-plane=controller-manager

  kubectl apply -f $(dirname "${BASH_SOURCE[0]}")/kube-systemd-containerd.yaml
  kubectlwait kube-system

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
}

function lib::install_driver_for_containerd() {
  lib::install_driver $(dirname "${BASH_SOURCE[0]}")/../../install/containerd.yaml $1
}

function lib::uninstall_driver_for_containerd() {
  kubectl delete -f $(dirname "${BASH_SOURCE[0]}")/../../install/containerd.yaml
}

function lib::install_driver_for_crio() {
  lib::install_driver $(dirname "${BASH_SOURCE[0]}")/../../install/cri-o.yaml $1
}

function lib::uninstall_driver_for_crio() {
  kubectl delete -f $(dirname "${BASH_SOURCE[0]}")/../../install/cri-o.yaml
}

function lib::install_driver() {
  local manifest=$1
  local image=$2
  kubectl delete --ignore-not-found -f "${manifest}"
  if [ "${image}" != "" ]; then
    sed "s|image: docker.io/warmmetal/csi-image.*|image: ${image}|" "${manifest}" | kubectl apply -f -
  else
    kubectl apply -f "${manifest}"
  fi
  kubectlwait kube-system -l=app=csi-image-warm-metal
}

function lib::install_private_registry() {
  # go around image pulling of the registry addon.
  minikube cache reload -p csi-image-test
  minikube addons -p csi-image-test enable registry --images=KubeRegistryProxy=google_containers/kube-registry-proxy:0.4

  kubectl -n kube-system apply -f $(dirname "${BASH_SOURCE[0]}")/registry-svc.yaml
  kubectl -n kube-system create cm registry-htpasswd --from-file=$(dirname "${BASH_SOURCE[0]}")/htpasswd
  kubectl -n kube-system patch rc registry --patch '{"spec": {"template": {"spec": {"containers": [{"name": "registry", "env": [{"name": "REGISTRY_AUTH", "value": "htpasswd"}, {"name": "REGISTRY_AUTH_HTPASSWD_REALM", "value": "Registry Realm"}, {"name": "REGISTRY_AUTH_HTPASSWD_PATH", "value": "/auth/htpasswd"}], "volumeMounts": [{"name": "htpasswd", "mountPath": "/auth"}]}], "volumes": [{"name": "htpasswd", "configMap": {"name": "registry-htpasswd"}}]}}}}'
  kubectl -n kube-system delete po -l kubernetes.io/minikube-addons=registry -l actual-registry=true
  kubectlwait kube-system -l kubernetes.io/minikube-addons=registry -l actual-registry=true
}
