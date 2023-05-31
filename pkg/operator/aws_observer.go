package operator

import (
	"encoding/json"
	"reflect"

	configLister "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/events"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type AWSListers interface {
	InfraLister() configLister.InfrastructureLister
	ConfigMapLister() corelisters.ConfigMapNamespaceLister
}

func NewAWSObserver(path []string, isHyperShift bool) configobserver.ObserveConfigFunc {
	observer := awsObserver{
		path:         path,
		isHyperShift: isHyperShift,
	}
	return observer.observe
}

type awsObserver struct {
	path         []string
	isHyperShift bool
}

type AWSConfig struct {
	CloudCAConfigMapName string            `json:"cloudCAConfigMapName,omitempty"`
	AWSEC2Endpoint       string            `json:"AWSEC2Endpoint,omitempty"`
	ExtraTags            map[string]string `json:"extraTags,omitempty"`
	Region               string            `json:"region,omitempty"`
}

func (h *awsObserver) observe(genericListers configobserver.Listers, eventRecorder events.Recorder, existingConfig map[string]interface{}) (map[string]interface{}, []error) {
	listers := genericListers.(AWSListers)
	errs := []error{}
	observedConfig := map[string]interface{}{}
	config := AWSConfig{}

	configName := cloudConfigName
	if h.isHyperShift {
		configName = "user-ca-bundle"
	}
	cloudConfigCM, err := listers.ConfigMapLister().Get(configName)
	if apierrors.IsNotFound(err) {
		// fall through with nil
		cloudConfigCM = nil
		err = nil
	}
	if err != nil {
		return nil, append(errs, err)
	}
	if cloudConfigCM != nil {
		if _, ok := cloudConfigCM.Data[caBundleKey]; ok {
			config.CloudCAConfigMapName = configName
		}
	}

	infra, err := listers.InfraLister().Get("cluster")
	if err != nil {
		return nil, append(errs, err)
	}
	if infra.Status.PlatformStatus != nil && infra.Status.PlatformStatus.AWS != nil {
		serviceEndPoints := infra.Status.PlatformStatus.AWS.ServiceEndpoints
		ec2EndPoint := ""
		for _, serviceEndPoint := range serviceEndPoints {
			if serviceEndPoint.Name == "ec2" {
				ec2EndPoint = serviceEndPoint.URL
			}
		}
		if ec2EndPoint != "" {
			config.AWSEC2Endpoint = ec2EndPoint
		}

		config.Region = infra.Status.PlatformStatus.AWS.Region
		if len(infra.Status.PlatformStatus.AWS.ResourceTags) > 0 {
			config.ExtraTags = make(map[string]string)
			for _, tag := range infra.Status.PlatformStatus.AWS.ResourceTags {
				config.ExtraTags[tag.Key] = tag.Value
			}
		}
	}

	// Convert to unstructured, so  SetNestedField can call runtime.DeepCopyJSONValue() on it
	j, err := json.Marshal(&config)
	if err != nil {
		return existingConfig, append(errs, err)
	}
	unstructuredConfig := map[string]interface{}{}
	err = json.Unmarshal(j, &unstructuredConfig)
	if err != nil {
		return existingConfig, append(errs, err)
	}

	if err := unstructured.SetNestedField(observedConfig, unstructuredConfig, h.path...); err != nil {
		return existingConfig, append(errs, err)
	}

	// Print any changes
	newConfig, _, err := unstructured.NestedFieldNoCopy(observedConfig, h.path...)
	if err != nil {
		errs = append(errs, err)
	}
	currentConfig, _, err := unstructured.NestedFieldNoCopy(existingConfig, h.path...)
	if err != nil {
		errs = append(errs, err)
	}

	if !reflect.DeepEqual(newConfig, currentConfig) {
		eventRecorder.Eventf("ObserveAWSConfig", "AWS config changed to %q", newConfig)
	}

	return observedConfig, errs
}
