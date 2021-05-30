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

function lib::gen_manifests() {
  local secret=$1
  minikube cp -p csi-image-test _output/warm-metal-csi-image-install /usr/bin/warm-metal-csi-image-install > /dev/null
  minikube ssh -p csi-image-test -- sudo chmod +x /usr/bin/warm-metal-csi-image-install
  if [[ "${secret}" != "" ]]; then
    minikube ssh --native-ssh=false -p csi-image-test -- sudo warm-metal-csi-image-install --pull-image-secret-for-daemonset=${secret} 2>/dev/null
  else
    minikube ssh --native-ssh=false -p csi-image-test -- sudo warm-metal-csi-image-install 2>/dev/null
  fi
}

function lib::uninstall_driver() {
  local manifest=$(lib::gen_manifests)
  echo "${manifest}" | kubectl delete -f -
}

function lib::install_driver_from_manifest_file() {
  local manifest=$1
  kubectl delete --ignore-not-found -f ${manifest}
  kubectl apply -f ${manifest}
  kubectlwait kube-system -l=app=csi-image-warm-metal
}

function lib::install_driver() {
  local image=$1
  local manifest=$(lib::gen_manifests $2)
  echo "${manifest}" | kubectl delete --ignore-not-found -f -
  if [ "${image}" != "" ]; then
    echo "${manifest}" | sed "s|image: docker.io/warmmetal/csi-image.*|image: ${image}|" | kubectl apply -f -
  else
    echo "${manifest}" | kubectl apply -f -
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
