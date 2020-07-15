module github.com/openshift/aws-ebs-csi-driver-operator

go 1.13

require (
	bitbucket.org/ww/goautoneg v0.0.0-20120707110453-75cd24fc2f2c // indirect
	github.com/google/go-cmp v0.4.0
	github.com/jteeuwen/go-bindata v3.0.8-0.20151023091102-a0ff2567cfb7+incompatible
	github.com/konsorten/go-windows-terminal-sequences v1.0.2 // indirect
	github.com/openshift/api v0.0.0-20200701144905-de5b010b2b38
	github.com/openshift/build-machinery-go v0.0.0-20200424080330-082bf86082cc
	github.com/openshift/client-go v0.0.0-20200521150516-05eb9880269c
	github.com/openshift/library-go v0.0.0-20200715125344-100bf3ff5a19
	github.com/prometheus/client_golang v1.4.1
	github.com/spf13/cobra v0.0.6
	github.com/spf13/pflag v1.0.5
	google.golang.org/genproto v0.0.0-20191220175831-5c49e3ecc1c1 // indirect
	k8s.io/api v0.18.3
	k8s.io/apiextensions-apiserver v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/client-go v0.18.3
	k8s.io/code-generator v0.18.3
	k8s.io/component-base v0.18.3
	k8s.io/klog v1.0.0
	sigs.k8s.io/controller-runtime v0.4.0
)
