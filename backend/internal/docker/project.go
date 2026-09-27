package docker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func DetectProjectDeployment(directory string) (ProjectDeploymentSupport, error) {
	dir, err := filepath.Abs(directory)
	if err != nil {
		return ProjectDeploymentSupport{}, fmt.Errorf("%w: invalid project directory", ErrInvalidInput)
	}
	if err := validateValue(dir, "project directory"); err != nil {
		return ProjectDeploymentSupport{}, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ProjectDeploymentSupport{}, fmt.Errorf("%w: project directory not found", ErrNotFound)
		}
		return ProjectDeploymentSupport{}, err
	}
	if !info.IsDir() {
		return ProjectDeploymentSupport{}, fmt.Errorf("%w: project path is not a directory", ErrInvalidInput)
	}

	support := ProjectDeploymentSupport{Modes: []DeploymentMode{}}
	dockerfile := filepath.Join(dir, "Dockerfile")
	if fileInfo, statErr := os.Stat(dockerfile); statErr == nil && fileInfo.Mode().IsRegular() {
		support.Dockerfile = true
		support.Modes = append(support.Modes, DeploymentModeDocker)
	}

	composeFile, composeErr := findComposeFile(dir)
	switch {
	case composeErr == nil:
		support.ComposeFile = filepath.Base(composeFile)
		support.Modes = append(support.Modes, DeploymentModeDockerCompose)
	case errors.Is(composeErr, ErrNotFound):
	default:
		return ProjectDeploymentSupport{}, composeErr
	}
	return support, nil
}
