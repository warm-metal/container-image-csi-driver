# Running Locally

## Requirements

To build on your own machine you will need:

* A local clone of this repository.
* [Go](https://golang.org/dl/)
* A local Kubernetes cluster ([`k3d`](https://k3d.io/#quick-start), [`kind`](https://kind.sigs.k8s.io/docs/user/quick-start/#installation), or [`minikube`](https://minikube.sigs.k8s.io/docs/start/))
* [`helm`](https://helm.sh/docs/intro/install/)

## Developing locally

_**Note:** Unless specified otherwise, you need to run all commands after changing your working directory to this repository - `cd /path/to/container-image-csi-driver-repository`_

1. First, make sure you can connect to the Kubernetes cluster by following the quickstart guide of your chosen local Kubernetes cluster provider.
  ```
  $ kubectl get nodes
  ```
  Make sure you don't see any errors in your terminal. If do get error(s), please check the quickstart guide or the local Kubernetes cluster provider's documentation on how to get started.

1. Install the container-image-csi-driver using the helm chart.
  ```
  helm upgrade --install wm-csi \
      charts/warm-metal-csi-driver \
      --wait
  ```

1. You can submit an example for testing using `kubectl`:
  ```bash
  kubectl create -f sample/ephemeral-volume.yaml
  ```
