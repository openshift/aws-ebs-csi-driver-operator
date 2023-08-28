package merge

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// TODO: Call GenerateAssets() only at the start of the operator, so the parsing and patching is done only once.
// TODO: add guest assets

const (
	ControllerDeploymentAssetName = "controller.yaml"
	MetricServiceAssetName        = "service.yaml"
	MetricServiceMonitorAssetName = "servicemonitor.yaml"
)

type AssetGenerator struct {
	runtimeConfig   *RuntimeConfig
	operatorConfig  *CSIDriverOperatorConfig
	replacements    []string
	generatedAssets *CSIDriverAssets
}

func NewAssetGenerator(runtimeConfig *RuntimeConfig, operatorConfig *CSIDriverOperatorConfig) *AssetGenerator {
	return &AssetGenerator{
		runtimeConfig:  runtimeConfig,
		operatorConfig: operatorConfig,
		replacements: append(runtimeConfig.Replacements,
			"${ASSET_PREFIX}", operatorConfig.AssetPrefix,
			"${ASSET_SHORT_PREFIX}", operatorConfig.AssetShortPrefix,
			"${NAMESPACE}", runtimeConfig.ControlPlaneNamespace,
		),
		generatedAssets: &CSIDriverAssets{},
	}
}

func (gen *AssetGenerator) GenerateAssets() (*CSIDriverAssets, error) {
	gen.generatedAssets = &CSIDriverAssets{
		ControllerStaticResources: make(map[string][]byte),
	}
	if err := gen.generateController(); err != nil {
		return nil, err
	}
	return gen.generatedAssets, nil
}

func (gen *AssetGenerator) generateController() error {
	if err := gen.generateDeployment(); err != nil {
		return err
	}

	if err := gen.generateMonitoringService(); err != nil {
		return err
	}

	if err := gen.collectControllerStaticAssets(); err != nil {
		return err
	}

	if err := gen.patchController(); err != nil {
		return err
	}

	return nil
}

func (gen *AssetGenerator) patchController() error {
	// Patch everything, including the CSI driver deployment.
	for _, patch := range gen.operatorConfig.ControllerConfig.AssetPatches {
		if !patch.ClusterFlavours.Has(gen.runtimeConfig.ClusterFlavour) {
			continue
		}
		switch patch.Name {
		case ControllerDeploymentAssetName:
			controllerYAML, err := applyAssetPatch(gen.generatedAssets.ControllerTemplate, patch.PatchAssetName, gen.replacements)
			if err != nil {
				return err
			}
			gen.generatedAssets.ControllerTemplate = controllerYAML
		default:
			assetYAML := gen.generatedAssets.ControllerStaticResources[patch.Name]
			if assetYAML == nil {
				return fmt.Errorf("asset %s not found to apply patch %s", patch.Name, patch.PatchAssetName)
			}
			// TODO: find out Kind:
			assetYAML, err := applyAssetPatch(assetYAML, patch.PatchAssetName, gen.replacements)
			if err != nil {
				return err
			}
			gen.generatedAssets.ControllerStaticResources[patch.Name] = assetYAML
		}
	}
	return nil
}

func (gen *AssetGenerator) generateDeployment() error {
	ctrlCfg := gen.operatorConfig.ControllerConfig
	deploymentYAML := mustReadAsset("base/controller.yaml", gen.replacements)
	var err error

	deploymentYAML, err = applyAssetPatch(deploymentYAML, ctrlCfg.DeploymentTemplateAssetName, gen.replacements)
	if err != nil {
		return err
	}

	localPortIndex := int(ctrlCfg.SidecarLocalMetricsPortStart)
	exposedPortIndex := int(ctrlCfg.SidecarExposedMetricsPortStart)
	var baseReplacements = append([]string{}, gen.replacements...)
	if ctrlCfg.LivenessProbePort > 0 {
		baseReplacements = append(baseReplacements, "${LIVENESS_PROBE_PORT}", strconv.Itoa(int(ctrlCfg.LivenessProbePort)))
	}

	// Add kube-rbac-proxy to all containers in the original DeploymentTemplateAssetName
	for i := 0; i < len(ctrlCfg.MetricsPorts); i++ {
		port := ctrlCfg.MetricsPorts[i]
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
		deploymentYAML, err = applyAssetPatch(deploymentYAML, StandaloneKubeRBACProxy, replacements)
		if err != nil {
			return err
		}
	}

	// Inject the sidecars + kube-rbac-proxies in the reverse order then.
	for i := 0; i < len(ctrlCfg.Sidecars); i++ {
		sidecar := ctrlCfg.Sidecars[i]
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
		deploymentYAML, err = addSidecar(deploymentYAML, sidecar.TemplateAssetName, replacements, sidecar.ExtraArguments, gen.runtimeConfig.ClusterFlavour, sidecar.AssetPatches)
		if err != nil {
			return err
		}
	}
	gen.generatedAssets.ControllerTemplate = deploymentYAML
	return nil
}

