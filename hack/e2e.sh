#!/bin/bash

set -e

repo_dir="$(dirname $0)/.."

# Prepare openshift-tests arguments for log output
additional_test_args=""
if [ -n "${ARTIFACT_DIR}" ]; then
    mkdir -p ${ARTIFACT_DIR}
    additional_test_args="-o ${ARTIFACT_DIR}/e2e.log --junit-dir ${ARTIFACT_DIR}/junit"
fi

# Start the operator
${repo_dir}/hack/start.sh

# Run openshift-tests
TEST_CSI_DRIVER_FILES=${repo_dir}/test/e2e/manifest.yaml openshift-tests run openshift/csi $additional_test_args
