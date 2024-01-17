# Running Locally

You have two options:

1. Use the [Dev Container](#development-container). This is the recommended approach. This can be used with VSCode, the `devcontainer` CLI, or GitHub Codespaces.
1. Install the [requirements](#requirements) on your computer manually.

## Development Container

The development container contains all the tools necessary to work with csi-driver-image.

You can use the development container in a few different ways:

1. [Visual Studio Code](https://code.visualstudio.com/) with [Dev Containers extension](https://marketplace.visualstudio.com/items?itemName=ms-vscode-remote.remote-containers). Open the local clone of `csi-driver-image` folder in VSCode and it should offer to use the development container automatically.
1. [`devcontainer` CLI](https://github.com/devcontainers/cli). Once installed, the local clone of `csi-driver-image` folder and run `devcontainer up --workspace-folder .` followed by `devcontainer exec --workspace-folder . /bin/bash` to get a shell where you can build the code. You can use any editor outside the container to edit code; any changes will be mirrored inside the container.
1. [GitHub Codespaces](https://github.com/codespaces). You can start editing as soon as VSCode is open.

Once you have entered the container, continue to [Developing Locally](#developing-locally).

## Requirements

To build on your own machine without using the Dev Container you will need:

* A local clone of this repository.
* [Go](https://golang.org/dl/)
* A local Kubernetes cluster ([`k3d`](https://k3d.io/#quick-start), [`kind`](https://kind.sigs.k8s.io/docs/user/quick-start/#installation), or [`minikube`](https://minikube.sigs.k8s.io/docs/start/))
* [`helm`](https://helm.sh/docs/intro/install/)

## Developing locally

_**Note:** Unless specified otherwise, you need to run all commands after changing your working directory to this repository - `cd /path/to/csi-driver-image-repository`_

1. First, make sure you can connect to the Kubernetes cluster by following the quickstart guide of your chosen local Kubernetes cluster provider.
  ```
  $ kubectl get nodes
  ```
  Make sure you don't see any errors in your terminal. If do get error(s), please check the quickstart guide or the local Kubernetes cluster provider's documentation on how to get started.

1. Install the csi-driver-image using the helm chart.
  ```
  helm install wm-csi \
      charts/warm-metal-csi-driver \
      -n kube-system \
      -f charts/warm-metal-csi-driver/values.yaml \
      --set csiPlugin.image.tag=v1.0.1 \
      --wait
  ```

1. You can submit a workflow for testing using `kubectl`:
  ```bash
  kubectl create -f sample/ephemeral-volume.yaml
  ```
