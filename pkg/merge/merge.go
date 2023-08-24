package merge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/openshift/aws-ebs-csi-driver-operator/assets"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	kjson "sigs.k8s.io/json"
	"sigs.k8s.io/yaml"
)

func GenerateAssets(flavour ClusterFlavour, cfg *CSIDriverOperatorConfig) (*CSIDriverAssets, error) {
	replacements := []string{
		"${ASSET_PREFIX}", cfg.AssetPrefix,
		"${ASSET_SHORT_PREFIX}", cfg.AssetShortPrefix,
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
	a.ControllerStaticResources["service.yaml"] = service
	a.ControllerStaticResources["servicemonitor.yaml"] = serviceMonitor

	staticAssets, err := CollectControllerStaticAssets(flavour, cfg, replacements)
	if err != nil {
		return err
	}
	for k, v := range staticAssets {
		a.ControllerStaticResources[k] = v
	}

	return nil
}

func GenerateDeployment(flavour ClusterFlavour, cfg *ControllPlaneConfig, replacements []string) ([]byte, error) {
	deploymentJSON := mustYAMLToJSON(mustReadAsset("base/controller.yaml", replacements))
	var err error

	localPortIndex := int(cfg.SidecarLocalMetricsPortStart)
	exposedPortIndex := int(cfg.SidecarExposedMetricsPortStart)
	var baseReplacements = append([]string{}, replacements...)
	if cfg.LivenessProbePort > 0 {
		baseReplacements = append(baseReplacements, "${LIVENESS_PROBE_PORT}", strconv.Itoa(int(cfg.LivenessProbePort)))
	}

	switch flavour {
	case FlavourStandalone:
		deploymentJSON, err = applyAssetPatch(deploymentJSON, "patches/standalone/controller.yaml", baseReplacements, &appv1.Deployment{})
	}

	// Strategic merge patch *prepends* new containers before the old ones.
	// Inject the sidecars + kube-rbac-proxies in the reverse order then.

	for i := len(cfg.Sidecars) - 1; i >= 0; i-- {
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
		deploymentJSON, err = applyAssetPatch(deploymentJSON, sidecar.TemplateAssetName, replacements, &appv1.Deployment{})
		if err != nil {
			return nil, err
		}
	}

	// Add kube-rbac-proxy to all containers in the original DeploymentTemplateAssetName
	for i := len(cfg.MetricsPorts) - 1; i >= 0; i-- {
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
		deploymentJSON, err = applyAssetPatch(deploymentJSON, StandaloneKubeRBACProxy, replacements, &appv1.Deployment{})
		if err != nil {
			return nil, err
		}
	}

	// Apply the CSI driver deployment template as the last one, it will prepend the CSI driver container as the first one
	deploymentJSON, err = applyAssetPatch(deploymentJSON, cfg.DeploymentTemplateAssetName, replacements, &appv1.Deployment{})
	if err != nil {
		return nil, err
	}

	return yaml.JSONToYAML(deploymentJSON)
}

func GenerateMonitoringService(flavour ClusterFlavour, cfg *ControllPlaneConfig, replacements []string) ([]byte, []byte, error) {
	serviceJSON := mustYAMLToJSON(mustReadAsset("base/controller_metrics_service.yaml", replacements))
	serviceMonitorJSON := mustYAMLToJSON(mustReadAsset("base/controller_metrics_servicemonitor.yaml", replacements))

	localPortIndex := int(cfg.SidecarLocalMetricsPortStart)
	exposedPortIndex := int(cfg.SidecarExposedMetricsPortStart)
	for i := len(cfg.Sidecars) - 1; i >= 0; i-- {
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
		serviceJSON, err = applyAssetPatch(serviceJSON, "patches/metrics/service-port.yaml", replacements, &v1.Service{})
		if err != nil {
			return nil, nil, err
		}
		serviceMonitorJSON, err = prependEndpointToServiceMonitor(serviceMonitorJSON, mustReadAsset("patches/metrics/service-monitor-port.yaml", replacements))
		if err != nil {
			return nil, nil, err
		}
	}

	for i := len(cfg.MetricsPorts) - 1; i >= 0; i-- {
		port := cfg.MetricsPorts[i]
		replacements := append(replacements,
			"${EXPOSED_METRICS_PORT}", strconv.Itoa(int(port.ExposedPort)),
			"${LOCAL_METRICS_PORT}", strconv.Itoa(int(port.LocalPort)),
			"${PORT_NAME}", port.Name,
		)
		var err error
		serviceJSON, err = applyAssetPatch(serviceJSON, "patches/metrics/service-port.yaml", replacements, &v1.Service{})
		if err != nil {
			return nil, nil, err
		}
		serviceMonitorJSON, err = prependEndpointToServiceMonitor(serviceMonitorJSON, mustReadAsset("patches/metrics/service-monitor-port.yaml", replacements))
		if err != nil {
			return nil, nil, err
		}
	}

	serviceYAML, err := yaml.JSONToYAML(serviceJSON)
	if err != nil {
		return nil, nil, err
	}
	serviceMonitorYAML, err := yaml.JSONToYAML(serviceMonitorJSON)
	if err != nil {
		return nil, nil, err
	}
	return serviceYAML, serviceMonitorYAML, nil
}

func CollectControllerStaticAssets(flavour ClusterFlavour, cfg *ControllPlaneConfig, replacements []string) (map[string][]byte, error) {
	staticAssets := make(map[string][]byte)
	for _, assetName := range cfg.StaticAssetNames {
		assetBytes := mustReadAsset(assetName, replacements)
		staticAssets[filepath.Base(assetName)] = assetBytes
	}
	for _, sidecar := range cfg.Sidecars {
		for _, assetName := range sidecar.StaticAssetNames {
			assetBytes := mustReadAsset(assetName, replacements)
			staticAssets[filepath.Base(assetName)] = assetBytes
		}
	}
	return staticAssets, nil
}

func applyAssetPatch(sourceJSON []byte, assetName string, replacements []string, kind interface{}) ([]byte, error) {
	patchBytes := mustReadAsset(assetName, replacements)
	patchJSON := mustYAMLToJSON(patchBytes)

	ret, err := strategicpatch.StrategicMergePatch(sourceJSON, patchJSON, kind)
	if err != nil {
		return nil, fmt.Errorf("failed to apply aseet %s: %v", assetName, err)
	}

	return ret, nil
}

func mustReadAsset(assetName string, replacements []string) []byte {
	assetBytes, err := assets.ReadFile(assetName)
	if err != nil {
		panic(err)
	}
	return replaceBytes(assetBytes, replacements)
}

func mustYAMLToJSON(yamlBytes []byte) []byte {
	jsonBytes, err := yaml.YAMLToJSONStrict(yamlBytes)
	if err != nil {
		panic(err)
	}
	return jsonBytes
}

func replaceBytes(src []byte, replacements []string) []byte {
	for i := 0; i < len(replacements); i += 2 {
		src = bytes.ReplaceAll(src, []byte(replacements[i]), []byte(replacements[i+1]))
	}
	return src
}

// prependEndpointToServiceMonitor prepends the given endpoint to the ServiceMonitor's list of endpoints.
// Using manual path, because ServiceMonitor does not have strategic merge patch support.
func prependEndpointToServiceMonitor(serviceMonitorJSON []byte, endpointYAML []byte) ([]byte, error) {
	serviceMonitor := &monitoringv1.ServiceMonitor{}
	if err := kjson.UnmarshalCaseSensitivePreserveInts(serviceMonitorJSON, serviceMonitor); err != nil {
		return nil, err
	}

	endpointJSON := mustYAMLToJSON(endpointYAML)
	endpoint := &monitoringv1.Endpoint{}
	if err := kjson.UnmarshalCaseSensitivePreserveInts(endpointJSON, endpoint); err != nil {
		return nil, err
	}

	// Do prepend, like strategic merge patch would do.
	serviceMonitor.Spec.Endpoints = append([]monitoringv1.Endpoint{*endpoint}, serviceMonitor.Spec.Endpoints...)
	return json.Marshal(serviceMonitor)
}
