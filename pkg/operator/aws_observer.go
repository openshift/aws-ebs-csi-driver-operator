package operator

import (
	"reflect"

	configv1 "github.com/openshift/api/config/v1"
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
	CloudCAConfigMapName string                    `json:"cloudCAConfigMapName,omitempty"`
	AWSEC2Endpoint       string                    `json:"AWSEC2Endpoint,omitempty"`
	ExtraTags            []configv1.AWSResourceTag `json:"extraTags,omitempty"`
	Region               string                    `json:"region,omitempty"`
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
	if _, ok := cloudConfigCM.Data[caBundleKey]; ok {
		config.CloudCAConfigMapName = configName
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
		config.ExtraTags = infra.Status.PlatformStatus.AWS.ResourceTags
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
		eventRecorder.Eventf("ObserveProxyConfig", "AWS config changed to %q", newConfig)
	}

	return observedConfig, errs
}
