# aws-ebs-csi-driver operator

An operator to deploy the [AWS EBS CSI driver](https://github.com/openshift/aws-ebs-csi-driver) in OKD.

This operator is currently under heavy development and is not ready for general use yet.

# Quick start

Compile the operator with:

```shell
$ make
```

Manually create the required resources with:

```shell
$ oc create -f ./manifests
```

Run the operator by running:

```shell
$ ./aws-ebs-csi-driver-operator start --kubeconfig $MY_KUBECONFIG --namespace openshift-aws-ebs-csi-driver-operator
```

# TODO

- [ ] Single CR through API validation of metadata.name
- [ ] Make sure there are no snapshots using the driver before removing the it
	- Right now it only checks for PVs
- [ ] Create CSV to make operator work with OLM
- [ ] Add fine-grained AWS permissions (in `./assets/credentials.yaml`)
	- Right now it allows everything (*ec2:\**)
- [ ] Sync status when error happens while syncing resources other than Deployment and DaemonSet?
- [ ] 20 min for resyncing is OK in OLM-managed operators? Check other operators
- [ ] Use better defaults in resources in `./assets`
    - And better/consistent resource names
- [ ] Move code to openshift org
- [ ] Add tests: unit and e2e

## openshift/library-go

- [ ] Get https://github.com/openshift/library-go/pull/750 merged
- Then revert commit fbd5b60d166dbb3727f2c8c05dc28760a9047328 here and update `openshift/library-go`
- [ ] Convert commit c8cd1a9 to a PR against to openshift/library-go
- Need to add tests as well because the whole ApplyStorageclass() function isn't tested
