package installation

import (
	"fmt"

	"github.com/Masterminds/semver"
	installationSDK "github.com/kyma-incubator/hydroform/install/installation"
	"github.com/kyma-project/cli/cmd/kyma/version"
	"github.com/kyma-project/cli/internal/kube"
	pkgErrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
)

// UpgradeKyma triggers the upgrade of a Kyma cluster.
func (i *Installation) UpgradeKyma() (*Result, error) {
	if i.Options.CI || i.Options.NonInteractive {
		i.Factory.NonInteractive = true
	}
	var err error
	if i.k8s, err = kube.NewFromConfigWithTimeout("", i.Options.KubeconfigPath, i.Options.Timeout); err != nil {
		return nil, pkgErrors.Wrap(err, "Could not initialize the Kubernetes client. Make sure your kubeconfig is valid")
	}

	s := i.newStep("Preparing Upgrade")
	// Checking existence of previous installation
	prevInstallationState, kymaVersion, err := i.checkPrevInstallation()
	if err != nil {
		s.Failure()
		return nil, err
	}
	logInfo, err := i.getUpgradeLogInfo(prevInstallationState, kymaVersion)
	if err != nil {
		s.Failure()
		return nil, err
	}

	if prevInstallationState == "Installed" {
		// Checking upgrade compatibility
		if err := i.checkUpgradeCompatability(kymaVersion, version.Version); err != nil {
			s.Failure()
			return nil, err
		}

		// Checking migration guide
		if err := i.promptMigrationGuide(kymaVersion, version.Version); err != nil {
			s.Failure()
			return nil, err
		}

		// Validating configurations
		if err := i.validateConfigurations(); err != nil {
			s.Failure()
			return nil, err
		}

		// Loading upgrade files
		files, err := i.prepareFiles()
		if err != nil {
			s.Failure()
			return nil, err
		}

		// Requesting Kyma Installer to upgrade Kyma
		if err := i.triggerUpgrade(files); err != nil {
			s.Failure()
			return nil, err
		}
		s.Successf("Upgrade is ready")

	} else {
		s.Successf(logInfo)
	}

	if !i.Options.NoWait {
		if prevInstallationState == "Installed" {
			i.newStep("Waiting for upgrade to start")
		} else {
			i.newStep("Re-attaching installation status")
		}
		if err := i.waitForInstaller(); err != nil {
			return nil, err
		}
	}

	result, err := i.buildResult()
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (i *Installation) getUpgradeLogInfo(prevInstallationState string, kymaVersion string) (string, error) {
	var logInfo string
	switch prevInstallationState {
	case installationSDK.NoInstallationState:
		return "", fmt.Errorf("It is not possible to upgrade, since Kyma is not installed on the cluster. Run \"kyma install\" to install Kyma")

	case "InProgress", "Error":
		// when installation is in in "Error" state, it doesn't mean that the installation has failed
		// Installer might sill recover from the error and install Kyma successfully
		logInfo = fmt.Sprintf("Installation in version %s is already in progress", kymaVersion)

	case "":
		return "", fmt.Errorf("Failed to get the installation status")
	}

	return logInfo, nil
}

func (i *Installation) checkUpgradeCompatability(kymaVersion string, cliVersion string) error {
	kymaSemVersion, err := semver.NewVersion(kymaVersion)
	if err != nil {
		return fmt.Errorf("unable to parse kyma version(%s): %v", kymaVersion, err)
	}
	cliSemVersion, err := semver.NewVersion(cliVersion)
	if err != nil {
		return fmt.Errorf("unable to parse cli version(%s): %v", cliVersion, err)
	}

	if kymaSemVersion.GreaterThan(cliSemVersion) {
		return fmt.Errorf("kyma version(%s) is greater than the cli version(%s). Kyma does not support a dedicated downgrade procedure", kymaSemVersion.String(), cliSemVersion.String())
	} else if kymaSemVersion.Equal(cliSemVersion) {
		return fmt.Errorf("kyma version(%s) is already matching the cli version(%s)", kymaSemVersion.String(), cliSemVersion.String())
	} else if kymaSemVersion.Major() != cliSemVersion.Major() {
		return fmt.Errorf("mismatch between kyma version(%s) and cli version(%s) is more than one minor version", kymaSemVersion.String(), cliSemVersion.String())
	} else if kymaSemVersion.Minor() != cliSemVersion.Minor() && kymaSemVersion.Minor()+1 != cliSemVersion.Minor() {
		return fmt.Errorf("mismatch between kyma version(%s) and cli version(%s) is more than one minor version", kymaSemVersion.String(), cliSemVersion.String())
	}

	// set the installation source to be the cli version
	i.Options.Source = cliSemVersion.String()
	i.currentStep.LogInfof("Upgrading Kyma from version %s to version %s", kymaSemVersion.String(), cliSemVersion.String())
	return nil
}

func (i *Installation) promptMigrationGuide(kymaVersion string, cliVersion string) error {
	kymaSemVersion, err := semver.NewVersion(kymaVersion)
	if err != nil {
		return fmt.Errorf("unable to parse kyma version(%s): %v", kymaVersion, err)
	}
	cliSemVersion, err := semver.NewVersion(cliVersion)
	if err != nil {
		return fmt.Errorf("unable to parse cli version(%s): %v", cliVersion, err)
	}

	guideURL := fmt.Sprintf(
		"https://github.com/kyma-project/kyma/blob/release-%v.%v/docs/migration-guides/%v.%v-%v.%v.md",
		cliSemVersion.Major(), cliSemVersion.Minor(),
		kymaSemVersion.Major(), kymaSemVersion.Minor(),
		cliSemVersion.Major(), cliSemVersion.Minor(),
	)
	statusCode, err := doGet(guideURL)
	if err != nil {
		return fmt.Errorf("unable to check migration guide url: %v", err)
	}
	if statusCode == 404 {
		// no migration guide for this release
		i.currentStep.LogInfof("No migration guide available for %s release", cliSemVersion.String())
		return nil
	}
	if statusCode != 200 {
		return fmt.Errorf("unexpected status code %v when checking migration guide url", statusCode)
	}

	promptMsg := fmt.Sprintf("Did you apply the migration guide? %s", guideURL)
	isGuideChecked := i.currentStep.PromptYesNo(promptMsg)
	if !isGuideChecked {
		return fmt.Errorf("migration guide must be applied before Kyma upgrade")
	}
	return nil
}

func (i *Installation) triggerUpgrade(files map[string]*File) error {
	componentList, err := i.loadComponentsConfig()
	if err != nil {
		return fmt.Errorf("Could not load components configuration file. Make sure file is a valid YAML and contains component list: %s", err.Error())
	}

	i.service, err = NewInstallationServiceWithComponents(i.k8s.Config(), i.Options.Timeout, "", componentList)
	if err != nil {
		return fmt.Errorf("Failed to create installation service. Make sure your kubeconfig is valid: %s", err.Error())
	}

	files, err = loadStringContent(files)
	if err != nil {
		return fmt.Errorf("Failed to load installation files: %s", err.Error())
	}

	tillerFileContent := files[tillerFile].StringContent
	installerFileContent := files[installerFile].StringContent
	installerCRFileContent := files[installerCRFile].StringContent
	configuration, err := i.loadConfigurations(files)
	if err != nil {
		return pkgErrors.Wrap(err, "unable to load the configurations")
	}

	err = i.service.TriggerUpgrade(i.k8s.Config(), tillerFileContent, installerFileContent, installerCRFileContent, configuration)
	if err != nil {
		return fmt.Errorf("Failed to start upgrade: %s", err.Error())
	}

	return i.k8s.WaitPodStatusByLabel("kyma-installer", "name", "kyma-installer", corev1.PodRunning)
}
