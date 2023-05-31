package operator

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

func Test_withObservedConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  CSIDriverConfig
		want *v1.Deployment
	}{
		{
			name: "empty config, standalone cluster",
			cfg: CSIDriverConfig{
				HyperShiftConfig:     nil,
				CloudCAConfigMapName: "",
				AWSEC2Endpoint:       "",
				ExtraTags:            nil,
				Region:               "",
			},
		},
		{
			name: "hypershift cluster",
			cfg: CSIDriverConfig{
				HyperShiftConfig: &HyperShiftConfig{
					HyperShiftImage:       "foo/bar/baz",
					ClusterName:           "testcluster",
					NodeSelector:          map[string]string{"myLabel": "myValue"},
					ControlPlaneNamespace: "testcluster",
				},
				CloudCAConfigMapName: "",
				AWSEC2Endpoint:       "",
				ExtraTags:            nil,
				Region:               "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := struct {
				CSIDriverConfig *CSIDriverConfig `json:"csiDriverConfig,omitempty"`
			}{
				CSIDriverConfig: &tt.cfg,
			}
			yamlBytes, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			spec := operatorv1.OperatorSpec{
				ObservedConfig: runtime.RawExtension{
					Raw: yamlBytes,
				},
			}
			hook := withObservedConfig()
			out, err := hook(&spec, []byte(deploymentTemplate))
			if err != nil {
				t.Errorf("hook failed: %s", err)
			}
			t.Log(string(out))
			_ = resourceread.ReadDeploymentV1OrDie(out)
		})
	}
}
