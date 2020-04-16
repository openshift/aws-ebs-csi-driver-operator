# aws-ebs-csi-driver operator

An operator to deploy the [AWS EBS CSI driver](https://github.com/openshift/aws-ebs-csi-driver) in OKD.

This operator is currently under heavy development and is not ready for general use yet.

# How to use this operator with OLM by building locally

1. Build a operator image using following command:

```
~> make image-aws-ebs-csi-driver-operator
```

tag and push this image to quay.io

2. Since the CSV in `bundle/manifests` directory contains a hardcoded reference to particular image version. Replace that field with image you tagged above.
3. Now to build the bundle manifest image:

```
~> cd bundle
~> opm alpha bundle build --directory . --tag quay.io/gnufied/ebs-csi-driver-operator-manifest:0.0.1 --package aws-ebs-csi-driver-operator --channels preview --default preview
```

This will give us an image called `quay.io/gnufied/ebs-csi-driver-operator-manifest:0.0.1` which can be pushed to quay.io

4. Now lets validate if everything is right with the image:


```
~> cd bundle
~> opm alpha bundle validate --tag quay.io/gnufied/ebs-csi-driver-operator-manifest:0.0.1
```

5. Lets add this bundle image to a index image:


```
~> opm index add --bundles quay.io/gnufied/ebs-csi-driver-operator-manifest:0.0.1 --tag quay.io/gnufied/olm-index:1.0.0 --container-tool docker
```

Where replace index image name and version with your choice. This should give us a index image which can be pushed to quay. Don't forget to
make your images public.

6. Using the index image with OLM:

We can apply following YAML to install the operator:


```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-aws-ebs-csi-driver-operator
---
apiVersion: operators.coreos.com/v1alpha2
kind: OperatorGroup
metadata:
  name: aws-driver-operator-group
  namespace: openshift-aws-ebs-csi-driver-operator
  spec:
    targetNamespaces:
    - openshift-aws-ebs-csi-driver-operator

---

apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: aws-driver-manifests
  namespace: openshift-aws-ebs-csi-driver-operator
spec:
  sourceType: grpc
  image: quay.io/gnufied/olm-index:1.0.0

---

apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: aws-driver-subscription
  namespace: openshift-aws-ebs-csi-driver-operator
spec:
  channel: preview
  name: aws-ebs-csi-driver-operator
  source: aws-driver-manifests
  sourceNamespace: openshift-aws-ebs-csi-driver-operator
```

Where you can replace image-index with version you built.

# Quick start

If you are in a hurry and want to just install the operator you can simply apply following YAML:


```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: openshift-aws-ebs-csi-driver-operator
---
apiVersion: operators.coreos.com/v1alpha2
kind: OperatorGroup
metadata:
  name: aws-driver-operator-group
  namespace: openshift-aws-ebs-csi-driver-operator
  spec:
    targetNamespaces:
    - openshift-aws-ebs-csi-driver-operator

---

apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: aws-driver-manifests
  namespace: openshift-aws-ebs-csi-driver-operator
spec:
  sourceType: grpc
  image: quay.io/gnufied/olm-index:1.0.0

---

apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: aws-driver-subscription
  namespace: openshift-aws-ebs-csi-driver-operator
spec:
  channel: preview
  name: aws-ebs-csi-driver-operator
  source: aws-driver-manifests
  sourceNamespace: openshift-aws-ebs-csi-driver-operator
```

After operator is installed you can create cluster CR via:

```yaml
apiVersion: ebs.aws.csi.openshift.io/v1alpha1
kind: Driver
metadata:
  name: cluster
  namespace: openshift-aws-ebs-csi-driver-operator
spec:
  managementState: Managed
```
