package manager

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/uuid"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/common/orchestration"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/notification"
	internalOrchestration "github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/orchestration"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/process"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/storage"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/storage/dbmodel"
	"github.com/pivotal-cf/brokerapi/v8/domain"
	"github.com/sirupsen/logrus"
)

type upgradeKymaFactory struct {
	operationStorage   storage.Operations
	defaultKymaVersion string
}

func NewUpgradeKymaManager(orchestrationStorage storage.Orchestrations, operationStorage storage.Operations, instanceStorage storage.Instances,
	kymaUpgradeExecutor orchestration.OperationExecutor, resolver orchestration.RuntimeResolver, pollingInterval time.Duration,
	log logrus.FieldLogger, cli client.Client, cfg *internalOrchestration.Config, bundleBuilder notification.BundleBuilder, speedFactor int) process.Executor {
	return &orchestrationManager{
		orchestrationStorage: orchestrationStorage,
		operationStorage:     operationStorage,
		instanceStorage:      instanceStorage,
		resolver:             resolver,
		factory: &upgradeKymaFactory{
			operationStorage:   operationStorage,
			defaultKymaVersion: cfg.KymaVersion,
		},
		executor:          kymaUpgradeExecutor,
		pollingInterval:   pollingInterval,
		log:               log,
		k8sClient:         cli,
		configNamespace:   cfg.Namespace,
		configName:        cfg.Name,
		kymaVersion:       cfg.KymaVersion,
		kubernetesVersion: cfg.KubernetesVersion,
		bundleBuilder:     bundleBuilder,
		speedFactor:       speedFactor,
	}
}

func (u *upgradeKymaFactory) NewOperation(o internal.Orchestration, r orchestration.Runtime, i internal.Instance, state domain.LastOperationState) (orchestration.RuntimeOperation, error) {
	id := uuid.New().String()
	details, err := i.GetInstanceDetails()
	if err != nil {
		return orchestration.RuntimeOperation{}, err
	}
	op := internal.UpgradeKymaOperation{
		Operation: internal.Operation{
			ID:                     id,
			Version:                0,
			CreatedAt:              time.Now(),
			UpdatedAt:              time.Now(),
			Type:                   internal.OperationTypeUpgradeKyma,
			InstanceID:             r.InstanceID,
			State:                  state,
			Description:            "Operation created",
			OrchestrationID:        o.OrchestrationID,
			ProvisioningParameters: i.Parameters,
			InstanceDetails:        details,
			RuntimeOperation: orchestration.RuntimeOperation{
				ID:           id,
				Runtime:      r,
				DryRun:       o.Parameters.DryRun,
				Notification: o.Parameters.Notification,
			},
		},
	}
	if o.Parameters.Kyma.Version != "" {
		var majorVer int
		var err error

		majorVer, err = determineMajorVersion(o.Parameters.Kyma.Version, u.defaultKymaVersion)
		if err != nil {
			return orchestration.RuntimeOperation{}, fmt.Errorf("while determining Kyma's major version: %w", err)
		}

		op.RuntimeVersion = *internal.NewRuntimeVersionFromParameters(o.Parameters.Kyma.Version, majorVer)
	}

	err = u.operationStorage.InsertUpgradeKymaOperation(op)
	return op.RuntimeOperation, err
}

func determineMajorVersion(version string, defaultVersion string) (int, error) {
	if isCustomVersion(version) {
		return extractMajorVersionNumberFromVersionString(defaultVersion)
	}
	return extractMajorVersionNumberFromVersionString(version)
}

func isCustomVersion(version string) bool {
	return strings.HasPrefix(version, "PR-") || strings.HasPrefix(version, "main-")
}

func extractMajorVersionNumberFromVersionString(version string) (int, error) {
	splitVer := strings.Split(version, ".")
	majorVerNum, err := strconv.Atoi(splitVer[0])
	if err != nil {
		return 0, fmt.Errorf("cannot convert major version to int")
	}
	return majorVerNum, nil
}

func (u *upgradeKymaFactory) ResumeOperations(orchestrationID string) ([]orchestration.RuntimeOperation, error) {
	ops, _, _, err := u.operationStorage.ListUpgradeKymaOperationsByOrchestrationID(orchestrationID, dbmodel.OperationFilter{States: []string{orchestration.InProgress, orchestration.Retrying, orchestration.Pending}})
	if err != nil {
		return nil, err
	}

	pending := make([]orchestration.RuntimeOperation, 0)
	retrying := make([]orchestration.RuntimeOperation, 0)
	inProgress := make([]orchestration.RuntimeOperation, 0)
	for _, op := range ops {
		if op.State == orchestration.Pending {
			pending = append(pending, op.RuntimeOperation)
		}
		if op.State == orchestration.Retrying {
			runtimeop, err := u.updateRetryingOperation(op)
			if err != nil {
				return nil, err
			}
			retrying = append(retrying, runtimeop)
		}
		if op.State == orchestration.InProgress {
			inProgress = append(inProgress, op.RuntimeOperation)
		}
	}

	return append(inProgress, append(retrying, pending...)...), nil
}

