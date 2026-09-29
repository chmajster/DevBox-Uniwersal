package containerspec

import "fmt"

// RuntimeImageCoordinates are the images used by the managed Dockerfile generator.
func RuntimeImageCoordinates(runtime string) (repository, suffix string, err error) {
	switch NormalizeRuntime(runtime) {
	case "php":
		return "library/php", "-cli-bookworm", nil
	case "node":
		return "library/node", "-bookworm-slim", nil
	case "python":
		return "library/python", "-slim-bookworm", nil
	case "go":
		return "library/golang", "-bookworm", nil
	case "static":
		return "nginxinc/nginx-unprivileged", "-alpine", nil
	default:
		return "", "", fmt.Errorf("unsupported managed runtime")
	}
}
func BaseImage(runtime, version string) (string, error) {
	if err := Validate(runtime, version, nil); err != nil {
		return "", err
	}
	repository, suffix, err := RuntimeImageCoordinates(runtime)
	if err != nil {
		return "", err
	}
	if version == "" {
		version = DefaultVersion(runtime)
	}
	return repository + ":" + version + suffix, nil
}
