module github.com/openshift/aws-ebs-csi-driver-operator

go 1.16

require (
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/google/go-cmp v0.5.5 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/openshift/api v0.0.0-20210429235223-e5fb810477d3
	github.com/openshift/build-machinery-go v0.0.0-20210209125900-0da259a2c359
	github.com/openshift/client-go v0.0.0-20210331195552-cf6c2669e01f
	github.com/openshift/library-go v0.0.0-20210408164723-7a65fdb398e2
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.0.0-20210428140749-89ef3d95e781 // indirect
	k8s.io/api v0.21.0
	k8s.io/apimachinery v0.21.0
	k8s.io/client-go v0.21.0
	k8s.io/component-base v0.21.0
	k8s.io/klog/v2 v2.8.0
	sigs.k8s.io/structured-merge-diff/v4 v4.1.1 // indirect
)

// points to temporary-watch-reduction-patch-1.21 to pick up k/k/pull/101102 - please remove it once the pr merges and a new Z release is cut
replace k8s.io/apiserver => github.com/openshift/kubernetes-apiserver v0.0.0-20210419140141-620426e63a99
