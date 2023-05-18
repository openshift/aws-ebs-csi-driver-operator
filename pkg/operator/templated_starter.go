package operator

import (
	"bytes"
	"fmt"
	"text/template"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	dc "github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type CSIDriverConfig struct {
	HyperShiftConfig *HyperShiftConfig `json:"hyperShiftConfig"`

	CloudCAConfigMapName string                    `json:"cloudCAConfigMapName,omitempty"`
	AWSEC2Endpoint       string                    `json:"AWSEC2Endpoint,omitempty"`
	ExtraTags            []configv1.AWSResourceTag `json:"extraTags,omitempty"`
	Region               string                    `json:"region,omitempty"`
}

type HyperShiftConfig struct {
	HyperShiftImage       string            `json:"hyperShiftImage,omitempty"`
	ClusterName           string            `json:"clusterName,omitempty"`
	NodeSelector          map[string]string `json:"nodeSelector,omitempty"`
	ControlPlaneNamespace string            `json:"controlPlaneNamespace,omitempty"`
}

// This should be in assets/controller.yaml, I am just lazy not to put it there
const deploymentTemplate = `
kind: Deployment
apiVersion: apps/v1
metadata:
  name: aws-ebs-csi-driver-controller
  
  {{ if .HyperShiftConfig }}
  namespace: {{ .HyperShiftConfig.ControlPlaneNamespace }}
  {{ else }}
  namespace: openshift-cluster-csi-drivers
  {{ end }}

  annotations:
    {{ if not .HyperShiftConfig }}
    # Proxy is valid only on a standalone cluster
    config.openshift.io/inject-proxy: csi-driver
    config.openshift.io/inject-proxy-cabundle: csi-driver
    {{ end }}

spec:
  selector:
    matchLabels:
      app: aws-ebs-csi-driver-controller
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 0
  template:
    metadata:
      annotations:
        # This annotation allows the workload pinning feature to work when clusters are configured for it.
        # An admission webhook will look for this annotation when Pod admission occurs to modify the
        # memory and cpu resources to a custom resource name that the schedular will use to correctly 
        # assign Pods in a workload pinned cluster. This annotation will be stripped from Pods when 
        # the cluster is not configured for workload pinning.
        # See (openshift/enhancements#1213) for more info.
        target.workload.openshift.io/management: '{"effect": "PreferredDuringScheduling"}'
      labels:
        app: aws-ebs-csi-driver-controller
        {{ if .HyperShiftConfig }}
        hypershift.openshift.io/hosted-control-plane: {{ .HyperShiftConfig.ClusterName }}
        {{ end }}

    spec:
      serviceAccount: aws-ebs-csi-driver-controller-sa

      {{ if .HyperShiftConfig }}
      priorityClassName: hypershift-control-plane
      {{ else }}
      priorityClassName: system-cluster-critical
      {{ end }}

      nodeSelector:
        {{ if .HyperShiftConfig }}
        # TODO: find a better function to render a simple map.
        {{ range $k, $v := .HyperShiftConfig.NodeSelector }}
        {{ $k }}: {{ $v }}
        {{ end }}
        {{ else }}
        node-role.kubernetes.io/master: ""
        {{ end }}

      tolerations:
        {{ if .HyperShiftConfig }}
        - key: hypershift.openshift.io/control-plane
          operator: Equal
          value: "true"
          effect: "NoSchedule"
        - key: hypershift.openshift.io/cluster
          operator: Equal
          value: "{{ .HyperShiftConfig.ClusterName }}"
          effect: "NoSchedule"
        {{ else }}
        - key: CriticalAddonsOnly
          operator: Exists
        - key: node-role.kubernetes.io/master
          operator: Exists
          effect: "NoSchedule"
        {{ end }}

      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app: aws-ebs-csi-driver-controller
                topologyKey: kubernetes.io/hostname

        {{ if .HyperShiftConfig }}
        podAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchLabels:
                    hypershift.openshift.io/hosted-control-plane: {{ .HyperShiftConfig.ClusterName }}
                topologyKey: kubernetes.io/hostname
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 50
              preference:
                matchExpression:
                  key: hypershift.openshift.io/control-plane
                  operator: In 
                  values:
                    - "true"
            - weight: 100
              preference:
                matchExpression:
                  key: hypershift.openshift.io/cluster
                  operator: In 
                  values:
                    - {{ .HyperShiftConfig.ClusterName }}
        {{ end }}

      containers:
          # CSI driver container
        - name: csi-driver
          image: ${DRIVER_IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - controller
            - --endpoint=$(CSI_ENDPOINT)
            - --k8s-tag-cluster-id=${CLUSTER_ID}
            - --logtostderr
            - --http-endpoint=localhost:8206
            - --v=${LOG_LEVEL}
            {{ if .ExtraTags }}
            - --extra-tags={{range $tag := .ExtraTags }}{{ $tag.Key }}={{ $tag.Value }},{{ end }}
            {{ end }}
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
            - name: AWS_REGION
              value: {{ .Region }}
            {{ if .AWSEC2Endpoint }}
            - name: AWS_EC2_ENDPOINT
              value: {{ .AWSEC2Endpoint }}
            {{ end }}

          ports:
            - name: healthz
              # Due to hostNetwork, this port is open on a node!
              containerPort: 10301
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /healthz
              port: healthz
            initialDelaySeconds: 10
            timeoutSeconds: 3
            periodSeconds: 10
            failureThreshold: 5
          volumeMounts:
            - name: aws-credentials
              mountPath: /var/run/secrets/aws
              readOnly: true
            - name: bound-sa-token
              mountPath: /var/run/secrets/openshift/serviceaccount
              readOnly: true
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
            - name: ca-bundle
              mountPath: /etc/ca
              readOnly: true
          resources:
            requests:
              memory: 50Mi
              cpu: 10m

        {{ if not .HyperShiftConfig }}
          # kube-rbac-proxy for csi-driver container.
          # Provides https proxy for http-based csi-driver metrics.
        - name: driver-kube-rbac-proxy
          args:
          - --secure-listen-address=0.0.0.0:9206
          - --upstream=http://127.0.0.1:8206/
          - --tls-cert-file=/etc/tls/private/tls.crt
          - --tls-private-key-file=/etc/tls/private/tls.key
          - --tls-cipher-suites=${TLS_CIPHER_SUITES}
          - --logtostderr=true
          image: ${KUBE_RBAC_PROXY_IMAGE}
          imagePullPolicy: IfNotPresent
          ports:
          - containerPort: 9206
            name: driver-m
            protocol: TCP
          resources:
            requests:
              memory: 20Mi
              cpu: 10m
          volumeMounts:
          - mountPath: /etc/tls/private
            name: metrics-serving-cert
        {{ end }}

          # external-provisioner container
        - name: csi-provisioner
          image: ${PROVISIONER_IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - --csi-address=$(ADDRESS)
            - --default-fstype=ext4
            - --feature-gates=Topology=true
            - --extra-create-metadata=true
            - --http-endpoint=localhost:8202
            - --leader-election
            - --leader-election-lease-duration=${LEADER_ELECTION_LEASE_DURATION}
            - --leader-election-renew-deadline=${LEADER_ELECTION_RENEW_DEADLINE}
            - --leader-election-retry-period=${LEADER_ELECTION_RETRY_PERIOD}
            - --leader-election-namespace=openshift-cluster-csi-drivers
            - --v=${LOG_LEVEL}
            {{ if .HyperShiftConfig }}
            - --kubeconfig=/etc/hosted-kubernetes/kubeconfig
            {{ end }}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
            {{ if .HyperShiftConfig }}
            - name: hosted-kubeconfig
              mountPath: /etc/hosted-kubernetes
              readOnly: true
            {{ end }}
          resources:
            requests:
              memory: 50Mi
              cpu: 10m

        {{ if not .HyperShiftConfig }}
          # kube-rbac-proxy for external-provisioner container.
          # Provides https proxy for http-based external-provisioner metrics.
        - name: provisioner-kube-rbac-proxy
          args:
          - --secure-listen-address=0.0.0.0:9202
          - --upstream=http://127.0.0.1:8202/
          - --tls-cert-file=/etc/tls/private/tls.crt
          - --tls-private-key-file=/etc/tls/private/tls.key
          - --tls-cipher-suites=${TLS_CIPHER_SUITES}
          - --logtostderr=true
          image: ${KUBE_RBAC_PROXY_IMAGE}
          imagePullPolicy: IfNotPresent
          ports:
          - containerPort: 9202
            name: provisioner-m
            protocol: TCP
          resources:
            requests:
              memory: 20Mi
              cpu: 10m
          volumeMounts:
          - mountPath: /etc/tls/private
            name: metrics-serving-cert
          # external-attacher container
        {{ end }}

        - name: csi-attacher
          image: ${ATTACHER_IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - --csi-address=$(ADDRESS)
            - --http-endpoint=localhost:8203
            - --leader-election
            - --leader-election-lease-duration=${LEADER_ELECTION_LEASE_DURATION}
            - --leader-election-renew-deadline=${LEADER_ELECTION_RENEW_DEADLINE}
            - --leader-election-retry-period=${LEADER_ELECTION_RETRY_PERIOD}
            - --leader-election-namespace=openshift-cluster-csi-drivers
            - --v=${LOG_LEVEL}
            {{ if .HyperShiftConfig }}
            - --kubeconfig=/etc/hosted-kubernetes/kubeconfig
            {{ end }}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
            {{ if .HyperShiftConfig }}
            - name: hosted-kubeconfig
              mountPath: /etc/hosted-kubernetes
              readOnly: true
            {{ end }}
          resources:
            requests:
              memory: 50Mi
              cpu: 10m

        {{ if not .HyperShiftConfig }}
        - name: attacher-kube-rbac-proxy
          args:
          - --secure-listen-address=0.0.0.0:9203
          - --upstream=http://127.0.0.1:8203/
          - --tls-cert-file=/etc/tls/private/tls.crt
          - --tls-private-key-file=/etc/tls/private/tls.key
          - --tls-cipher-suites=${TLS_CIPHER_SUITES}
          - --logtostderr=true
          image: ${KUBE_RBAC_PROXY_IMAGE}
          imagePullPolicy: IfNotPresent
          ports:
          - containerPort: 9203
            name: attacher-m
            protocol: TCP
          resources:
            requests:
              memory: 20Mi
              cpu: 10m
          volumeMounts:
          - mountPath: /etc/tls/private
            name: metrics-serving-cert
        {{ end }}

          # external-resizer container
        - name: csi-resizer
          image: ${RESIZER_IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - --csi-address=$(ADDRESS)
            - --timeout=300s
            - --http-endpoint=localhost:8204
            - --leader-election
            - --leader-election-lease-duration=${LEADER_ELECTION_LEASE_DURATION}
            - --leader-election-renew-deadline=${LEADER_ELECTION_RENEW_DEADLINE}
            - --leader-election-retry-period=${LEADER_ELECTION_RETRY_PERIOD}
            - --leader-election-namespace=openshift-cluster-csi-drivers
            - --v=${LOG_LEVEL}
            {{ if .HyperShiftConfig }}
            - --kubeconfig=/etc/hosted-kubernetes/kubeconfig
            {{ end }}
          env:
            - name: ADDRESS
              value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
            - name: socket-dir
              mountPath: /var/lib/csi/sockets/pluginproxy/
            {{ if .HyperShiftConfig }}
            - name: hosted-kubeconfig
              mountPath: /etc/hosted-kubernetes
              readOnly: true
            {{ end }}
          resources:
            requests:
              memory: 50Mi
              cpu: 10m

        {{ if not .HyperShiftConfig }}
        - name: resizer-kube-rbac-proxy
          args:
          - --secure-listen-address=0.0.0.0:9204
          - --upstream=http://127.0.0.1:8204/
          - --tls-cert-file=/etc/tls/private/tls.crt
          - --tls-private-key-file=/etc/tls/private/tls.key
          - --tls-cipher-suites=${TLS_CIPHER_SUITES}
          - --logtostderr=true
          image: ${KUBE_RBAC_PROXY_IMAGE}
          imagePullPolicy: IfNotPresent
          ports:
          - containerPort: 9204
            name: resizer-m
            protocol: TCP
          resources:
            requests:
              memory: 20Mi
              cpu: 10m
          volumeMounts:
          - mountPath: /etc/tls/private
            name: metrics-serving-cert
        {{ end }}

          # external-snapshotter container
        - name: csi-snapshotter
          image: ${SNAPSHOTTER_IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - --csi-address=$(ADDRESS)
            - --metrics-address=localhost:8205
            - --leader-election
            - --leader-election-lease-duration=${LEADER_ELECTION_LEASE_DURATION}
            - --leader-election-renew-deadline=${LEADER_ELECTION_RENEW_DEADLINE}
            - --leader-election-retry-period=${LEADER_ELECTION_RETRY_PERIOD}
            - --leader-election-namespace=openshift-cluster-csi-drivers
            - --v=${LOG_LEVEL}
            {{ if .HyperShiftConfig }}
            - --kubeconfig=/etc/hosted-kubernetes/kubeconfig
            {{ end }}
          env:
          - name: ADDRESS
            value: /var/lib/csi/sockets/pluginproxy/csi.sock
          volumeMounts:
          - mountPath: /var/lib/csi/sockets/pluginproxy/
            name: socket-dir
          {{ if .HyperShiftConfig }}
          - name: hosted-kubeconfig
            mountPath: /etc/hosted-kubernetes
            readOnly: true
          {{ end }}
          resources:
            requests:
              memory: 50Mi
              cpu: 10m

        {{ if not .HyperShiftConfig }}
        - name: snapshotter-kube-rbac-proxy
          args:
          - --secure-listen-address=0.0.0.0:9205
          - --upstream=http://127.0.0.1:8205/
          - --tls-cert-file=/etc/tls/private/tls.crt
          - --tls-private-key-file=/etc/tls/private/tls.key
          - --tls-cipher-suites=${TLS_CIPHER_SUITES}
          - --logtostderr=true
          image: ${KUBE_RBAC_PROXY_IMAGE}
          imagePullPolicy: IfNotPresent
          ports:
          - containerPort: 9205
            name: snapshotter-m
            protocol: TCP
          resources:
            requests:
              memory: 20Mi
              cpu: 10m
          volumeMounts:
          - mountPath: /etc/tls/private
            name: metrics-serving-cert
        {{ end }}

        - name: csi-liveness-probe
          image: ${LIVENESS_PROBE_IMAGE}
          imagePullPolicy: IfNotPresent
          args:
            - --csi-address=/csi/csi.sock
            - --probe-timeout=3s
            - --health-port=10301
            - --v=${LOG_LEVEL}
          volumeMounts:
            - name: socket-dir
              mountPath: /csi
          resources:
            requests:
              memory: 50Mi
              cpu: 10m

        {{ if .HyperShiftConfig }}
        - name: token-minter
          image: {{ .HyperShiftConfig.HyperShiftImage }}
          imagePullPolicy: IfNotPresent
          command:
            - /usr/bin/control-plane-operator
          args:
            - token-minter
            - --service-account-namespace=openshift-cluster-csi-drivers 
            - --service-account-name=aws-ebs-csi-driver-controller-sa
            - --token-audience=openshift
            - --token-file=/var/run/secrets/openshift/serviceaccount/token
            - --kubeconfig=/etc/hosted-kubernetes/kubeconfig
          volumeMounts:
            - name: bound-sa-token
              mountPath: /var/run/secrets/openshift/serviceaccount
            - name: hosted-kubeconfig
              mountPath: /etc/hosted-kubernetes
              readOnly: true
          resources:
            requests:
              memory: 10Mi
              cpu: 10m

        {{ end }}

      volumes:
        - name: aws-credentials
          secret:
            secretName: ebs-cloud-credentials

        # This service account token can be used to provide identity outside the cluster.
        # For example, this token can be used with AssumeRoleWithWebIdentity to authenticate with AWS using IAM OIDC provider and STS.
        - name: bound-sa-token
          {{ if .HyperShiftConfig }}
          emptyDir:
            medium: Memory
          {{ else }}
          projected:
            sources:
            - serviceAccountToken:
                path: token
                audience: openshift
          {{ end }}

        - name: ca-bundle
          configMap:
            name: {{ .CloudCAConfigMapName }}

        - name: socket-dir
          emptyDir: {}

        {{ if not .HyperShiftConfig }}
        - name: metrics-serving-cert
          secret:
            secretName: aws-ebs-csi-driver-controller-metrics-serving-cert
        {{ end }}

        {{ if .HyperShiftConfig }}
        - name: hosted-kubeconfig
          secret:
            # TODO: use a better kubeconfig
            secretName: service-network-admin-kubeconfig
        {{ end }}
`

func withObservedConfig() dc.ManifestHookFunc {
	return func(spec *operatorv1.OperatorSpec, manifest []byte) ([]byte, error) {
		observedConfigExtension := spec.ObservedConfig
		cfg := struct {
			CSIDriverConfig *CSIDriverConfig `json:"csiDriverConfig,omitempty"`
		}{}
		err := yaml.Unmarshal(observedConfigExtension.Raw, &cfg)
		if err != nil {
			return nil, err
		}

		if cfg.CSIDriverConfig == nil {
			// no deployment
			return nil, fmt.Errorf("No observed config yet")
		}

		tmpl, err := template.New("deployment").Parse(string(manifest))
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		err = tmpl.ExecuteTemplate(&buf, "deployment", cfg.CSIDriverConfig)
		return buf.Bytes(), err
	}
}
