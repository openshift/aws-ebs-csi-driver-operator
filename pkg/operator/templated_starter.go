package operator

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/openshift/aws-ebs-csi-driver-operator/assets2"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	configLister "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type listers struct {
	infraLister              configLister.InfrastructureLister
	configMapLister          corelisters.ConfigMapNamespaceLister
	hostedControlPlaneLister cache.GenericLister

	informersSynced []cache.InformerSynced
}

func (l listers) ResourceSyncer() resourcesynccontroller.ResourceSyncer {
	return nil
}

func (l listers) PreRunHasSynced() []cache.InformerSynced {
	return l.informersSynced
}

func (l listers) HostedControlPlaneLister() cache.GenericLister {
	return l.hostedControlPlaneLister
}

func (l listers) InfraLister() configLister.InfrastructureLister {
	return l.infraLister
}

func (l listers) ConfigMapLister() corelisters.ConfigMapNamespaceLister {
	return l.configMapLister
}

func templateReplacer(client v1helpers.OperatorClient) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		spec, _, _, err := client.GetOperatorState()
		if err != nil {
			return nil, err
		}
		observedConfigExtension := spec.ObservedConfig
		cfg := map[string]interface{}{}
		err = yaml.Unmarshal(observedConfigExtension.Raw, cfg)
		if err != nil {
			return nil, err
		}
		klog.V(4).Infof("Got observedConfig %+v", cfg)

		asset, err := assets2.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("cannot find asset %s: %s", name, err)
		}
		tmpl, err := template.New("hypershift").Parse(string(asset))
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		err = tmpl.ExecuteTemplate(&buf, "hypershift", cfg)
		return buf.Bytes(), err
	}
}

type observerController struct {
	factory.Controller
}

// NewObserverController returns a new observerController.
func NewObserverController(
	name string,
	operatorClient v1helpers.OperatorClient,
	configInformer configinformers.SharedInformerFactory,
	hcpInformer informers.GenericInformer,
	configMapInformer coreinformers.ConfigMapInformer,
	eventRecorder events.Recorder,
	isHyperShift bool,
	namespace string,
) *observerController {
	informers := []factory.Informer{
		operatorClient.Informer(),
		configInformer.Config().V1().Infrastructures().Informer(),
		hcpInformer.Informer(),
		configMapInformer.Informer(),
	}

	c := &observerController{
		Controller: configobserver.NewConfigObserver(
			operatorClient,
			eventRecorder.WithComponentSuffix("observer-controller-"+strings.ToLower(name)),
			listers{
				infraLister:              configInformer.Config().V1().Infrastructures().Lister(),
				hostedControlPlaneLister: hcpInformer.Lister(),
				configMapLister:          configMapInformer.Lister().ConfigMaps(namespace),
				informersSynced: append([]cache.InformerSynced{},
					operatorClient.Informer().HasSynced,
					configInformer.Config().V1().Infrastructures().Informer().HasSynced,
					hcpInformer.Informer().HasSynced,
					configMapInformer.Informer().HasSynced,
				),
			},
			informers,
			NewAWSObserver([]string{"AWSConfig"}, isHyperShift),
			NewHyperShiftObserver([]string{"HyperShiftConfig"}, namespace),
		),
	}

	return c
}
