#!/bin/bash
set -eux

# Copies over welcome message
cp .devcontainer/welcome-message.txt /usr/local/etc/vscode-dev-containers/first-run-notice.txt
