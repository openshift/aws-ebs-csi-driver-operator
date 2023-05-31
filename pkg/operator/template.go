package operator

import (
	"bytes"
	"encoding/json"
	"strings"
	"text/template"

	opv1 "github.com/openshift/api/operator/v1"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	configLister "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/configobserver"
	"github.com/openshift/library-go/pkg/operator/configobserver/proxy"
	"github.com/openshift/library-go/pkg/operator/csi/csiconfigobservercontroller"
	"github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// Listers for all operator's ConfigObservers
type operatorListers struct {
	infraLister              configLister.InfrastructureLister
	proxyLister              configLister.ProxyLister
	configMapLister          corelisters.ConfigMapNamespaceLister
	hostedControlPlaneLister cache.GenericLister
	informersSynced          []cache.InformerSynced
}

func (l operatorListers) ResourceSyncer() resourcesynccontroller.ResourceSyncer {
	return nil
}

func (l operatorListers) PreRunHasSynced() []cache.InformerSynced {
	return l.informersSynced
}

func (l operatorListers) HostedControlPlaneLister() cache.GenericLister {
	return l.hostedControlPlaneLister
}

func (l operatorListers) InfraLister() configLister.InfrastructureLister {
	return l.infraLister
}

func (l operatorListers) ProxyLister() configLister.ProxyLister {
	return l.proxyLister
}

func (l operatorListers) ConfigMapLister() corelisters.ConfigMapNamespaceLister {
	return l.configMapLister
}

type observerController struct {
	factory.Controller
}

// NewObserverController creates a controller with all operator's ConfigObservers.
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
		configInformer.Config().V1().Proxies().Informer(),
		configMapInformer.Informer(),
	}

	listers := operatorListers{
		infraLister:     configInformer.Config().V1().Infrastructures().Lister(),
		proxyLister:     configInformer.Config().V1().Proxies().Lister(),
		configMapLister: configMapInformer.Lister().ConfigMaps(namespace),
		informersSynced: append([]cache.InformerSynced{},
			operatorClient.Informer().HasSynced,
			configInformer.Config().V1().Infrastructures().Informer().HasSynced,
			configInformer.Config().V1().Proxies().Informer().HasSynced,
			configMapInformer.Informer().HasSynced,
		),
	}

	observeFuncs := []configobserver.ObserveConfigFunc{
		NewAWSObserver([]string{"awsConfig"}, isHyperShift),
		proxy.NewProxyObserveFunc(csiconfigobservercontroller.ProxyConfigPath()),
	}

	if isHyperShift {
		listers.hostedControlPlaneLister = hcpInformer.Lister()
		listers.informersSynced = append(listers.informersSynced, hcpInformer.Informer().HasSynced)
		informers = append(informers, hcpInformer.Informer())
		observeFuncs = append(observeFuncs, NewHyperShiftObserver([]string{"hyperShiftConfig"}, namespace))
	}

	return &observerController{
		Controller: configobserver.NewConfigObserver(
			operatorClient,
			eventRecorder.WithComponentSuffix("observer-controller-"+strings.ToLower(name)),
			listers,
			informers,
			observeFuncs...,
		),
	}
}

func templateReplacer() deploymentcontroller.ManifestHookFunc {
	return func(spec *opv1.OperatorSpec, asset []byte) ([]byte, error) {
		observedConfigExtension := spec.ObservedConfig
		cfg := map[string]interface{}{}
		klog.V(5).Infof("Observed config passed to templates: %s", observedConfigExtension.Raw)
		err := json.Unmarshal(observedConfigExtension.Raw, &cfg)
		if err != nil {
			return nil, err
		}

		tmpl, err := template.New("operator").Parse(string(asset))
		if err != nil {
			return nil, err
		}

		// Fail when the template refers to a missing key / field in ObservedConfig.
		// This allows us to catch bugs / typos in the template, however,
		// it makes the template slightly more complicated.
		// This will error, because it refers to optional `extraTags` field:
		//   {{ if .awsConfig.extraTags }} put the tags somewhere {{ end }}
		// We must explicitly check if the field is present in .awsConfig instead:
		//   {{ if index .awsConfig "extraTags" }} put the tags somewhere {{ end }}
		tmpl.Option("missingkey=error")

		var buf bytes.Buffer
		err = tmpl.ExecuteTemplate(&buf, "operator", cfg)
		out := buf.Bytes()
		if klog.V(5).Enabled() {
			klog.V(5).Infof("Expanded template asset:\n%s", string(out))
		}
		return out, err
	}
}
