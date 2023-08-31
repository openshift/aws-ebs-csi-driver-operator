#!/bin/bash

set -e

REPO_ROOT="$(dirname $0)/.."

cd "${REPO_ROOT}"
DUMP="go run github.com/openshift/aws-ebs-csi-driver-operator/cmd/dump"

# TODO: loop over flavours and drivers
${DUMP} -flavour standalone -path assets/generated/aws-ebs/standalone
${DUMP} -flavour hypershift -path assets/generated/aws-ebs/hypershift
