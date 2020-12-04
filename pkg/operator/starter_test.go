package operator

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestReplacementsForCustomCABundle(t *testing.T) {
	cases := []struct {
		name             string
		cm               *corev1.ConfigMap
		expectedEnvVar   string
		expectedOptional string
	}{
		{
			name:             "no configmap",
			expectedEnvVar:   "UNUSED_AWS_CA_BUNDLE",
			expectedOptional: "true",
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
			expectedEnvVar:   "UNUSED_AWS_CA_BUNDLE",
			expectedOptional: "true",
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
			expectedEnvVar:   "AWS_CA_BUNDLE",
			expectedOptional: "false",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resources := []runtime.Object{}
			if tc.cm != nil {
				resources = append(resources, tc.cm)
			}
			kubeClient := fake.NewSimpleClientset(resources...)
			actualReplaces, err := replacementsForCustomCABundle(kubeClient)()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if e, a := 2, len(actualReplaces); e != a {
				t.Errorf("unexpected number of replaces. expected=%v, got=%v", e, a)
			}
			if e, a := tc.expectedEnvVar, actualReplaces["AWS_CA_BUNDLE_ENV_VAR"]; e != a {
				t.Errorf("unexpected replacement for env var name. expected=%v, got=%v", e, a)
			}
			if e, a := tc.expectedOptional, actualReplaces["CA_BUNDLE_OPTIONAL"]; e != a {
				t.Errorf("unexpected replacement for optional value. expected=%v, got=%v", e, a)
			}
		})
	}
}
