package merge

import (
	"bytes"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/openshift/aws-ebs-csi-driver-operator/assets"
	"sigs.k8s.io/kustomize/kyaml/yaml"
	"sigs.k8s.io/kustomize/kyaml/yaml/merge2"
	sigyaml "sigs.k8s.io/yaml"
)

func applyAssetPatch(sourceYAML []byte, assetName string, replacements []string) ([]byte, error) {
	if strings.HasSuffix(assetName, ".patch") {
		return applyJSONPatch(sourceYAML, assetName, replacements)
	}
	return applyStrategicMergePatch(sourceYAML, assetName, replacements)
}

func applyStrategicMergePatch(sourceYAML []byte, assetName string, replacements []string) ([]byte, error) {
	patchYAML := mustReadAsset(assetName, replacements)
	opts := yaml.MergeOptions{ListIncreaseDirection: yaml.MergeOptionsListAppend}
	ret, err := merge2.MergeStrings(string(patchYAML), string(sourceYAML), false, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply aseet %s: %v", assetName, err)
	}
	return []byte(ret), nil
}

func addSidecar(sourceYAML []byte, assetName string, replacements []string, extraArguments []string, flavour ClusterFlavour, assetPatches AssetPatches) ([]byte, error) {
	sidecarYAML := mustReadAsset(assetName, replacements)

	sidecarYAML, err := addArguments(sidecarYAML, extraArguments)
	if err != nil {
		return nil, err
	}

	// Apply all assetPatches
	for _, patch := range assetPatches {
		if !patch.ClusterFlavours.Has(flavour) {
			continue
		}
		sidecarYAML, err = applyAssetPatch(sidecarYAML, patch.PatchAssetName, replacements)
		if err != nil {
			return nil, err
		}
	}

	opts := yaml.MergeOptions{ListIncreaseDirection: yaml.MergeOptionsListAppend}
	ret, err := merge2.MergeStrings(string(sidecarYAML), string(sourceYAML), false, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply asset %s: %v", assetName, err)
	}

	return []byte(ret), nil
}

func addArguments(sidecarYAML []byte, extraArguments []string) ([]byte, error) {
	if len(extraArguments) == 0 {
		return sidecarYAML, nil
	}

	sidecarJSON, err := sigyaml.YAMLToJSON(sidecarYAML)
	if err != nil {
		return nil, err
	}

	// JSON patch does not allow adding multiple elements to a list at once.
	// So we need to apply a patch for each extra argument.
	finalPatchYAML := bytes.NewBuffer(nil)
	for _, arg := range extraArguments {
		singleArgYAMLPatch := mustReadAsset("patches/add-arg.yaml.patch", []string{"${EXTRA_ARGUMENTS}", arg})
		finalPatchYAML.Write(singleArgYAMLPatch)
	}

	finalPatchJSON, err := sigyaml.YAMLToJSON(finalPatchYAML.Bytes())
	if err != nil {
		return nil, err
	}
	argsPatch, err := jsonpatch.DecodePatch(finalPatchJSON)
	if err != nil {
		return nil, err
	}
	sidecarJSON, err = argsPatch.Apply(sidecarJSON)
	if err != nil {
		return nil, err
	}
	sidecarYAML, err = sigyaml.JSONToYAML(sidecarJSON)
	if err != nil {
		return nil, err
	}
	return sidecarYAML, nil
}

func applyJSONPatch(sourceYAML []byte, assetName string, replacements []string) ([]byte, error) {
	patchYAML := mustReadAsset(assetName, replacements)
	patchJSON, err := sigyaml.YAMLToJSON(patchYAML)
	if err != nil {
		return nil, err
	}
	sourceJSON, err := sigyaml.YAMLToJSON(sourceYAML)
	if err != nil {
		return nil, err
	}
	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return nil, err
	}
	sourceJSON, err = patch.Apply(sourceJSON)
	if err != nil {
		return nil, err
	}
	ret, err := sigyaml.JSONToYAML(sourceJSON)
	if err != nil {
		return nil, err
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

func replaceBytes(src []byte, replacements []string) []byte {
	for i := 0; i < len(replacements); i += 2 {
		src = bytes.ReplaceAll(src, []byte(replacements[i]), []byte(replacements[i+1]))
	}
	return src
}
