#!/bin/bash

set -e

REPO_ROOT="$(dirname $0)/.."
NAMESPACE="openshift-cluster-csi-drivers"

# Prepare openshift-tests arguments for log output
ADDITIONAL_TEST_ARGS=""
if [ -n "${ARTIFACT_DIR}" ]; then
    mkdir -p ${ARTIFACT_DIR}
    ADDITIONAL_TEST_ARGS="-o ${ARTIFACT_DIR}/e2e.log --junit-dir ${ARTIFACT_DIR}/junit"
fi

# Start the operator
${REPO_ROOT}/hack/start.sh

# Wait for the CSI driver to get deployed. This is necessary for topology tests
# - they need the driver on all nodes.

# Step1: The operator says it's available (at least some pods are running).
echo "Waiting for clustercsidrivers.operator.openshift.io/ebs.csi.aws.com"
oc wait clustercsidrivers.operator.openshift.io/ebs.csi.aws.com --for=condition=AWSEBSDriverControllerAvailable --timeout=5m

# Step2: Wait for *all* pods to be running.
echo "Waiting for all driver pods"
oc wait -n "$NAMESPACE" pod --all --for=condition=Ready --timeout=5m

# Run openshift-tests
TEST_CSI_DRIVER_FILES=${REPO_ROOT}/test/e2e/manifest.yaml openshift-tests run openshift/csi $ADDITIONAL_TEST_ARGS
