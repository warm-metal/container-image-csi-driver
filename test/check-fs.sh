#!/usr/bin/env sh

[ "${TARGET}" != "" ] && \
  [ -f "${TARGET}/csi-file1" ] && \
  [ -f "${TARGET}/csi-file2" ] && \
  [ -d "${TARGET}/csi-folder1" ] && \
  [ -f "${TARGET}/csi-folder1/file" ] && \
   exit 0

exit 1
