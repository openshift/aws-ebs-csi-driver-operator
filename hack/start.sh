#!/bin/bash

# Operator startup script, mainly for e2e tests.
# - Applies everything from manifests/ directory
# - Prepares operator Deployment template from hack/start-manifests based on $IMAGE_FORMAT (if set)
# - Applies the Deployment
#

set -e

usage() {
    cat <<EOF
$0 [-d] [-h]

    -d: dry-run
    -h: help
EOF
    exit
}

dry_run=false

while getopts ":hd" opt; do
  case ${opt} in
    h ) usage
        ;;
    d )
        dry_run=true
        if [ -z "$ARTIFACT_DIR" ]; then
            echo 'ERROR: $ARTIFACT_DIR must be set in dry-run mode'
            exit 1
        fi
        ;;
    \? ) usage
        ;;
  esac
done

# Our e2e has only /bin/oc, no kubectl. At the same time, allow users to use kubectl.
if which oc &>/dev/null; then
    CLIENT=oc
else
    CLIENT=kubectl
fi

manifest=$(mktemp -d)

cleanup(){
    local return_code="$?"

    set +e

    # Save manifests for debugging if ARTIFACT_DIR is set.
    if [ -n "$ARTIFACT_DIR" ]; then
        mkdir -p $ARTIFACT_DIR/manifest
        cp ${manifest}/* $ARTIFACT_DIR/manifest/
    fi

    rm -rf ${manifest}
    exit $return_code
}

trap cleanup exit

# Interpret $IMAGE_FORMAT. Following the same format as in
# See https://github.com/openshift/origin/blob/master/test/extended/testdata/csi/README.md
# Example IMAGE_FORMAT in OCP CI: "registry.svc.ci.openshift.org/ci-op-pthpkjbt/stable:${component}"
if [ -z "${IMAGE_FORMAT}" ] ; then
    defaultOCPVersion=4.5
    echo 'WARNING: missing $IMAGE_FORMAT, using images from OCP' ${defaultOCPVersion}
    imageBasePrefix="registry.svc.ci.openshift.org/ocp/${defaultOCPVersion}:"
    imageBaseSuffix=""
else
    imageBasePrefix=$( echo ${IMAGE_FORMAT} | sed -e 's/${component}.*//' )
    imageBaseSuffix=$( echo ${IMAGE_FORMAT} | sed -e 's/.*${component}//' )
fi

cat <<EOF >${manifest}/.sedscript
s,{{.AttacherImage}},${imageBasePrefix}csi-external-attacher${imageBaseSuffix},
s,{{.ProvisionerImage}},${imageBasePrefix}csi-external-provisioner${imageBaseSuffix},
s,{{.ResizerImage}},${imageBasePrefix}csi-external-resizer${imageBaseSuffix},
s,{{.SnapshotterImage}},${imageBasePrefix}csi-external-snapshotter${imageBaseSuffix},
s,{{.NodeDriverRegistrarImage}},${imageBasePrefix}csi-node-driver-registrar${imageBaseSuffix},
s,{{.LivenessProbeImage}},${imageBasePrefix}csi-livenessprobe${imageBaseSuffix},
s,{{.ImageBasePrefix}},${imageBasePrefix},
s,{{.ImageBaseSuffix}},${imageBaseSuffix},
EOF

repo_dir="$(dirname $0)/.."

cp ${repo_dir}/manifests/*.yaml ${manifest}/

for infile in $( ls ${repo_dir}/hack/start-manifests/*.yaml ); do
    outfile=${manifest}/$( basename ${infile} )
    cp ${infile} ${outfile}
    sed -i -f ${manifest}/.sedscript ${outfile}
done

if ! ${dry_run}; then
    # CRD must be applied as a separate transaction to be able to apply CR below
    $CLIENT apply -f ${manifest}/00_crd.yaml
    $CLIENT apply -f ${manifest}/
fi
