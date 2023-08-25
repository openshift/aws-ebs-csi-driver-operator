package merge

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/openshift/aws-ebs-csi-driver-operator/assets"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/kustomize/kyaml/yaml"
	"sigs.k8s.io/kustomize/kyaml/yaml/merge2"
	sigyaml "sigs.k8s.io/yaml"
)

// TODO: use kustomize and yamls instead of constant yaml -> json -> yaml conversions for strategic merge patching?
// TODO: Call GenerateAssets() only at the start of the operator, so the parsing and patching is done only once.
// TODO: add guest assets

func GenerateAssets(flavour ClusterFlavour, cfg *CSIDriverOperatorConfig) (*CSIDriverAssets, error) {
	replacements := []string{
		"${ASSET_PREFIX}", cfg.AssetPrefix,
		"${ASSET_SHORT_PREFIX}", cfg.AssetShortPrefix,
		// TODO: set namespace from somewhere
		// TODO: set images and other env. var replacement?
	}

	a := &CSIDriverAssets{}
	if err := GenerateController(a, flavour, cfg.ControllerConfig, replacements); err != nil {
		return nil, err
	}
	return a, nil
}

func GenerateController(a *CSIDriverAssets, flavour ClusterFlavour, cfg *ControllPlaneConfig, replacements []string) error {
	var err error
	a.ControllerTemplate, err = GenerateDeployment(flavour, cfg, replacements)
	if err != nil {
		return err
	}

	service, serviceMonitor, err := GenerateMonitoringService(flavour, cfg, replacements)
	if err != nil {
		return err
	}

	if a.ControllerStaticResources == nil {
		a.ControllerStaticResources = make(map[string][]byte)
	}
	// TODO: use constants!
	a.ControllerStaticResources["service.yaml"] = service
	a.ControllerStaticResources["servicemonitor.yaml"] = serviceMonitor

	staticAssets, err := CollectControllerStaticAssets(flavour, cfg, replacements)
	if err != nil {
		return err
	}
	for k, v := range staticAssets {
		a.ControllerStaticResources[k] = v
	}

	// Patch everything, including the CSI driver deployment.
	for _, patch := range cfg.AssetPatches {
		if patch.ClusterFlavour != nil && *patch.ClusterFlavour != flavour {
			continue
		}
		switch patch.Name {
		case "controller.yaml":
			controllerYAML, err := applyAssetPatch(a.ControllerTemplate, patch.PatchAssetName, replacements, &appv1.Deployment{})
			if err != nil {
				return err
			}
			a.ControllerTemplate = controllerYAML
		default:
			assetYAML := a.ControllerStaticResources[patch.Name]
			if assetYAML == nil {
				return fmt.Errorf("asset %s not found to apply patch %s", patch.Name, patch.PatchAssetName)
			}
			// TODO: find out Kind:
			assetYAML, err = applyAssetPatch(assetYAML, patch.PatchAssetName, replacements, &v1.ServiceAccount{})
			a.ControllerStaticResources[patch.Name] = assetYAML
		}
	}

	return nil
}

