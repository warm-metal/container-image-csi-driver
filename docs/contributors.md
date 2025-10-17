# Contributor Guide

Welcome! This guide helps contributors set up their forked repository and follow best practices when contributing to this project.

## Contributing Guidelines

We follow standard open-source contribution practices. Please read:
- [GitHub's Guide to Contributing](https://docs.github.com/en/get-started/exploring-projects-on-github/contributing-to-a-project)
- [How to Contribute to Open Source](https://opensource.guide/how-to-contribute/)

### Good Practices

- **Search existing issues** before creating new ones
- **Create focused PRs** - one feature or fix per PR
- **Write clear commit messages** - explain what and why, not just how
- **Test your changes** - ensure CI passes before requesting review
- **Update documentation** - if your changes affect usage
- **Be respectful** - follow the [code of conduct](https://opensource.guide/code-of-conduct/)

## Fork Setup for Image Builds

When you fork this repository and push changes, GitHub Actions will automatically:
- Build multi-architecture container images (amd64 and arm64)
- Push images to your GitHub Container Registry at `ghcr.io/<your-username>/container-image-csi-driver:<branch>`
- Make images available for testing your changes before creating a PR

### Setup Steps

#### 1. Enable GitHub Actions

1. Go to your fork: `https://github.com/<your-username>/container-image-csi-driver`
2. Click **Settings** → **Actions** → **General**
3. Under **"Actions permissions"**, select: **"Allow all actions and reusable workflows"**
4. Click **Save**

#### 2. Configure Workflow Permissions

In the same **Settings** → **Actions** → **General** page:

1. Scroll down to **"Workflow permissions"**
2. Select: **"Read and write permissions"**
3. Click **Save**

This allows GitHub Actions to push images to your GitHub Container Registry (GHCR).

#### 3. Push and Build

Once configured, simply push to any branch:

```bash
git push origin <your-branch>
```

GitHub Actions will automatically build and push to: `ghcr.io/<your-username>/container-image-csi-driver:<your-branch>`

### Using Your Fork Images for Testing

```bash
# Pull your fork's image
docker pull ghcr.io/<your-username>/container-image-csi-driver:<branch>

# Or use in Helm
helm install my-csi charts/warm-metal-csi-driver \
  --set csiPlugin.image.repository=ghcr.io/<your-username>/container-image-csi-driver \
  --set csiPlugin.image.tag=<branch>
```

## Troubleshooting

### Permission denied while pushing to GHCR

**Cause:** Workflow permissions not enabled or PAT expired.

**Solution:**
1. Verify **Step 2** above (Read and write permissions)
2. If using a Personal Access Token (PAT), check if it's expired:
   - Go to GitHub → **Settings** → **Developer settings** → **Personal access tokens**
   - Create new token with `write:packages` scope
   - Add to your fork: **Settings** → **Secrets** → Name: `GHCR_TOKEN`

### Build is very slow (>20 minutes)

**Expected:** ARM64 builds use QEMU emulation on AMD64 runners.
- First build: ~25 minutes (no cache)
- Subsequent builds: ~5-15 minutes (with cache)

### Package does not exist

**Wait** for the first GitHub Actions workflow to complete. The package is created automatically.

## Image Tagging

| Push Event | Image Tag |
|------------|-----------|
| Push to any branch | `<branch-name>` |
| Push to main | `latest` |
| Push version tag | `v<version>` |

## Additional Resources

- [Multi-architecture builds explanation](https://docs.docker.com/build/building/multi-platform/)
- [GitHub Container Registry docs](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
- [Creating pull requests](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/creating-a-pull-request)

## Need Help?

- Check [GitHub Actions logs](https://github.com/<your-username>/container-image-csi-driver/actions) in your fork
- Search [existing issues](https://github.com/warm-metal/container-image-csi-driver/issues)
- Create a new issue with details about your problem
