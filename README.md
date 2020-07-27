# aws-ebs-csi-driver operator

An operator to deploy the [AWS EBS CSI driver](https://github.com/openshift/aws-ebs-csi-driver) in OKD.

This operator is installed by the [cluster-storage-operator](https://github.com/openshift/cluster-storage-operator).

# Quick start

To build and run the operator locally:

```shell
# Create only the resources the operator needs to run via CLI
oc apply -f manifests/00_crd.yaml
oc apply -f manifests/01_namespace.yaml
oc apply -f manifests/08_cr.yaml

# Build the operator
make

# Set the environment variables
export DRIVER_IMAGE=quay.io/openshift/origin-aws-ebs-csi-driver:latest
export PROVISIONER_IMAGE=quay.io/openshift/origin-csi-external-provisioner:latest
export ATTACHER_IMAGE=quay.io/openshift/origin-csi-external-attacher:latest
export RESIZER_IMAGE=quay.io/openshift/origin-csi-external-resizer:latest
export SNAPSHOTTER_IMAGE=quay.io/openshift/origin-csi-external-snapshotter:latest
export NODE_DRIVER_REGISTRAR_IMAGE=quay.io/openshift/origin-csi-node-driver-registrar:latest
export LIVENESS_PROBE_IMAGE=quay.io/openshift/origin-csi-livenessprobe:latest

# Run the operator via CLI
./aws-ebs-csi-driver-operator start --kubeconfig $MY_KUBECONFIG --namespace openshift-cluster-csi-drivers
```

To run the latest build of the operator:

```shell
# Set the environment variables
export DRIVER_IMAGE=quay.io/openshift/origin-aws-ebs-csi-driver:latest
export PROVISIONER_IMAGE=quay.io/openshift/origin-csi-external-provisioner:latest
export ATTACHER_IMAGE=quay.io/openshift/origin-csi-external-attacher:latest
export RESIZER_IMAGE=quay.io/openshift/origin-csi-external-resizer:latest
export SNAPSHOTTER_IMAGE=quay.io/openshift/origin-csi-external-snapshotter:latest
export NODE_DRIVER_REGISTRAR_IMAGE=quay.io/openshift/origin-csi-node-driver-registrar:latest
export LIVENESS_PROBE_IMAGE=quay.io/openshift/origin-csi-livenessprobe:latest

# Deploy the operator and everything it needs
oc apply -f manifests/
```
