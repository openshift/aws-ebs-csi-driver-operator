package operator

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated"
)

const controllerWithoutCABundle = `kind: Deployment
apiVersion: apps/v1
metadata:
  name: aws-ebs-csi-driver-controller
  namespace: openshift-cluster-csi-drivers
spec:
  selector:
    matchLabels:
      app: aws-ebs-csi-driver-controller
  serviceName: aws-ebs-csi-driver-controller
  replicas: 1
  template:
    metadata:
      labels:
        app: aws-ebs-csi-driver-controller
    spec:
      hostNetwork: true
      serviceAccount: aws-ebs-csi-driver-controller-sa
      priorityClassName: system-cluster-critical
      nodeSelector:
        node-role.kubernetes.io/master: ""
      tolerations:
        - key: CriticalAddonsOnly
          operator: Exists
        - key: node-role.kubernetes.io/master
          operator: Exists
          effect: "NoSchedule"
      containers:
        - name: csi-driver
          image: ${DRIVER_IMAGE}
          args:
            - --endpoint=$(CSI_ENDPOINT)
            - --k8s-tag-cluster-id=${CLUSTER_ID}
            - --logtostderr
            - --v=${LOG_LEVEL}
          env:
            - name: CSI_ENDPOINT
              value: unix:///var/lib/csi/sockets/pluginproxy/csi.sock
            - name: AWS_ACCESS_KEY_ID
              valueFrom:
                secretKeyRef:
                  name: ebs-cloud-credentials
                  key: aws_access_key_id
                  optional: true
            - name: AWS_SECRET_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: ebs-cloud-credentials
                  key: aws_secret_access_key
                  optional: true
            - name: AWS_SDK_LOAD_CONFIG
              value: '1'
            - name: AWS_CONFIG_FILE
              value: /var/run/secrets/aws/credentials
          ports:
            - name: healthz
              # Due to hostNetwork, this port is open on a node!
              containerPort: 10301
              protocol: TCP
          volumeMounts:
            - name: aws-credentials
              mountPath: /var/run/secrets/aws
              readOnly: true
            - name: bound-sa-token
              mountPath: /var/run/secrets/openshift/serviceaccount
              readOnly: true
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-provisioner
          image: ${PROVISIONER_IMAGE}
          args:
            - --csi-address=$(ADDRESS)
            - --default-fstype=ext4
            - --feature-gates=Topology=true
            - --extra-create-metadata=true
            - --v=${LOG_LEVEL}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-attacher
          image: ${ATTACHER_IMAGE}
          args:
            - --csi-address=$(ADDRESS)
            - --v=${LOG_LEVEL}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-resizer
          image: ${RESIZER_IMAGE}
          args:
            - --csi-address=$(ADDRESS)
            - --timeout=300s
            - --v=${LOG_LEVEL}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-snapshotter
          image: ${SNAPSHOTTER_IMAGE}
          args:
            - --csi-address=$(ADDRESS)
            - --v=${LOG_LEVEL}
          env:
          - name: ADDRESS
            value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
          - mountPath: /var/lib/csi/sockets/pluginproxy/
            name: socket-dir
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-liveness-probe
          image: ${LIVENESS_PROBE_IMAGE}
          args:
            - --csi-address=/csi/csi.sock
            - --probe-timeout=3s
            - --health-port=10301
          volumeMounts:
            - name: socket-dir
              mountPath: /csi
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
      volumes:
        - name: aws-credentials
          secret:
            secretName: ebs-cloud-credentials
        # This service account token can be used to provide identity outside the cluster.
        # For example, this token can be used with AssumeRoleWithWebIdentity to authenticate with AWS using IAM OIDC provider and STS.
        - name: bound-sa-token
          projected:
            sources:
            - serviceAccountToken:
                path: token
                audience: openshift
        - name: socket-dir
          emptyDir: {}
`

