package update

import (
	"fmt"
	"time"

	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/process"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/provisioner"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/storage"
	"github.com/kyma-project/control-plane/components/provisioner/pkg/gqlschema"
	"github.com/sirupsen/logrus"
)

const DryRunPrefix = "dry_run-"
const retryDuration = 10 * time.Second

type UpgradeShootStep struct {
	operationManager    *process.OperationManager
	provisionerClient   provisioner.Client
	runtimeStateStorage storage.RuntimeStates
}

func NewUpgradeShootStep(
	os storage.Operations,
	runtimeStorage storage.RuntimeStates,
	cli provisioner.Client) *UpgradeShootStep {

	return &UpgradeShootStep{
		operationManager:    process.NewOperationManager(os),
		provisionerClient:   cli,
		runtimeStateStorage: runtimeStorage,
	}
}

func (s *UpgradeShootStep) Name() string {
	return "Upgrade_Shoot"
}

func (s *UpgradeShootStep) Run(operation internal.Operation, log logrus.FieldLogger) (internal.Operation, time.Duration, error) {
	if operation.RuntimeID == "" {
		log.Infof("Runtime does not exists, skipping a call to Provisioner")
		return operation, 0, nil
	}
	log = log.WithField("runtimeID", operation.RuntimeID)

	latestRuntimeStateWithOIDC, err := s.runtimeStateStorage.GetLatestWithOIDCConfigByRuntimeID(operation.RuntimeID)
	if err != nil {
		return s.operationManager.RetryOperation(operation, err.Error(), err, 5*time.Second, 1*time.Minute, log)
	}
	operation.LastRuntimeState = latestRuntimeStateWithOIDC

	input, err := s.createUpgradeShootInput(operation)
	if err != nil {
		return s.operationManager.OperationFailed(operation, "invalid operation data - cannot create upgradeShoot input", err, log)
	}

	var provisionerResponse gqlschema.OperationStatus
	if operation.ProvisionerOperationID == "" {
		// trigger upgradeRuntime mutation
		provisionerResponse, err = s.provisionerClient.UpgradeShoot(operation.ProvisioningParameters.ErsContext.GlobalAccountID, operation.RuntimeID, input)
		if err != nil {
			log.Errorf("call to provisioner failed: %s", err)
			return operation, retryDuration, nil
		}

		repeat := time.Duration(0)
		operation, repeat, _ = s.operationManager.UpdateOperation(operation, func(op *internal.Operation) {
			op.ProvisionerOperationID = *provisionerResponse.ID
			op.Description = "update in progress"
		}, log)
		if repeat != 0 {
			log.Errorf("cannot save operation ID from provisioner")
			return operation, retryDuration, nil
		}
	}

	log.Infof("call to provisioner succeeded for update, got operation ID %q", *provisionerResponse.ID)

	rs := internal.NewRuntimeState(*provisionerResponse.RuntimeID, operation.ID, nil, gardenerUpgradeInputToConfigInput(input))
	rs.KymaVersion = operation.RuntimeVersion.Version
	err = s.runtimeStateStorage.Insert(rs)
	if err != nil {
		log.Errorf("cannot insert runtimeState: %s", err)
		return operation, 10 * time.Second, nil
	}
	log.Infof("cluster upgrade process initiated successfully")

	// return repeat mode to start the initialization step which will now check the runtime status
	return operation, 0, nil

}

func (s *UpgradeShootStep) createUpgradeShootInput(operation internal.Operation) (gqlschema.UpgradeShootInput, error) {
	operation.InputCreator.SetProvisioningParameters(operation.ProvisioningParameters)
	if operation.LastRuntimeState.ClusterConfig.OidcConfig != nil {
		operation.InputCreator.SetOIDCLastValues(*operation.LastRuntimeState.ClusterConfig.OidcConfig)
	}
	fullInput, err := operation.InputCreator.CreateUpgradeShootInput()
	if err != nil {
		return fullInput, fmt.Errorf("while building upgradeShootInput for provisioner: %w", err)
	}

	// modify configuration
	result := gqlschema.UpgradeShootInput{
		GardenerConfig: &gqlschema.GardenerUpgradeInput{
			OidcConfig:     fullInput.GardenerConfig.OidcConfig,
			AutoScalerMax:  operation.UpdatingParameters.AutoScalerMax,
			AutoScalerMin:  operation.UpdatingParameters.AutoScalerMin,
			MaxSurge:       operation.UpdatingParameters.MaxSurge,
			MaxUnavailable: operation.UpdatingParameters.MaxUnavailable,
			MachineType:    operation.UpdatingParameters.MachineType,
		},
		Administrators: fullInput.Administrators,
	}
	result.GardenerConfig.ShootNetworkingFilterDisabled = operation.ProvisioningParameters.ErsContext.DisableEnterprisePolicyFilter()

	return result, nil
}

func gardenerUpgradeInputToConfigInput(input gqlschema.UpgradeShootInput) *gqlschema.GardenerConfigInput {
	result := &gqlschema.GardenerConfigInput{
		MachineImage:        input.GardenerConfig.MachineImage,
		MachineImageVersion: input.GardenerConfig.MachineImageVersion,
		DiskType:            input.GardenerConfig.DiskType,
		VolumeSizeGb:        input.GardenerConfig.VolumeSizeGb,
		Purpose:             input.GardenerConfig.Purpose,
		OidcConfig:          input.GardenerConfig.OidcConfig,
	}
	if input.GardenerConfig.KubernetesVersion != nil {
		result.KubernetesVersion = *input.GardenerConfig.KubernetesVersion
	}
	if input.GardenerConfig.MachineType != nil {
		result.MachineType = *input.GardenerConfig.MachineType
	}
	if input.GardenerConfig.AutoScalerMin != nil {
		result.AutoScalerMin = *input.GardenerConfig.AutoScalerMin
	}
	if input.GardenerConfig.AutoScalerMax != nil {
		result.AutoScalerMax = *input.GardenerConfig.AutoScalerMax
	}
	if input.GardenerConfig.MaxSurge != nil {
		result.MaxSurge = *input.GardenerConfig.MaxSurge
	}
	if input.GardenerConfig.MaxUnavailable != nil {
		result.MaxUnavailable = *input.GardenerConfig.MaxUnavailable
	}
	if input.GardenerConfig.ShootNetworkingFilterDisabled != nil {
		result.ShootNetworkingFilterDisabled = input.GardenerConfig.ShootNetworkingFilterDisabled
	}

	return result
}
