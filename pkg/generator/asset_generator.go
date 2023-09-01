package generator

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/openshift/aws-ebs-csi-driver-operator/pkg/generated-assets"
)

type AssetGenerator struct {
	runtimeConfig   *RuntimeConfig
	operatorConfig  *CSIDriverGeneratorConfig
	replacements    []string
	generatedAssets *generated_assets.CSIDriverAssets
}

func NewAssetGenerator(runtimeConfig *RuntimeConfig, operatorConfig *CSIDriverGeneratorConfig) *AssetGenerator {
	return &AssetGenerator{
		runtimeConfig:  runtimeConfig,
		operatorConfig: operatorConfig,
		replacements: append(runtimeConfig.Replacements,
			"${ASSET_PREFIX}", operatorConfig.AssetPrefix,
			"${ASSET_SHORT_PREFIX}", operatorConfig.AssetShortPrefix,
			"${DRIVER_NAME}", operatorConfig.DriverName,
		),
		generatedAssets: &generated_assets.CSIDriverAssets{},
	}
}

func (gen *AssetGenerator) GenerateAssets() (*generated_assets.CSIDriverAssets, error) {
	if err := gen.generateController(); err != nil {
		return nil, err
	}
	if err := gen.generateGuest(); err != nil {
		return nil, err
	}
	return gen.generatedAssets, nil
}

