package merge

import "fmt"

func (a *CSIDriverAssets) GetAsset(assetName string) ([]byte, error) {
	switch assetName {
	case ControllerDeploymentAssetName:
		return a.ControllerTemplate, nil

	default:
		if assetYAML, ok := a.ControllerStaticResources[assetName]; ok {
			return assetYAML, nil
		}
		return nil, fmt.Errorf("asset %s not found", assetName)
	}
}

func (a *CSIDriverAssets) GetStaticControllerAssetNames() []string {
	var names []string
	for name := range a.ControllerStaticResources {
		names = append(names, name)
	}
	return names
}