func (u *upgradeKymaFactory) CancelOperation(orchestrationID string, runtimeID string) error {
	ops, _, _, err := u.operationStorage.ListUpgradeKymaOperationsByOrchestrationID(orchestrationID, dbmodel.OperationFilter{States: []string{orchestration.Pending}})
	if err != nil {
		return fmt.Errorf("while listing upgrade kyma operations: %w", err)
	}
	for _, op := range ops {
		if op.InstanceDetails.RuntimeID == runtimeID {
			op.State = orchestration.Canceled
			op.Description = "Operation was canceled"
			_, err := u.operationStorage.UpdateUpgradeKymaOperation(op)
			if err != nil {
				return fmt.Errorf("while updating upgrade kyma operation: %w", err)
			}
		}
	}

	return nil
}

func (u *upgradeKymaFactory) CancelOperations(orchestrationID string) error {
	ops, _, _, err := u.operationStorage.ListUpgradeKymaOperationsByOrchestrationID(orchestrationID, dbmodel.OperationFilter{States: []string{orchestration.Pending}})
	if err != nil {
		return fmt.Errorf("while listing upgrade kyma operations: %w", err)
	}
	for _, op := range ops {
		op.State = orchestration.Canceled
		op.Description = "Operation was canceled"
		_, err := u.operationStorage.UpdateUpgradeKymaOperation(op)
		if err != nil {
			return fmt.Errorf("while updating upgrade kyma operation: %w", err)
		}
	}

	return nil
}

// get current retrying operations
func (u *upgradeKymaFactory) RetryOperations(retryOps []string) ([]orchestration.RuntimeOperation, error) {
	result := []orchestration.RuntimeOperation{}

	for _, opId := range retryOps {
		runtimeop, err := u.operationStorage.GetUpgradeKymaOperationByID(opId)
		if err != nil {
			fmt.Errorf("while geting (retrying) upgrade kyma operation %s in storage: %w", opId, err)
		}

		result = append(result, runtimeop.RuntimeOperation)
	}

	return result, nil
}

// update storage in corresponding upgrade factory to avoid too many storage read and write
func (u *upgradeKymaFactory) updateRetryingOperation(op internal.UpgradeKymaOperation) (orchestration.RuntimeOperation, error) {
	op.UpdatedAt = time.Now()
	op.State = orchestration.Pending
	op.Description = "Operation retry triggered"
	op.ProvisionerOperationID = ""

	opUpdated, err := u.operationStorage.UpdateUpgradeKymaOperation(op)
	if err != nil {
		return orchestration.RuntimeOperation{}, fmt.Errorf("while updating (retrying) upgrade kyma operation %s in storage: %w", op.Operation.ID, err)
	}

	return opUpdated.RuntimeOperation, nil
}

func (u *upgradeKymaFactory) QueryOperation(orchestrationID string, r orchestration.Runtime) (bool, orchestration.RuntimeOperation, error) {
	ops, _, _, err := u.operationStorage.ListUpgradeKymaOperationsByOrchestrationID(orchestrationID, dbmodel.OperationFilter{States: []string{orchestration.Pending}})
	if err != nil {
		return false, orchestration.RuntimeOperation{}, fmt.Errorf("while listing upgrade kyma operations: %w", err)
	}
	for _, op := range ops {
		if op.InstanceDetails.RuntimeID == r.RuntimeID {
			return true, op.RuntimeOperation, nil
		}
	}

	return false, orchestration.RuntimeOperation{}, nil
}

func (u *upgradeKymaFactory) QueryOperations(orchestrationID string) ([]orchestration.RuntimeOperation, error) {
	ops, _, _, err := u.operationStorage.ListUpgradeKymaOperationsByOrchestrationID(orchestrationID, dbmodel.OperationFilter{States: []string{orchestration.Pending}})
	if err != nil {
		return []orchestration.RuntimeOperation{}, fmt.Errorf("while listing kyma cluster operations: %w", err)
	}
	result := []orchestration.RuntimeOperation{}
	for _, op := range ops {
		result = append(result, op.RuntimeOperation)
	}

	return result, nil
}

func (u *upgradeKymaFactory) NotifyOperation(orchestrationID string, runtimeID string, oState string, notifyState orchestration.NotificationStateType) error {
	ops, _, _, err := u.operationStorage.ListUpgradeKymaOperationsByOrchestrationID(orchestrationID, dbmodel.OperationFilter{States: []string{oState}})
	if err != nil {
		return fmt.Errorf("while listing upgrade kyma operations: %w", err)
	}
	for _, op := range ops {
		if op.InstanceDetails.RuntimeID == runtimeID {
			op.RuntimeOperation.NotificationState = notifyState
			_, err := u.operationStorage.UpdateUpgradeKymaOperation(op)
			if err != nil {
				return fmt.Errorf("while updating pending upgrade kyma operation %s in storage: %w", op.Operation.ID, err)
			}
		}
	}
	return nil
}
