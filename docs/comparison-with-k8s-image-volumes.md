# Comparison: container-image-csi-driver vs. Kubernetes Native Image Volumes

This document provides an objective comparison between the container-image-csi-driver (warm-metal) and Kubernetes native image volumes feature introduced in Kubernetes v1.31 (alpha) and promoted to beta in v1.35.

## Overview

### container-image-csi-driver
A CSI driver that mounts container images as volumes in Kubernetes pods. It integrates directly with the container runtime (containerd or CRI-O) to leverage the shared image store and snapshot capabilities.

**Status**: Production-ready (v2.x releases)  
**Installation**: Requires CSI driver deployment  
**First Release**: 2020  

### Kubernetes Image Volumes
A native Kubernetes feature that allows mounting OCI images directly as volumes without requiring an external CSI driver.

**Status**: Beta (v1.35, enabled by default)  
**Installation**: Built-in feature, requires feature gate in earlier versions  
**First Release**: v1.31 (alpha), v1.35 (beta)  

---

## Feature Comparison Matrix

| Feature | container-image-csi-driver | Kubernetes Image Volumes | Notes |
|---------|---------------------------|-------------------------|-------|
| **Read-Only Volumes** | ✅ Yes | ✅ Yes | Both support read-only mounts |
| **Read-Write Volumes** | ✅ Yes | ❌ No | K8s image volumes are always read-only |
| **Ephemeral Inline Volumes** | ✅ Yes | ✅ Yes | Both support inline volume declarations |
| **PersistentVolume Support** | ✅ Yes (ReadOnlyMany/Once) | ❌ No | Only CSI driver supports pre-provisioned PVs |
| **Installation Required** | ✅ DaemonSet + RBAC | ❌ None | Native feature vs external driver |
| **Minimum Kubernetes Version** | v1.25+ | v1.31+ (alpha), v1.35+ (beta) | CSI driver supports older versions |
| **Container Runtime Support** | containerd 1.6.8+, CRI-O 1.20.9+ | containerd 2.0+, CRI-O 1.30+ | CSI driver supports older runtimes |

---

## Detailed Feature Breakdown

### 1. Volume Access Modes

#### container-image-csi-driver
- **Ephemeral volumes**: Can be mounted as read-write or read-only
  - Read-only: Multiple volumes of the same image share a single snapshot
  - Read-write: Each volume gets its own snapshot with copy-on-write semantics
  - Changes in read-write volumes persist until pod deletion
- **PersistentVolumes**: Only ReadOnlyMany and ReadOnlyOnce modes supported
- **Access control**: Determined by `readOnly` field in volumeMount and PV AccessMode

#### Kubernetes Image Volumes
- **Always read-only**: All mounts are enforced as read-only
- **No write capability**: Cannot create, modify, or delete files in mounted volumes
- **Security context**: `fsGroupChangePolicy` has no effect on image volumes

**Use Case Impact**: Applications requiring file system modifications (e.g., creating symlinks, temporary files, or caches) in the mounted image cannot use Kubernetes native image volumes.

---

### 2. Image Pull Policies

#### container-image-csi-driver
```yaml
volumeAttributes:
  image: "docker.io/example/image:tag"
  pullAlways: "true"  # or "false" (default)
```
- **pullAlways: "true"**: Always pull the image, even if cached locally
- **pullAlways: "false"** or omitted: Pull only if image not present locally
- Binary on/off control

#### Kubernetes Image Volumes
```yaml
volumes:
- name: volume
  image:
    reference: "docker.io/example/image:tag"
    pullPolicy: IfNotPresent  # Always, Never, or IfNotPresent
```
- **Always**: Always pull the image (default for `:latest` tag)
- **Never**: Only use local cache, fail if not present
- **IfNotPresent**: Pull if not cached (default for non-`:latest` tags)
- Follows standard Kubernetes container image pull policy semantics

---

### 3. Storage Efficiency & Snapshot Management

