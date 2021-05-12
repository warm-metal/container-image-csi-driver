#!/usr/bin/env sh

set -x

[ "${TARGET}" != "" ] && \
  [ -f "${TARGET}/csi-file1" ] && \
  [ -f "${TARGET}/csi-file2" ] && \
  [ -d "${TARGET}/csi-folder1" ] && \
  [ -f "${TARGET}/csi-folder1/file" ] && \
   exit 0

if [ "${CHECK_RO}" != "" ]; then
  err=$(touch "${TARGET}/.ro")
  [ "${err}" == *"Read-only file system" ] || exit 1
fi

exit 1
set +x