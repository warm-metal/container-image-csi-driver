#!/usr/bin/env sh

set -x

[ "${TARGET1}" != "" ] && [ "${TARGET2}" != "" ] && \
  [ $(stat -c%i "${TARGET1}/csi-file1") -eq $(stat -c%i "${TARGET2}/csi-file1") ] && \
  [ $(stat -c%i "${TARGET1}/csi-file2") -eq $(stat -c%i "${TARGET2}/csi-file2") ] && \
  [ $(stat -c%i "${TARGET1}/csi-folder1") -eq $(stat -c%i "${TARGET2}/csi-folder1") ] && \
  [ $(stat -c%i "${TARGET1}/csi-folder1/file") -eq $(stat -c%i "${TARGET2}/csi-folder1/file") ] && \
  exit 0

exit 1
set +x