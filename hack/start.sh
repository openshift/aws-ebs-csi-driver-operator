#!/bin/bash

# Operator startup script, mainly for e2e tests.
# - Parses operator CSV and fills json files in template/ directory from it.
# - Applies the resulting json files.
# - Stores the json files in $ARTIFACT_DIR, if set.
#
# Assumptions about the CSV:
# - .spec.install.spec.permissions has exactly one item
# - .spec.install.spec.clusterPermissions has exactly one item
# - .spec.install.spec.deployments has exactly one item

set -euo pipefail

log::debug() {
    echo >&2 $@
}
log::info() {
    echo >&2 $@
}
log::warn() {
    echo >&2 WARNING: $@
}

usage() {
    cat <<EOF
$0 [-d] [-h]

    -d: dry-run
    -h: help
EOF
    exit
}

get_image() {
    component=$1
    eval echo $IMAGE_FORMAT
}

cleanup(){
    local RETURN_CODE=$?

    set +e

    # Save manifests for debugging if ARTIFACT_DIR is set.
    if [ -n "$ARTIFACT_DIR" ]; then
        mkdir -p $ARTIFACT_DIR/manifest
        cp $MANIFEST/* $ARTIFACT_DIR/manifest/
    fi

    if [ -n "$MANIFEST" ]; then
        rm -rf $MANIFEST
    fi
    exit $RETURN_CODE
}


DRYRUN=false
REPO_ROOT="$(dirname $0)/.."
YAML2JSON=$REPO_ROOT/hack/yaml2json.py
IMAGE_FORMAT=${IMAGE_FORMAT:-""}
ARTIFACT_DIR=${ARTIFACT_DIR:-""}
MANIFEST=$(mktemp -d)
trap cleanup exit

# Find the latest OCP version. It's the greatest 4.x directory in /manifests dir
CSV_FILE=$REPO_ROOT/bundle/manifests/aws-ebs-csi-driver-operator.clusterserviceversion.yaml
log::debug "Using CSV $CSV_FILE"

if [ ! -e $CSV_FILE ]; then
    echo "$CSV_FILE does not exist"
    exit 1
fi

while getopts ":hd" OPT; do
  case $OPT in
    h ) usage
        ;;
    d )
        DRYRUN=true
        if [ -z $ARTIFACT_DIR ]; then
            echo 'ERROR: $ARTIFACT_DIR must be set in dry-run mode'
            exit 1
        fi
        ;;
    \? ) usage
        ;;
  esac
done


# Interpret $IMAGE_FORMAT to get current images.
# Example IMAGE_FORMAT in OCP CI: "registry.svc.ci.openshift.org/ci-op-pthpkjbt/stable:${component}"
if [ -n "${IMAGE_FORMAT}" ] ; then
    cat <<EOF >$MANIFEST/.sedscript
s,quay.io/openshift/origin-csi-external-attacher:latest,$(get_image csi-external-attacher),
s,quay.io/openshift/origin-csi-external-provisioner:latest,$(get_image csi-external-provisioner),
s,quay.io/openshift/origin-csi-external-resizer:latest,$(get_image csi-external-resizer),
s,quay.io/openshift/origin-csi-external-snapshotter:latest,$(get_image csi-external-snapshotter),
s,quay.io/openshift/origin-csi-node-driver-registrar:latest,$(get_image csi-node-driver-registrar),
s,quay.io/openshift/origin-csi-livenessprobe:latest,$(get_image csi-livenessprobe),
s,quay.io/openshift/origin-aws-ebs-csi-driver:latest,$(get_image aws-ebs-csi-driver),
s,quay.io/openshift/origin-aws-ebs-csi-driver-operator:latest,$(get_image aws-ebs-csi-driver-operator),
EOF
else
    log::warn 'Missing $IMAGE_FORMAT, using images from CSV'
    echo "" >$MANIFEST/.sedscript
fi

log::info "Using IMAGE_FORMAT=$IMAGE_FORMAT"

# Parse variables needed by templates from CSV.
# Using --raw-output for single-value output to remove "" around the value.
export SERVICE_ACCOUNT_NAME=$( $YAML2JSON < $CSV_FILE | jq  --raw-output ".spec.install.spec.permissions[0].serviceAccountName" )
export ROLE_RULES=$( $YAML2JSON < $CSV_FILE | jq  ".spec.install.spec.permissions[0].rules" )
export CLUSTER_ROLE_RULES=$( $YAML2JSON < $CSV_FILE | jq  ".spec.install.spec.clusterPermissions[0].rules" )
export DEPLOYMENT_NAME=$( $YAML2JSON < $CSV_FILE | jq --raw-output ".spec.install.spec.deployments[0].name" )
export DEPLOYMENT_SPEC=$( $YAML2JSON < $CSV_FILE | jq ".spec.install.spec.deployments[0].spec" )

log::debug "Parsed service account name: $SERVICE_ACCOUNT_NAME"

# Process all templates in lexographic order - CRD and namespace must be created first.
for INFILE in $( ls $REPO_ROOT/hack/templates/* | sort ); do
    log::info Processing $INFILE
    OUTFILE=$MANIFEST/$( basename $INFILE )

    # Fill JSON file with values from CSV
    envsubst <$INFILE > $OUTFILE

    # Replace image names
    sed -i -f $MANIFEST/.sedscript $OUTFILE

    if ! $DRYRUN; then
        oc apply -f $OUTFILE
    fi
done
