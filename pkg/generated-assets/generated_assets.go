package generated_assets

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
)

const (
	manifestFileName              = "manifests.yaml"
	ControllerDeploymentAssetName = "controller.yaml"
	NodeDaemonSetAssetName        = "node.yaml"
	MetricServiceAssetName        = "service.yaml"
	MetricServiceMonitorAssetName = "servicemonitor.yaml"
)

type CSIDriverAssets struct {
	ControllerTemplate         []byte
	ControllerStaticResources  map[string][]byte
	NodeTemplate               []byte
	GuestStaticResources       map[string][]byte
	GuestStorageClassAssets    map[string][]byte
	GuestVolumeSnapshotClasses map[string][]byte

	replacer *strings.Replacer
}

func (a *CSIDriverAssets) GetAsset(assetName string) ([]byte, error) {

	asset, err := a.getRawAsset(assetName)
	if err != nil {
		return nil, err
	}
	if a.replacer == nil {
		return asset, nil
	}
	assetString := a.replacer.Replace(string(asset))
	return []byte(assetString), nil
}

func (a *CSIDriverAssets) SetReplacements(replacements []string) {
	a.replacer = strings.NewReplacer(replacements...)
}

func (a *CSIDriverAssets) getRawAsset(assetName string) ([]byte, error) {
	switch assetName {
	case ControllerDeploymentAssetName:
		return a.ControllerTemplate, nil
	case NodeDaemonSetAssetName:
		return a.NodeTemplate, nil

	default:
		if assetYAML, ok := a.ControllerStaticResources[assetName]; ok {
			return assetYAML, nil
		}
		if assetYAML, ok := a.GuestStorageClassAssets[assetName]; ok {
			return assetYAML, nil
		}
		if assetYAML, ok := a.GuestVolumeSnapshotClasses[assetName]; ok {
			return assetYAML, nil
		}
		if assetYAML, ok := a.GuestStaticResources[assetName]; ok {
			return assetYAML, nil
		}
		return nil, fmt.Errorf("asset %s not found", assetName)
	}
}

func (a *CSIDriverAssets) GetControllerStaticAssetNames() []string {
	var names []string
	for name := range a.ControllerStaticResources {
		names = append(names, name)
	}
	return names
}

func (a *CSIDriverAssets) GetGuestStaticAssetNames() []string {
	var names []string
	for name := range a.GuestStaticResources {
		names = append(names, name)
	}
	return names
}

func (a *CSIDriverAssets) GetStorageClassAssetNames() []string {
	var names []string
	for name := range a.GuestStorageClassAssets {
		names = append(names, name)
	}
	return names
}

func (a *CSIDriverAssets) GetVolumeSnapshotClassAssetNames() []string {
	var names []string
	for name := range a.GuestVolumeSnapshotClasses {
		names = append(names, name)
	}
	return names
}

type AssetsManifest struct {
	ControllerStaticAssetNames []string `yaml:"controllerStaticAssetNames"`
	GuestStaticAssetNames      []string `yaml:"guestStaticAssetNames"`
	StorageClassAssetNames     []string `yaml:"storageClassAssetNames"`
	VolumeSnapshotClassNames   []string `yaml:"volumeSnapshotClassNames"`
}

