package operator

import (
	"os"
	"reflect"

	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

type HostedControlPlaneLister interface {
	HostedControlPlaneLister() cache.GenericLister
}

func NewHyperShiftObserver(path []string, namespace string) configobserver.ObserveConfigFunc {
	observer := hyperShiftObserver{
		path:      path,
		namespace: namespace,
	}
	return observer.observe
}

type hyperShiftObserver struct {
	path      []string
	namespace string
}

type HyperShiftConfig struct {
	HyperShiftImage       string            `json:"hyperShiftImage,omitempty"`
	ClusterName           string            `json:"clusterName,omitempty"`
	NodeSelector          map[string]string `json:"nodeSelector,omitempty"`
	ControlPlaneNamespace string            `json:"controlPlaneNamespace,omitempty"`
}

func (h *hyperShiftObserver) observe(genericListers configobserver.Listers, eventRecorder events.Recorder, existingConfig map[string]interface{}) (map[string]interface{}, []error) {
	listers := genericListers.(HostedControlPlaneLister)
	errs := []error{}
	observedConfig := map[string]interface{}{}

	hcp, err := getHostedControlPlane(listers.HostedControlPlaneLister(), h.namespace)
	if err != nil {
		return nil, append(errs, err)
	}

	config := HyperShiftConfig{
		ControlPlaneNamespace: h.namespace,
		ClusterName:           hcp.GetName(),
		HyperShiftImage:       os.Getenv(hypershiftImageEnvName),
	}

	nodeSelector, exists, err := unstructured.NestedStringMap(hcp.UnstructuredContent(), "spec", "nodeSelector")
	if err != nil {
		return nil, append(errs, err)
	}
	if exists {
		config.NodeSelector = nodeSelector
	}

	if err := unstructured.SetNestedField(observedConfig, &config, h.path...); err != nil {
		return existingConfig, append(errs, err)
	}

	newConfig, _, err := unstructured.NestedStringMap(observedConfig, h.path...)
	if err != nil {
		errs = append(errs, err)
	}
	currentConfig, _, err := unstructured.NestedStringMap(existingConfig, h.path...)
	if err != nil {
		errs = append(errs, err)
	}

	if !reflect.DeepEqual(newConfig, currentConfig) {
		eventRecorder.Eventf("ObserveProxyConfig", "HyperShift config changed to %q", newConfig)
	}

	return observedConfig, errs
}
