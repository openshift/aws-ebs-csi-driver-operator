FROM registry.svc.ci.openshift.org/openshift/release:golang-1.13 AS builder
WORKDIR /go/src/github.com/openshift/aws-ebs-csi-driver-operator
COPY . .
RUN make

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/aws-ebs-csi-driver-operator/aws-ebs-csi-driver-operator /usr/bin/
COPY manifests /manifests
ENTRYPOINT ["/usr/bin/aws-ebs-csi-driver-operator"]
LABEL com.redhat.delivery.appregistry=true
LABEL io.k8s.display-name="OpenShift aws-ebs-csi-driver-" \
      io.k8s.description="The aws-ebs-csi-driver-operator installs and maintains the AWS EBS CSI Driver on a cluster."
