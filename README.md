# aws-ebs-csi-driver operator

An operator to deploy the [AWS EBS CSI driver](https://github.com/openshift/aws-ebs-csi-driver) in OKD.

This operator is currently under heavy development and is not ready for general use yet.

# Quick start

Compile the operator with:

```shell
$ make build
```

Manually create the required resources with:

```shell
$ oc create -f ./manifests
```

Run the operator with:

```shell
$ ./aws-ebs-csi-driver-operator start --kubeconfig $MY_KUBECONFIG --namespace openshift-aws-ebs-csi-driver-operator
```

If you want the operator to deploy a custom AWS EBS CSI driver:

```shell
$ OPERAND_IMAGE_VERSION=0.1 OPERAND_IMAGE=quay.io/bertinatto/my-custom-aws-ebs-csi-driver ./aws-ebs-csi-driver-operator start --kubeconfig $MY_KUBECONFIG --namespace openshift-aws-ebs-csi-driver-operator
```

# TODO

## openshift/aws-ebs-csi-driver-operator

- [ ] Check why deployment and daemonset are updated when there're no changes
- [ ] Single CR through API validation of metadata.name
- [ ] Make sure there are no snapshots using the driver before removing the it
	- Right now it only checks for PVs
- [ ] Create CSV to make operator work with OLM
- [ ] Sync status when error happens while syncing resources other than Deployment and DaemonSet?
- [ ] 20 min for resyncing is OK in OLM-managed operators? Check other operators
- [ ] Add tests: unit and e2e

## openshift/library-go

- [ ] In ApplyStorageClass(), recreate Storage class if the new one changes an immutable field.
    - Currently, if we release a new version of the operator with a different StorageClass (with a different immutable field), ApplyStorageClass() will fail indefinitely
- [ ] Create function to replace `deleteAll()` from this operator
