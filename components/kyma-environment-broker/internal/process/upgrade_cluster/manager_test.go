package upgrade_cluster

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kyma-project/control-plane/components/kyma-environment-broker/common/orchestration"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/storage"

	"context"
	"sync"

	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/event"
	"github.com/kyma-project/control-plane/components/kyma-environment-broker/internal/process"
	"github.com/pivotal-cf/brokerapi/v8/domain"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	operationIDSuccess = "5b954fa8-fc34-4164-96e9-49e3b6741278"
	operationIDFailed  = "69b8ee2b-5c21-4997-9070-4fd356b24c46"
	operationIDRepeat  = "ca317a1e-ddab-44d2-b2ba-7bbd9df9066f"
	operationIDPanic   = "8ffadf20-5fe6-410b-93ce-9d00088e1e17"
)

func TestManager_Execute(t *testing.T) {
	for name, tc := range map[string]struct {
		operationID            string
		expectedError          bool
		expectedRepeat         time.Duration
		expectedDesc           string
		expectedNumberOfEvents int
	}{
		"operation successful": {
			operationID:            operationIDSuccess,
			expectedError:          false,
			expectedRepeat:         time.Duration(0),
			expectedDesc:           "init one two final",
			expectedNumberOfEvents: 4,
		},
		"operation failed": {
			operationID:            operationIDFailed,
			expectedError:          true,
			expectedNumberOfEvents: 1,
		},
		"operation panicked": {
			operationID:            operationIDPanic,
			expectedError:          true,
			expectedNumberOfEvents: 0,
		},
		"operation repeated": {
			operationID:            operationIDRepeat,
			expectedError:          false,
			expectedRepeat:         time.Duration(10),
			expectedDesc:           "init",
			expectedNumberOfEvents: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			// given
			log := logrus.New()
			memoryStorage := storage.NewMemoryStorage()
			operations := memoryStorage.Operations()
			err := operations.InsertUpgradeClusterOperation(fixOperation(tc.operationID))
			assert.NoError(t, err)

			sInit := testStep{t: t, name: "init", storage: operations}
			s1 := testStep{t: t, name: "one", storage: operations}
			s2 := testStep{t: t, name: "two", storage: operations}
			s3 := testStep{t: t, name: "to be skipped", storage: operations}
			sFinal := testStep{t: t, name: "final", storage: operations}

			eventBroker := event.NewPubSub(logrus.New())
			eventCollector := &collectingEventHandler{}
			eventBroker.Subscribe(process.UpgradeClusterStepProcessed{}, eventCollector.OnEvent)

			manager := NewManager(operations, eventBroker, log)
			manager.InitStep(&sInit)

			manager.AddStep(2, &sFinal, nil)
			manager.AddStep(1, &s1, nil)
			manager.AddStep(1, &s2, func(operation internal.Operation) bool { return true })
			manager.AddStep(1, &s3, func(operation internal.Operation) bool { return false })

			// when
			repeat, err := manager.Execute(tc.operationID)

			// then
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedRepeat, repeat)

				operation, err := operations.GetOperationByID(tc.operationID)
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedDesc, strings.Trim(operation.Description, " "))
			}
			assert.NoError(t, wait.PollImmediate(20*time.Millisecond, 2*time.Second, func() (bool, error) {
				return len(eventCollector.Events) == tc.expectedNumberOfEvents, nil
			}))
		})
	}
}

func fixOperation(ID string) internal.UpgradeClusterOperation {
	return internal.UpgradeClusterOperation{
		Operation: internal.Operation{
			ID:               ID,
			State:            domain.InProgress,
			InstanceID:       "fea2c1a1-139d-43f6-910a-a618828a79d5",
			Description:      "",
			RuntimeOperation: orchestration.RuntimeOperation{},
		},
	}
}

type testStep struct {
	t       *testing.T
	name    string
	storage storage.Operations
}

func (ts *testStep) Name() string {
	return ts.name
}

func (ts *testStep) Run(operation internal.UpgradeClusterOperation, logger logrus.FieldLogger) (internal.UpgradeClusterOperation, time.Duration, error) {
	logger.Infof("inside %s step", ts.name)

	operation.Description = fmt.Sprintf("%s %s", operation.Description, ts.name)
	updated, err := ts.storage.UpdateUpgradeClusterOperation(operation)
	if err != nil {
		ts.t.Error(err)
	}

	switch operation.Operation.ID {
	case operationIDFailed:
		return *updated, 0, fmt.Errorf("operation %s failed", operation.Operation.ID)
	case operationIDRepeat:
		return *updated, time.Duration(10), nil
	case operationIDPanic:
		panic("panic during operation")
	default:
		return *updated, 0, nil
	}
}

type collectingEventHandler struct {
	mu     sync.Mutex
	Events []interface{}
}

func (h *collectingEventHandler) OnEvent(ctx context.Context, ev interface{}) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.Events = append(h.Events, ev)
	return nil
}