#### container-image-csi-driver
- **Snapshot sharing**: Multiple read-only volumes of the same image share one snapshot
- **Reference counting**: Automatic tracking of snapshot usage across volumes
- **Copy-on-write**: Read-write volumes use independent snapshots with minimal overhead
- **Automatic cleanup**: Snapshots are removed when last reference is deleted
- **Crash recovery**: Rebuilds snapshot cache on driver restart from runtime metadata
- **Runtime integration**: Direct integration with containerd/CRI-O snapshot service

#### Kubernetes Image Volumes
- **Runtime-dependent**: Storage efficiency depends on container runtime implementation
- **No explicit guarantees**: Snapshot sharing behavior not specified in documentation
- **OCI-level handling**: Delegated to container runtime's native OCI handling

---

### 4. Asynchronous Image Pulling

#### container-image-csi-driver
- **Async mode**: Enabled when `asyncImagePullTimeout` ≥ 30 seconds
- **Session management**: Multiple pods requesting the same image share a single pull operation
- **Timeout handling**: Configurable timeout with graceful failure on expiration
- **Queue depth**: Configurable channel depth for pull session management (default: 100)
- **Non-blocking**: Pod startup proceeds to mount phase while pull continues in background
- **Metrics**: Separate metrics for async pull start, wait, and completion

**Configuration example**:
```bash
--async-image-pull-timeout=300  # 5 minutes
```

#### Kubernetes Image Volumes
- **Synchronous only**: Image pulls block pod startup
- **Standard retry**: Uses Kubernetes' default volume backoff and retry mechanism
- **No deduplication**: Each pod pull is independent (no session sharing documented)

**Impact**: Large images (multi-GB) can cause significant pod startup delays with native volumes.

---

### 5. Authentication & Credential Management

#### container-image-csi-driver

**Multiple credential sources**:

1. **Credential Provider Plugins** (Recommended for cloud environments)
   ```yaml
   # Driver configuration
   --image-credential-provider-config=/etc/kubernetes/image-credential-providers/config.json
   --image-credential-provider-bin-dir=/etc/kubernetes/image-credential-providers
   ```
   - Supports AWS ECR, Google GCR, Azure ACR
   - Uses cloud provider IAM roles/service accounts
   - Automatic credential rotation
   - No long-lived secrets in cluster

2. **Node-level secrets** (via `nodePublishSecretRef`)
   ```yaml
   csi:
     driver: container-image.csi.k8s.io
     nodePublishSecretRef:
       name: my-registry-secret
   ```

3. **ServiceAccount ImagePullSecrets**
   - Shared secrets attached to driver's ServiceAccount
   - Automatic discovery and usage

4. **Secret caching**
   - In-memory cache for performance (can be disabled)
   - Useful for high-frequency pulls

#### Kubernetes Image Volumes

**Standard Kubernetes mechanisms**:

1. **ImagePullSecrets** (same as container images)
   ```yaml
   spec:
     imagePullSecrets:
     - name: my-registry-secret
   ```

2. **ServiceAccount ImagePullSecrets**
   - Attached to pod's ServiceAccount

3. **Node credential lookup**
   - Standard kubelet credential provider mechanism

**Note**: Credential provider plugin support for image volumes depends on kubelet configuration, not the volume feature itself.

---

### 6. Monitoring & Observability

#### container-image-csi-driver

**Built-in Prometheus Metrics**:

```
# Image pull duration (gauge)
warm_metal_pull_duration_seconds{image="...", error="true|false"}

# Image pull duration histogram
warm_metal_pull_duration_seconds_hist{error="true|false"}

# Image size in bytes
warm_metal_pull_size_bytes{image="..."}

# Operation error counter
warm_metal_operation_errors_total{operation_type="pull-async-start|pull-async-wait|pull-sync-call|mount|unmount"}
```

**Metrics endpoint**: Exposed by driver pods for Prometheus scraping

#### Kubernetes Image Volumes

**Standard Kubernetes Metrics**:
- Volume metrics from kubelet
- Generic volume operation metrics
- No image-specific metrics documented

**Observability**: Relies on standard kubelet and container runtime metrics.

---

### 7. Container Runtime Compatibility

#### container-image-csi-driver

**Tested Compatibility Matrix**:

| CSI Driver Version | Kubernetes | containerd | CRI-O |
|-------------------|------------|------------|-------|
| 0.6.x - 2.0.x | v1.25 | 1.6.8 | v1.20.9 - v1.25.2 |
| 2.1.x | v1.32 | 2.x | v1.25.2 |

**Supported Runtimes**:
- containerd (1.6.8+, 2.x)
- CRI-O (1.20.9+)
- Docker with containerd backend (after clearing config)

**Auto-detection**: Installer utility auto-detects runtime and cluster type (k8s, k3s, microk8s)

#### Kubernetes Image Volumes

**Minimum Runtime Requirements**:

| Runtime | Minimum Version | Notes |
|---------|----------------|-------|
| containerd | 2.0 | CRI-level support required |
| CRI-O | 1.30 | CRI-level support required |
| runc | 1.1 | OCI-level (if used) |
| crun | 1.8.6 | OCI-level (if used) |

**Compatibility**: Requires newer runtime versions than CSI driver.

---

### 8. Volume Types & Usage Patterns

#### container-image-csi-driver

**1. Ephemeral Inline Volumes**:
```yaml
volumes:
- name: my-volume
  csi:
    driver: container-image.csi.k8s.io
    nodePublishSecretRef:
      name: registry-secret
    volumeAttributes:
      image: "docker.io/example/image:tag"
      pullAlways: "true"
```

**2. Pre-provisioned PersistentVolumes**:
```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: my-pv
spec:
  storageClassName: container-image.csi.k8s.io
  capacity:
    storage: 5Gi
  accessModes:
    - ReadOnlyMany
  csi:
    driver: container-image.csi.k8s.io
    volumeHandle: "docker.io/example/image:tag"
    nodePublishSecretRef:
      name: registry-secret
      namespace: default
```

#### Kubernetes Image Volumes

**Ephemeral Inline Volumes Only**:
```yaml
volumes:
- name: my-volume
  image:
    reference: "docker.io/example/image:tag"
    pullPolicy: IfNotPresent
```

**No PersistentVolume Support**: Cannot create PVs with image volumes.

---

### 9. SubPath Support

#### container-image-csi-driver
- ✅ Full subPath support
- ✅ subPathExpr support
- Works with both read-only and read-write volumes

```yaml
volumeMounts:
- name: image-volume
  mountPath: /app/config
  subPath: config/production
```

#### Kubernetes Image Volumes
- ✅ subPath support (from v1.33+)
- ✅ subPathExpr support (from v1.33+)
- Read-only enforcement applies to subPath mounts

```yaml
volumeMounts:
- name: image-volume
  mountPath: /app/config
  subPath: config/production
```

---

### 10. Installation & Operations

#### container-image-csi-driver

**Installation Requirements**:
1. Deploy CSI driver DaemonSet to all nodes
2. Create ServiceAccount, ClusterRole, ClusterRoleBinding
3. Create CSIDriver resource
4. Optional: Configure credential provider plugins

**Installation Methods**:

1. **Using installer utility** (Recommended):
```bash
container-image-csi-driver-install | kubectl apply -f -
```

2. **Using Helm**:
```bash
helm install warm-metal-csi-driver ./charts/warm-metal-csi-driver
```

3. **Manual YAML**:
```bash
kubectl apply -f sample/install/
```

**Configuration Options**:
- Namespace customization
- Pull secrets configuration
- Credential provider plugins
- Secret caching settings
- Async pull timeout

**Operational Overhead**:
- Driver updates and maintenance
- Monitoring driver health
- Managing driver permissions

#### Kubernetes Image Volumes

**Installation Requirements**:
- None (built-in feature)
- Enable feature gate if using v1.31-v1.34 (alpha)
- Enabled by default in v1.35+ (beta)

**Feature Gate** (if needed):
```yaml
# kube-apiserver and kubelet flags
--feature-gates=ImageVolume=true
```

**Operational Overhead**:
- None
- No additional components to manage

---

### 11. Testing & Stability

#### container-image-csi-driver

**Test Coverage**:
- ✅ Sanity tests (CSI spec compliance)
- ✅ End-to-end tests
- ✅ Integration tests (containerd, CRI-O)
- ✅ Driver restart tests
- ✅ Runtime restart tests
- ✅ Metrics validation tests