func (a *CSIDriverAssets) Save(path string) error {
	if path != "" {
		if err := os.MkdirAll(path, 0755); err != nil {
			return err
		}
	}
	m := AssetsManifest{
		ControllerStaticAssetNames: a.GetControllerStaticAssetNames(),
		GuestStaticAssetNames:      a.GetGuestStaticAssetNames(),
		StorageClassAssetNames:     a.GetStorageClassAssetNames(),
		VolumeSnapshotClassNames:   a.GetVolumeSnapshotClassAssetNames(),
	}

	// Write the list in manifest file in stable order
	sort.Strings(m.ControllerStaticAssetNames)
	sort.Strings(m.GuestStaticAssetNames)
	sort.Strings(m.StorageClassAssetNames)
	sort.Strings(m.VolumeSnapshotClassNames)

	mYAML, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshall manifests: %w", err)
	}
	manifestsFile := filepath.Join(path, manifestFileName)
	if err := os.WriteFile(manifestsFile, mYAML, 0644); err != nil {
		return fmt.Errorf("failed to write manifests: %w", err)
	}

	controllerTemplate := MustSanitize(a.ControllerTemplate)
	if err := os.WriteFile(filepath.Join(path, ControllerDeploymentAssetName), controllerTemplate, 0644); err != nil {
		return fmt.Errorf("failed to write controller deployment: %w", err)
	}

	dsTemplate := MustSanitize(a.NodeTemplate)
	if err := os.WriteFile(filepath.Join(path, NodeDaemonSetAssetName), dsTemplate, 0644); err != nil {
		return fmt.Errorf("failed to write node daemonset: %w", err)
	}

	if err := saveAssets(path, a.ControllerStaticResources); err != nil {
		return fmt.Errorf("failed to save controller static resources: %w", err)
	}

	if err := saveAssets(path, a.GuestStaticResources); err != nil {
		return fmt.Errorf("failed to save guest static resources: %w", err)
	}

	if err := saveAssets(path, a.GuestStorageClassAssets); err != nil {
		return fmt.Errorf("failed to save StorageClass assets: %w", err)
	}

	if err := saveAssets(path, a.GuestVolumeSnapshotClasses); err != nil {
		return fmt.Errorf("failed to save VolumeSnapshotClass assets: %w", err)
	}

	return nil
}

func saveAssets(path string, assets map[string][]byte) error {
	for name, assetBytes := range assets {
		path := filepath.Join(path, name)
		data := MustSanitize(assetBytes)
		if err := os.WriteFile(path, data, 0644); err != nil {
			return fmt.Errorf("failed to write asset %s: %w", name, err)
		}
	}
	return nil
}

func NewFromAssets(reader resourceapply.AssetFunc, dir string) (*CSIDriverAssets, error) {
	klog.V(4).Infof("Loading assets from %s", dir)

	manifestsFile := filepath.Join(dir, manifestFileName)
	manifestsBytes, err := reader(manifestsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifests file: %w", err)
	}

	var m AssetsManifest
	if err := yaml.Unmarshal(manifestsBytes, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshall manifests: %w", err)
	}

	assets := &CSIDriverAssets{}

	if assets.ControllerTemplate, err = loadAsset(reader, dir, ControllerDeploymentAssetName); err != nil {
		return nil, fmt.Errorf("failed to read controller deployment: %w", err)
	}

	if assets.NodeTemplate, err = loadAsset(reader, dir, NodeDaemonSetAssetName); err != nil {
		return nil, fmt.Errorf("failed to read node daemonset: %w", err)
	}

	if assets.ControllerStaticResources, err = loadAssetsArray(reader, dir, m.ControllerStaticAssetNames); err != nil {
		return nil, err
	}

	if assets.GuestStaticResources, err = loadAssetsArray(reader, dir, m.GuestStaticAssetNames); err != nil {
		return nil, err
	}

	if assets.GuestStorageClassAssets, err = loadAssetsArray(reader, dir, m.StorageClassAssetNames); err != nil {
		return nil, err
	}

	if assets.GuestVolumeSnapshotClasses, err = loadAssetsArray(reader, dir, m.VolumeSnapshotClassNames); err != nil {
		return nil, err
	}

	return assets, nil
}

func loadAssetsArray(reader resourceapply.AssetFunc, dir string, names []string) (map[string][]byte, error) {
	assets := make(map[string][]byte, len(names))
	for _, name := range names {
		assetBytes, err := loadAsset(reader, dir, name)
		if err != nil {
			return nil, err
		}
		assets[name] = assetBytes
	}
	return assets, nil
}

func loadAsset(reader resourceapply.AssetFunc, dir, assetName string) ([]byte, error) {
	filename := filepath.Join(dir, assetName)
	assetBytes, err := reader(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read asset %s: %w", filename, err)
	}
	klog.V(4).Infof("Loaded asset %s", filename)
	return assetBytes, nil
}
