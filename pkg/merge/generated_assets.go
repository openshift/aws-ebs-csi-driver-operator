package merge

import "fmt"

func (a *CSIDriverAssets) GetAsset(assetName string) ([]byte, error) {
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