func (gen *AssetGenerator) generateMonitoringService() error {
	ctrlCfg := gen.operatorConfig.ControllerConfig
	serviceYAML := mustReadAsset("base/controller_metrics_service.yaml", gen.replacements)
	serviceMonitorYAML := mustReadAsset("base/controller_metrics_servicemonitor.yaml", gen.replacements)

	localPortIndex := int(ctrlCfg.SidecarLocalMetricsPortStart)
	exposedPortIndex := int(ctrlCfg.SidecarExposedMetricsPortStart)
	for i := 0; i < len(ctrlCfg.Sidecars); i++ {
		sidecar := ctrlCfg.Sidecars[i]
		if !sidecar.HasMetricsPort {
			continue
		}
		replacements := append(gen.replacements,
			"${LOCAL_METRICS_PORT}", strconv.Itoa(localPortIndex),
			"${EXPOSED_METRICS_PORT}", strconv.Itoa(exposedPortIndex),
			"${PORT_NAME}", sidecar.MetricPortName,
		)
		localPortIndex++
		exposedPortIndex++

		var err error
		serviceYAML, err = applyAssetPatch(serviceYAML, "patches/metrics/service-port.yaml", replacements)
		if err != nil {
			return err
		}
		serviceMonitorYAML, err = applyAssetPatch(serviceMonitorYAML, "patches/metrics/service-monitor-port.yaml.patch", replacements)
		if err != nil {
			return err
		}
	}

	for i := 0; i < len(ctrlCfg.MetricsPorts); i++ {
		port := ctrlCfg.MetricsPorts[i]
		replacements := append(gen.replacements,
			"${EXPOSED_METRICS_PORT}", strconv.Itoa(int(port.ExposedPort)),
			"${LOCAL_METRICS_PORT}", strconv.Itoa(int(port.LocalPort)),
			"${PORT_NAME}", port.Name,
		)
		var err error
		serviceYAML, err = applyAssetPatch(serviceYAML, "patches/metrics/service-port.yaml", replacements)
		if err != nil {
			return err
		}
		serviceMonitorYAML, err = applyAssetPatch(serviceMonitorYAML, "patches/metrics/service-monitor-port.yaml.patch", replacements)
		if err != nil {
			return err
		}
	}

	gen.generatedAssets.ControllerStaticResources[MetricServiceAssetName] = serviceYAML
	gen.generatedAssets.ControllerStaticResources[MetricServiceMonitorAssetName] = serviceMonitorYAML
	return nil
}

func (gen *AssetGenerator) collectControllerStaticAssets() error {
	ctrlCfg := gen.operatorConfig.ControllerConfig
	for _, a := range ctrlCfg.StaticAssets {
		if a.ClusterFlavours.Has(gen.runtimeConfig.ClusterFlavour) {
			assetBytes := mustReadAsset(a.AssetName, gen.replacements)
			gen.generatedAssets.ControllerStaticResources[filepath.Base(a.AssetName)] = assetBytes
		}
	}
	for _, sidecar := range ctrlCfg.Sidecars {
		for _, assetName := range sidecar.StaticAssetNames {
			assetBytes := mustReadAsset(assetName, gen.replacements)
			gen.generatedAssets.ControllerStaticResources[filepath.Base(assetName)] = assetBytes
		}
	}
	return nil
}