func GenerateDeployment(flavour ClusterFlavour, cfg *ControllPlaneConfig, replacements []string) ([]byte, error) {
	deploymentYAML := mustReadAsset("base/controller.yaml", replacements)
	var err error

	deploymentYAML, err = applyAssetPatch(deploymentYAML, cfg.DeploymentTemplateAssetName, replacements, &appv1.Deployment{})
	if err != nil {
		return nil, err
	}

	localPortIndex := int(cfg.SidecarLocalMetricsPortStart)
	exposedPortIndex := int(cfg.SidecarExposedMetricsPortStart)
	var baseReplacements = append([]string{}, replacements...)
	if cfg.LivenessProbePort > 0 {
		baseReplacements = append(baseReplacements, "${LIVENESS_PROBE_PORT}", strconv.Itoa(int(cfg.LivenessProbePort)))
	}

	// Add kube-rbac-proxy to all containers in the original DeploymentTemplateAssetName
	for i := 0; i < len(cfg.MetricsPorts); i++ {
		port := cfg.MetricsPorts[i]
		if !port.InjectKubeRBACProxy {
			continue
		}
		replacements := append([]string{}, baseReplacements...)
		replacements = append(replacements,
			"${LOCAL_METRICS_PORT}", strconv.Itoa(int(port.LocalPort)),
			"${EXPOSED_METRICS_PORT}", strconv.Itoa(int(port.ExposedPort)),
			"${PORT_NAME}", port.Name,
		)
		localPortIndex++
		exposedPortIndex++
		deploymentYAML, err = applyAssetPatch(deploymentYAML, StandaloneKubeRBACProxy, replacements, &appv1.Deployment{})
		if err != nil {
			return nil, err
		}
	}

	// Inject the sidecars + kube-rbac-proxies in the reverse order then.
	for i := 0; i < len(cfg.Sidecars); i++ {
		sidecar := cfg.Sidecars[i]
		replacements := append([]string{}, baseReplacements...)
		if sidecar.HasMetricsPort {
			replacements = append(replacements,
				"${LOCAL_METRICS_PORT}", strconv.Itoa(localPortIndex),
				"${EXPOSED_METRICS_PORT}", strconv.Itoa(exposedPortIndex),
				"${PORT_NAME}", sidecar.MetricPortName,
			)
			localPortIndex++
			exposedPortIndex++
		}
		deploymentYAML, err = applySidecarPatch(deploymentYAML, sidecar.TemplateAssetName, replacements, sidecar.ExtraArguments, &appv1.Deployment{})
		if err != nil {
			return nil, err
		}
	}

	return deploymentYAML, nil
}

func GenerateMonitoringService(flavour ClusterFlavour, cfg *ControllPlaneConfig, replacements []string) ([]byte, []byte, error) {
	serviceYAML := mustReadAsset("base/controller_metrics_service.yaml", replacements)
	serviceMonitorYAML := mustReadAsset("base/controller_metrics_servicemonitor.yaml", replacements)

	localPortIndex := int(cfg.SidecarLocalMetricsPortStart)
	exposedPortIndex := int(cfg.SidecarExposedMetricsPortStart)
	for i := 0; i < len(cfg.Sidecars); i++ {
		sidecar := cfg.Sidecars[i]
		if !sidecar.HasMetricsPort {
			continue
		}
		replacements := append(replacements,
			"${LOCAL_METRICS_PORT}", strconv.Itoa(localPortIndex),
			"${EXPOSED_METRICS_PORT}", strconv.Itoa(exposedPortIndex),
			"${PORT_NAME}", sidecar.MetricPortName,
		)
		localPortIndex++
		exposedPortIndex++

		var err error
		serviceYAML, err = applyAssetPatch(serviceYAML, "patches/metrics/service-port.yaml", replacements, &v1.Service{})
		if err != nil {
			return nil, nil, err
		}
		serviceMonitorYAML, err = addEndpointToServiceMonitor(serviceMonitorYAML, mustReadAsset("patches/metrics/service-monitor-port.yaml", replacements))
		if err != nil {
			return nil, nil, err
		}
	}

	for i := 0; i < len(cfg.MetricsPorts); i++ {
		port := cfg.MetricsPorts[i]
		replacements := append(replacements,
			"${EXPOSED_METRICS_PORT}", strconv.Itoa(int(port.ExposedPort)),
			"${LOCAL_METRICS_PORT}", strconv.Itoa(int(port.LocalPort)),
			"${PORT_NAME}", port.Name,
		)
		var err error
		serviceYAML, err = applyAssetPatch(serviceYAML, "patches/metrics/service-port.yaml", replacements, &v1.Service{})
		if err != nil {
			return nil, nil, err
		}
		serviceMonitorYAML, err = addEndpointToServiceMonitor(serviceMonitorYAML, mustReadAsset("patches/metrics/service-monitor-port.yaml", replacements))
		if err != nil {
			return nil, nil, err
		}
	}

	return serviceYAML, serviceMonitorYAML, nil
}

