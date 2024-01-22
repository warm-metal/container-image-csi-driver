# Automation

There are a few [GitHub Workflows](https://docs.github.com/en/actions/using-workflows/about-workflows#about-workflows) that run on this repository.

## Build
- [![Create and publish the container image.](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/images-build-and-push.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/images-build-and-push.yaml)
    - On the main repository - `warm-metal/container-image-csi-driver`, builds and pushes the container image to Docker Hub [`warmmetal/container-image-csi-driver`](https://hub.docker.com/r/warmmetal/container-image-csi-driver)
    - On any forks, builds and pushes the container image to `ghcr.io/<repository-name>`

## Tests

- [![backward-compatibility-5mins](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/backward-compatibility.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/backward-compatibility.yaml)
- [![containerd-11mins](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/containerd.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/containerd.yaml)
- [![cri-o-10mins](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/cri-o.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/cri-o.yaml)
- [![restart-ds-containerd-5mins](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/restart-ds-containerd.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/restart-ds-containerd.yaml)
- [![restart-ds-crio-8mins](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/restart-ds-crio.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/restart-ds-crio.yaml)
- [![test-metrics-5m](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/metrics.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/metrics.yaml)

## Maintenance

- [![Delete old container images](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/images-cleanup.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/images-cleanup.yaml)
    - Deletes all `ghcr.io/<repository-name>` image tags, expect `latest` or any semver tags. This workflow will run on forks only.
- [![Close stale issues and PRs](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/stale.yaml/badge.svg)](https://github.com/warm-metal/container-image-csi-driver/actions/workflows/stale.yaml)
    - [Create a repository variable](https://docs.github.com/en/actions/learn-github-actions/variables#creating-configuration-variables-for-a-repository) `DEBUG_ONLY` with value `true` to run the action in dry-run mode.
    - Marks issues or PRs as stale after 30 days and closes them after 7 days, except those labeled with any of of the following
        - `awaiting-approval`
        - `work-in-progress`
        - `help-wanted`
- [Dependabot](../.github/dependabot.yml) - [Dependabot](https://docs.github.com/en/code-security/getting-started/dependabot-quickstart-guide#about-dependabot) is currently being used to [keep the GitHub Actions up to date](https://docs.github.com/en/code-security/dependabot/working-with-dependabot/keeping-your-actions-up-to-date-with-dependabot)
