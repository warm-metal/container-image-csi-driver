# SELinux Support Implementation Plan

## Overview
Implement SELinux support in the warm-metal container-image CSI driver, mirroring the changes from AWS EBS CSI driver PRs #2253 and #2507.

## Background
The AWS EBS CSI driver added SELinux support through two PRs:
- **PR #2253**: Added initial SELinux mount infrastructure
- **PR #2507**: Fixed critical bugs (removed readOnly from /sys/fs/selinux, added seLinuxMount field)

## Objectives
Enable SELinux-aware volume mounting in the warm-metal CSI driver by:
1. Adding a configuration parameter to enable/disable SELinux support
2. Mounting SELinux-related directories when enabled
3. Setting the CSIDriver spec field to allow SELinux mount options

## Implementation Details

### 1. Configuration Parameter (values.yaml)

**Location:** `charts/warm-metal-csi-driver/values.yaml`

**Add after line 77:**
```yaml
# SELinux support
# Enable SELinux-aware mounting on nodes with SELinux enabled
# WARNING: Only set to true if ALL nodes in the cluster have SELinux enabled
# The driver will mount /sys/fs/selinux which only exists on SELinux-enabled systems
selinux: false
```

**Rationale:**
- Default to `false` for backward compatibility
- `/sys/fs/selinux` doesn't exist on non-SELinux systems
- Clear warning prevents misconfiguration

### 2. Container Volume Mounts (nodeplugin.yaml)

**Location:** `charts/warm-metal-csi-driver/templates/nodeplugin.yaml`

**Add after the existing volumeMounts in the csi-plugin container (after line 159):**
```yaml
            {{- if .Values.selinux }}
            - mountPath: /sys/fs/selinux
              name: selinux-sysfs
            - mountPath: /etc/selinux/config
              name: selinux-config
              readOnly: true
            {{- end }}
```

**Important Notes:**
- `/sys/fs/selinux` must NOT be readOnly (learned from PR #2507)
- `/etc/selinux/config` can be readOnly
- Conditional rendering based on `.Values.selinux`

### 3. Host Path Volume Definitions (nodeplugin.yaml)

**Location:** `charts/warm-metal-csi-driver/templates/nodeplugin.yaml`

**Add after the existing volumes section (after line 210, before tolerations):**
```yaml
        {{- if .Values.selinux }}
        - name: selinux-sysfs
          hostPath:
            path: /sys/fs/selinux
            type: Directory
        - name: selinux-config
          hostPath:
            path: /etc/selinux/config
            type: File
        {{- end }}
```

**Important Notes:**
- Both volumes use hostPath to mount from node filesystem
- `type: Directory` for sysfs, `type: File` for config
- No readOnly in hostPath definition (only in volumeMount for config)

### 4. CSIDriver Spec Update (csi-driver.yaml)

**Location:** `charts/warm-metal-csi-driver/templates/csi-driver.yaml`

**Add after the fsGroupPolicy section (after line 14):**
```yaml
  {{- if .Values.selinux }}
  seLinuxMount: true
  {{- end }}
```

**Rationale:**
- Required for Kubernetes to pass SELinux mount options to the CSI driver
- Only enabled when selinux parameter is true
- See: https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#csidriverspec-v1-storage-k8s-io

## Testing Strategy

### Test 1: Default Behavior (selinux: false)
```bash
helm template warm-metal-csi-driver charts/warm-metal-csi-driver > before.yaml
# Apply changes
helm template warm-metal-csi-driver charts/warm-metal-csi-driver > after.yaml
diff before.yaml after.yaml
# Expected: No differences (selinux defaults to false)
```

### Test 2: SELinux Enabled (selinux: true)
```bash
helm template warm-metal-csi-driver charts/warm-metal-csi-driver \
  --set selinux=true > selinux-enabled.yaml
# Verify output contains:
# 1. selinux-sysfs and selinux-config volumes in DaemonSet
# 2. Corresponding volumeMounts in csi-plugin container
# 3. seLinuxMount: true in CSIDriver spec
```

### Test 3: Verify Volume Mount Properties
Check that:
- `/sys/fs/selinux` is mounted WITHOUT `readOnly: true` in volumeMount
- `/etc/selinux/config` is mounted WITH `readOnly: true` in volumeMount
- Both volumes appear in the volumes section with correct hostPath types

## Architecture Diagram

```mermaid
graph TD
    A[Helm Values] -->|selinux: true/false| B[values.yaml]
    B --> C{SELinux Enabled?}
    C -->|Yes| D[Mount SELinux Volumes]
    C -->|Yes| E[Set seLinuxMount: true]
    C -->|No| F[Skip SELinux Config]
    D --> G[csi-plugin container]
    G --> H[/sys/fs/selinux writable]
    G --> I[/etc/selinux/config readonly]
    H --> J[SELinux-aware mount]
    I --> J
    E --> K[CSIDriver Spec]
    K --> L[K8s passes SELinux options]
    L --> J
```

## Files to Modify

1. **charts/warm-metal-csi-driver/values.yaml**
   - Add `selinux: false` parameter with documentation

2. **charts/warm-metal-csi-driver/templates/nodeplugin.yaml**
   - Add conditional volumeMounts for SELinux paths
   - Add conditional volume definitions for SELinux hostPaths

3. **charts/warm-metal-csi-driver/templates/csi-driver.yaml**
   - Add conditional `seLinuxMount: true` field

## Key Differences from AWS EBS Implementation

1. **No separate node section**: AWS EBS has `node.selinux`, we use top-level `selinux`
2. **Simpler structure**: Warm-metal has a simpler Helm chart structure
3. **Single template file**: All node configuration in `nodeplugin.yaml` vs separate templates

## Success Criteria

- [ ] Helm chart templates successfully with selinux disabled (default)
- [ ] Helm chart templates successfully with selinux enabled
- [ ] No changes to output when selinux is disabled
- [ ] Correct volumes and mounts appear when selinux is enabled
- [ ] seLinuxMount field appears in CSIDriver spec when enabled
- [ ] /sys/fs/selinux is mounted without readOnly restriction
- [ ] /etc/selinux/config is mounted with readOnly restriction

## References

- AWS EBS CSI Driver PR #2253: https://github.com/kubernetes-sigs/aws-ebs-csi-driver/pull/2253
- AWS EBS CSI Driver PR #2507: https://github.com/kubernetes-sigs/aws-ebs-csi-driver/pull/2507
- Kubernetes CSIDriver API: https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.33/#csidriverspec-v1-storage-k8s-io