const controllerWithCABundle = `kind: Deployment
apiVersion: apps/v1
metadata:
  name: aws-ebs-csi-driver-controller
  namespace: openshift-cluster-csi-drivers
spec:
  selector:
    matchLabels:
      app: aws-ebs-csi-driver-controller
  serviceName: aws-ebs-csi-driver-controller
  replicas: 1
  template:
    metadata:
      labels:
        app: aws-ebs-csi-driver-controller
    spec:
      hostNetwork: true
      serviceAccount: aws-ebs-csi-driver-controller-sa
      priorityClassName: system-cluster-critical
      nodeSelector:
        node-role.kubernetes.io/master: ""
      tolerations:
        - key: CriticalAddonsOnly
          operator: Exists
        - key: node-role.kubernetes.io/master
          operator: Exists
          effect: "NoSchedule"
      containers:
        - name: csi-driver
          image: ${DRIVER_IMAGE}
          args:
            - --endpoint=$(CSI_ENDPOINT)
            - --k8s-tag-cluster-id=${CLUSTER_ID}
            - --logtostderr
            - --v=${LOG_LEVEL}
          env:
            - name: CSI_ENDPOINT
              value: unix:///var/lib/csi/sockets/pluginproxy/csi.sock
            - name: AWS_ACCESS_KEY_ID
              valueFrom:
                secretKeyRef:
                  name: ebs-cloud-credentials
                  key: aws_access_key_id
                  optional: true
            - name: AWS_SECRET_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: ebs-cloud-credentials
                  key: aws_secret_access_key
                  optional: true
            - name: AWS_SDK_LOAD_CONFIG
              value: '1'
            - name: AWS_CONFIG_FILE
              value: /var/run/secrets/aws/credentials
            - name: AWS_CA_BUNDLE
              value: /etc/ca/ca-bundle.pem
          ports:
            - name: healthz
              # Due to hostNetwork, this port is open on a node!
              containerPort: 10301
              protocol: TCP
          volumeMounts:
            - name: aws-credentials
              mountPath: /var/run/secrets/aws
              readOnly: true
            - name: bound-sa-token
              mountPath: /var/run/secrets/openshift/serviceaccount
              readOnly: true
            - name: ca-bundle
              mountPath: /etc/ca
              readOnly: true
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-provisioner
          image: ${PROVISIONER_IMAGE}
          args:
            - --csi-address=$(ADDRESS)
            - --default-fstype=ext4
            - --feature-gates=Topology=true
            - --extra-create-metadata=true
            - --v=${LOG_LEVEL}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-attacher
          image: ${ATTACHER_IMAGE}
          args:
            - --csi-address=$(ADDRESS)
            - --v=${LOG_LEVEL}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-resizer
          image: ${RESIZER_IMAGE}
          args:
            - --csi-address=$(ADDRESS)
            - --timeout=300s
            - --v=${LOG_LEVEL}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-snapshotter
          image: ${SNAPSHOTTER_IMAGE}
          args:
            - --csi-address=$(ADDRESS)
            - --v=${LOG_LEVEL}
          env:
          - name: ADDRESS
            value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
          - mountPath: /var/lib/csi/sockets/pluginproxy/
            name: socket-dir
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
        - name: csi-liveness-probe
          image: ${LIVENESS_PROBE_IMAGE}
          args:
            - --csi-address=/csi/csi.sock
            - --probe-timeout=3s
            - --health-port=10301
          volumeMounts:
            - name: socket-dir
              mountPath: /csi
          resources:
            requests:
              memory: 50Mi
              cpu: 10m
      volumes:
        - name: aws-credentials
          secret:
            secretName: ebs-cloud-credentials
        # This service account token can be used to provide identity outside the cluster.
        # For example, this token can be used with AssumeRoleWithWebIdentity to authenticate with AWS using IAM OIDC provider and STS.
        - name: bound-sa-token
          projected:
            sources:
            - serviceAccountToken:
                path: token
                audience: openshift
        - name: ca-bundle
          configMap:
            name: kube-cloud-config
        - name: socket-dir
          emptyDir: {}
`

func TestWithCustomCABundle(t *testing.T) {
	cases := []struct {
		name     string
		cm       *corev1.ConfigMap
		expected string
	}{
		{
			name:     "no configmap",
			expected: controllerWithoutCABundle,
		},
		{
			name: "no CA bundle in configmap",
			cm: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "openshift-config-managed",
					Name:      "kube-cloud-config",
				},
				Data: map[string]string{
					"other-key": "other-data",
				},
			},
			expected: controllerWithoutCABundle,
		},
		{
			name: "custom CA bundle",
			cm: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "openshift-config-managed",
					Name:      "kube-cloud-config",
				},
				Data: map[string]string{
					"ca-bundle.pem": "a custom bundle",
				},
			},
			expected: controllerWithCABundle,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resources := []runtime.Object{}
			if tc.cm != nil {
				resources = append(resources, tc.cm)
			}
			kubeClient := fake.NewSimpleClientset(resources...)
			actual := string(withCustomCABundle(generated.MustAsset, kubeClient)("controller.yaml"))
			if e, a := tc.expected, actual; e != a {
				t.Errorf("unexpected controller asset\nexpected:\n%s\ngot:\n%s", e, a)
			}
		})
	}
}
