YAML files to run the operator from command line.

# Usage:

```sh
$ make
$ kubectl apply -f manifests/
$ ./aws-ebs-csi-driver-operator start -v 5 --alsologtostderr --kubeconfig=$KUBECONFIG --namespace openshift-aws-ebs-csi-driver-operator