**CI/CD**:
- Multi-runtime test matrix
- Automated builds for every commit
- Multi-architecture support (amd64, arm64)

**Production Usage**:
- Documented production deployments
- Active community support
- Regular maintenance and updates

#### Kubernetes Image Volumes

**Maturity Level**:
- Beta in v1.35 (2025)
- Alpha in v1.31 (2024)
- Enabled by default in v1.35+

**Testing**:
- Kubernetes e2e test suite
- SIG-Storage testing
- Less battle-tested than mature CSI driver

**Production Readiness**:
- Beta status indicates API stability
- Breaking changes possible before GA
- Production use at own discretion

---

## Decision Guide

### Choose container-image-csi-driver when you need:

- ✅ **Read-write ephemeral volumes** for applications that modify mounted filesystems
- ✅ **Pre-provisioned PersistentVolumes** for sharing images across multiple pods/namespaces
- ✅ **Large image support** with asynchronous pulling to avoid pod startup delays
- ✅ **Advanced credential management** with cloud provider plugins (ECR, GCR, ACR)
- ✅ **Detailed metrics** for monitoring image pull performance and storage usage
- ✅ **Older runtime versions** (containerd 1.6.x, CRI-O 1.20.x)
- ✅ **Production-proven solution** with extensive test coverage
- ✅ **Explicit snapshot sharing** for storage efficiency

### Choose Kubernetes Image Volumes when you need:

- ✅ **Read-only access only** for immutable configuration or static content
- ✅ **Zero operational overhead** with no external components to manage
- ✅ **Native Kubernetes integration** without CSI driver dependencies
- ✅ **Standard pull policy semantics** (`Always`, `Never`, `IfNotPresent`)
- ✅ **Future-proof solution** as part of Kubernetes core
- ✅ **Newer runtime versions** (containerd 2.0+, CRI-O 1.30+)
- ✅ **Simpler architecture** for straightforward use cases

---

## Migration Considerations

### Migrating FROM container-image-csi-driver TO Kubernetes Image Volumes

**Prerequisites**:
1. Kubernetes v1.35+ (or v1.31+ with feature gate enabled)
2. Container runtime: containerd 2.0+ or CRI-O 1.30+
3. All volumes must be read-only (no file system modifications required)
4. No PersistentVolume usage

**Manifest Changes**:

**Before** (CSI driver):
```yaml
volumes:
- name: app-code
  csi:
    driver: container-image.csi.k8s.io
    nodePublishSecretRef:
      name: registry-secret
    volumeAttributes:
      image: "docker.io/example/app:v1.0"
      pullAlways: "true"
```

**After** (Native image volumes):
```yaml
volumes:
- name: app-code
  image:
    reference: "docker.io/example/app:v1.0"
    pullPolicy: Always
```

**Pull secrets** move to pod spec:
```yaml
spec:
  imagePullSecrets:
  - name: registry-secret
```
---

## References

### container-image-csi-driver
- **Repository**: https://github.com/warm-metal/container-image-csi-driver
- **Documentation**: https://github.com/warm-metal/container-image-csi-driver/blob/main/README.md
- **Installation Guide**: https://github.com/warm-metal/container-image-csi-driver#installation
- **Credential Providers**: https://github.com/warm-metal/container-image-csi-driver/blob/main/docs/credential-providers/README.md

### Kubernetes Image Volumes
- **KEP (Enhancement Proposal)**: [KEP-4639: Image Volumes](https://github.com/kubernetes/enhancements/tree/master/keps/sig-storage/4639-image-volumes)
- **Documentation**: https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/
- **Concept Documentation**: https://kubernetes.io/docs/concepts/storage/volumes/#image
- **Feature Gate**: `ImageVolume` (beta in v1.35)

### Related Resources
- **CSI Specification**: https://github.com/container-storage-interface/spec
- **Container Runtime Requirements**: 
  - containerd releases: https://containerd.io/releases/
  - CRI-O releases: https://github.com/cri-o/cri-o/releases
- **Kubernetes Credential Providers**: https://kubernetes.io/docs/tasks/kubelet-credential-provider/
