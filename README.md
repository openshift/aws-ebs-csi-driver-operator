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