func CollectControllerStaticAssets(flavour ClusterFlavour, cfg *ControllPlaneConfig, replacements []string) (map[string][]byte, error) {
	staticAssets := make(map[string][]byte)
	for _, a := range cfg.StaticAssets {
		if a.ClusterFlavour == nil || *a.ClusterFlavour == flavour {
			assetBytes := mustReadAsset(a.AssetName, replacements)
			staticAssets[filepath.Base(a.AssetName)] = assetBytes
		}
	}
	for _, sidecar := range cfg.Sidecars {
		for _, assetName := range sidecar.StaticAssetNames {
			assetBytes := mustReadAsset(assetName, replacements)
			staticAssets[filepath.Base(assetName)] = assetBytes
		}
	}
	return staticAssets, nil
}

func applyAssetPatch(sourceYAML []byte, assetName string, replacements []string, kind interface{}) ([]byte, error) {
	patchYAML := mustReadAsset(assetName, replacements)
	opts := yaml.MergeOptions{ListIncreaseDirection: yaml.MergeOptionsListAppend}
	ret, err := merge2.MergeStrings(string(patchYAML), string(sourceYAML), false, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply aseet %s: %v", assetName, err)
	}
	return []byte(ret), nil
}

func applySidecarPatch(sourceYAML []byte, assetName string, replacements []string, extraArguments []string, kind interface{}) ([]byte, error) {
	patchYAML := mustReadAsset(assetName, replacements)
	// set extra arguments
	if len(extraArguments) > 0 {
		patch, err := yaml.Parse(string(patchYAML))
		if err != nil {
			return nil, fmt.Errorf("failed to read asset %s: %v", assetName, err)
		}
		args, err := patch.GetSlice("spec.template.spec.containers[0].args")
		if err != nil {
			return nil, fmt.Errorf("failed to get arguments from %s: %v", assetName, err)
		}
		finalArgs := []string{}
		for _, arg := range args {
			finalArgs = append(finalArgs, arg.(string))
		}
		finalArgs = append(finalArgs, extraArguments...)
		patch.SetMapField(yaml.NewListRNode(finalArgs...), "spec", "template", "spec", "containers", "0", "args")
		patchYAMLString, err := patch.String()
		if err != nil {
			return nil, fmt.Errorf("failed to assemble asset %s with extra args: %v", assetName, err)
		}
		patchYAML = []byte(patchYAMLString)
	}
	opts := yaml.MergeOptions{ListIncreaseDirection: yaml.MergeOptionsListAppend}
	ret, err := merge2.MergeStrings(string(patchYAML), string(sourceYAML), false, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply asset %s: %v", assetName, err)
	}
	return []byte(ret), nil
}

func mustReadAsset(assetName string, replacements []string) []byte {
	assetBytes, err := assets.ReadFile(assetName)
	if err != nil {
		panic(err)
	}
	return replaceBytes(assetBytes, replacements)
}

func replaceBytes(src []byte, replacements []string) []byte {
	for i := 0; i < len(replacements); i += 2 {
		src = bytes.ReplaceAll(src, []byte(replacements[i]), []byte(replacements[i+1]))
	}
	return src
}

// addEndpointToServiceMonitor adds the given endpoint to the ServiceMonitor's list of endpoints.
// Using manual path, because ServiceMonitor does not have strategic merge patch support.
// TODO: use json patch instead of custom func?
func addEndpointToServiceMonitor(serviceMonitorYAML []byte, endpointYAML []byte) ([]byte, error) {
	serviceMonitor := &monitoringv1.ServiceMonitor{}
	if err := sigyaml.UnmarshalStrict(serviceMonitorYAML, serviceMonitor); err != nil {
		return nil, err
	}

	endpoint := &monitoringv1.Endpoint{}
	if err := sigyaml.UnmarshalStrict(endpointYAML, endpoint); err != nil {
		return nil, err
	}

	serviceMonitor.Spec.Endpoints = append(serviceMonitor.Spec.Endpoints, *endpoint)
	return sigyaml.Marshal(serviceMonitor)
}

func MustSanitize(src string) string {
	sanitized, err := Sanitize(src)
	if err != nil {
		panic(err)
	}

	return sanitized
}

func Sanitize(src string) (string, error) {
	var obj interface{}
	sigyaml.Unmarshal([]byte(src), &obj)
	bytes, err := sigyaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