func (gen *AssetGenerator) generateController() error {
	gen.generatedAssets = &generated_assets.CSIDriverAssets{
		ControllerStaticResources: make(map[string][]byte),
	}
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
		switch patch.SourceAssetName {
		case generated_assets.ControllerDeploymentAssetName:
			controllerYAML, err := applyAssetPatch(gen.generatedAssets.ControllerTemplate, patch.PatchAssetName, gen.replacements)
			if err != nil {
				return err
			}
			gen.generatedAssets.ControllerTemplate = controllerYAML
		default:
			assetYAML := gen.generatedAssets.ControllerStaticResources[patch.SourceAssetName]
			if assetYAML == nil {
				return fmt.Errorf("asset %s not found to apply patch %s", patch.SourceAssetName, patch.PatchAssetName)
			}
			assetYAML, err := applyAssetPatch(assetYAML, patch.PatchAssetName, gen.replacements)
			if err != nil {
				return err
			}
			gen.generatedAssets.ControllerStaticResources[patch.SourceAssetName] = assetYAML
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

	gen.generatedAssets.ControllerStaticResources[generated_assets.MetricServiceAssetName] = serviceYAML
	if gen.runtimeConfig.ClusterFlavour != FlavourHyperShift {
		// TODO: figure out monitoring on HyperShift. The operator does not have RBAC for ServiceMonitors now.
		gen.generatedAssets.ControllerStaticResources[generated_assets.MetricServiceMonitorAssetName] = serviceMonitorYAML
	}
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
	return nil
}

func (gen *AssetGenerator) generateGuest() error {
	gen.generatedAssets.GuestStaticResources = make(map[string][]byte)
	gen.generatedAssets.GuestStorageClassAssets = make(map[string][]byte)
	gen.generatedAssets.GuestVolumeSnapshotClasses = make(map[string][]byte)

	if err := gen.generateDaemonSet(); err != nil {
		return err
	}
	if err := gen.collectGuestStaticAssets(); err != nil {
		return err
	}
	if err := gen.collectGuestStorageClasses(); err != nil {
		return err
	}
	if err := gen.collectGuestVolumeSnapshotClasses(); err != nil {
		return err
	}

	if err := gen.patchGuest(); err != nil {
		return err
	}
	return nil
}

func (gen *AssetGenerator) generateDaemonSet() error {
	cfg := gen.operatorConfig.GuestConfig
	dsYAML := mustReadAsset("base/node.yaml", gen.replacements)
	var err error

	var replacements = append([]string{}, gen.replacements...)
	if cfg.LivenessProbePort > 0 {
		replacements = append(replacements, "${LIVENESS_PROBE_PORT}", strconv.Itoa(int(cfg.LivenessProbePort)))
	}

	dsYAML, err = applyAssetPatch(dsYAML, cfg.DaemonSetTemplateAssetName, replacements)
	if err != nil {
		return err
	}

	for i := 0; i < len(cfg.Sidecars); i++ {
		sidecar := cfg.Sidecars[i]
		dsYAML, err = addSidecar(dsYAML, sidecar.TemplateAssetName, replacements, sidecar.ExtraArguments, gen.runtimeConfig.ClusterFlavour, sidecar.AssetPatches)
		if err != nil {
			return err
		}
	}
	gen.generatedAssets.NodeTemplate = dsYAML
	return nil
}

func (gen *AssetGenerator) patchGuest() error {
	// Patch everything, including the CSI driver DaemonSet.
	for _, patch := range gen.operatorConfig.GuestConfig.AssetPatches {
		if !patch.ClusterFlavours.Has(gen.runtimeConfig.ClusterFlavour) {
			continue
		}
		switch patch.SourceAssetName {
		case generated_assets.NodeDaemonSetAssetName:
			dsYAML, err := applyAssetPatch(gen.generatedAssets.NodeTemplate, patch.PatchAssetName, gen.replacements)
			if err != nil {
				return err
			}
			gen.generatedAssets.NodeTemplate = dsYAML
		default:
			if assetYAML, ok := gen.generatedAssets.GuestStorageClassAssets[patch.SourceAssetName]; ok {
				assetYAML, err := applyAssetPatch(assetYAML, patch.PatchAssetName, gen.replacements)
				if err != nil {
					return err
				}
				gen.generatedAssets.GuestStorageClassAssets[patch.SourceAssetName] = assetYAML
				return nil
			}
			if assetYAML, ok := gen.generatedAssets.GuestVolumeSnapshotClasses[patch.SourceAssetName]; ok {
				assetYAML, err := applyAssetPatch(assetYAML, patch.PatchAssetName, gen.replacements)
				if err != nil {
					return err
				}
				gen.generatedAssets.GuestVolumeSnapshotClasses[patch.SourceAssetName] = assetYAML
				return nil
			}
			if assetYAML, ok := gen.generatedAssets.GuestStaticResources[patch.SourceAssetName]; ok {
				assetYAML, err := applyAssetPatch(assetYAML, patch.PatchAssetName, gen.replacements)
				if err != nil {
					return err
				}
				gen.generatedAssets.GuestStaticResources[patch.SourceAssetName] = assetYAML
				return nil
			}
			return fmt.Errorf("asset %s not found to apply patch %s", patch.SourceAssetName, patch.PatchAssetName)
		}
	}
	return nil
}

func (gen *AssetGenerator) collectGuestStaticAssets() error {
	cfg := gen.operatorConfig.GuestConfig
	for _, a := range cfg.StaticAssets {
		if a.ClusterFlavours.Has(gen.runtimeConfig.ClusterFlavour) {
			assetBytes := mustReadAsset(a.AssetName, gen.replacements)
			gen.generatedAssets.GuestStaticResources[filepath.Base(a.AssetName)] = assetBytes
		}
	}

	// Collect all guest static assets from the controller config.
	ctrlCfg := gen.operatorConfig.ControllerConfig
	for _, sidecar := range ctrlCfg.Sidecars {
		for _, assetName := range sidecar.GuestStaticAssetNames {
			assetBytes := mustReadAsset(assetName, gen.replacements)
			gen.generatedAssets.GuestStaticResources[filepath.Base(assetName)] = assetBytes
		}
	}

	return nil
}

func (gen *AssetGenerator) collectGuestStorageClasses() error {
	cfg := gen.operatorConfig.GuestConfig
	for _, assetName := range cfg.StorageClassAssetNames {
		assetBytes := mustReadAsset(assetName, gen.replacements)
		gen.generatedAssets.GuestStorageClassAssets[filepath.Base(assetName)] = assetBytes
	}
	return nil
}

func (gen *AssetGenerator) collectGuestVolumeSnapshotClasses() error {
	cfg := gen.operatorConfig.GuestConfig
	for _, assetName := range cfg.VolumeSnapshotClassAssetNames {
		assetBytes := mustReadAsset(assetName, gen.replacements)
		gen.generatedAssets.GuestVolumeSnapshotClasses[filepath.Base(assetName)] = assetBytes
	}
	return nil
}
