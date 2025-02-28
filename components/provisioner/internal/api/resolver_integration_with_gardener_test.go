package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kyma-project/control-plane/components/provisioner/internal/api/fake/seeds"
	"github.com/kyma-project/control-plane/components/provisioner/internal/api/fake/shoots"
	"github.com/kyma-project/control-plane/components/provisioner/internal/uuid"

	provisioning2 "github.com/kyma-project/control-plane/components/provisioner/internal/operations/stages/provisioning"

	"github.com/kyma-project/control-plane/components/provisioner/internal/api"

	"github.com/kyma-project/control-plane/components/provisioner/internal/util/k8s/mocks"

	"github.com/kyma-project/control-plane/components/provisioner/internal/operations/queue"

	"github.com/kyma-project/control-plane/components/provisioner/internal/model"

	"github.com/kyma-project/control-plane/components/provisioner/internal/api/middlewares"

	gardener_types "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardener_apis "github.com/gardener/gardener/pkg/client/core/clientset/versioned/typed/core/v1beta1"

	"github.com/kyma-incubator/compass/components/director/pkg/graphql"
	directormock "github.com/kyma-project/control-plane/components/provisioner/internal/director/mocks"
	"github.com/kyma-project/control-plane/components/provisioner/internal/gardener"
	"github.com/kyma-project/control-plane/components/provisioner/internal/persistence/database"
	"github.com/kyma-project/control-plane/components/provisioner/internal/persistence/testutils"
	"github.com/kyma-project/control-plane/components/provisioner/internal/provisioning"
	"github.com/kyma-project/control-plane/components/provisioner/internal/provisioning/persistence/dbsession"
	runtimeConfig "github.com/kyma-project/control-plane/components/provisioner/internal/runtime"
	"github.com/kyma-project/control-plane/components/provisioner/internal/util"
	"github.com/kyma-project/control-plane/components/provisioner/pkg/gqlschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var testEnv *envtest.Environment
var cfg *rest.Config
var mgr ctrl.Manager

const (
	namespace  = "default"
	syncPeriod = 3 * time.Second
	waitPeriod = 5 * time.Second

	kymaVersion                   = "1.8"
	kymaSystemNamespace           = "kyma-system"
	compassSystemNamespace        = "kyma-system"
	clusterEssentialsComponent    = "cluster-essentials"
	rafterComponent               = "rafter"
	coreComponent                 = "core"
	applicationConnectorComponent = "application-connector"
	runtimeAgentComponent         = "compass-runtime-agent"

	tenant               = "tenant"
	rafterSourceURL      = "github.com/kyma-project/kyma.git//resources/rafter"
	auditLogPolicyCMName = "auditLogPolicyConfigMap"
	subAccountId         = "sub-account"
	gardenerGenSeed      = "az-us2"

	defaultEnableKubernetesVersionAutoUpdate   = false
	defaultEnableMachineImageVersionAutoUpdate = false

	mockedKubeconfig = `apiVersion: v1
clusters:
- cluster:
    server: https://192.168.64.4:8443
  name: minikube
contexts:
- context:
    cluster: minikube
    user: minikube
  name: minikube
current-context: minikube
kind: Config
preferences: {}
users:
- name: minikube
  user:
    client-certificate-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURBRENDQWVpZ0F3SUJBZ0lCQWpBTkJna3Foa2lHOXcwQkFRc0ZBREFWTVJNd0VRWURWUVFERXdwdGFXNXAKYTNWaVpVTkJNQjRYRFRFNU1URXhOekE0TXpBek1sb1hEVEl3TVRFeE56QTRNekF6TWxvd01URVhNQlVHQTFVRQpDaE1PYzNsemRHVnRPbTFoYzNSbGNuTXhGakFVQmdOVkJBTVREVzFwYm1scmRXSmxMWFZ6WlhJd2dnRWlNQTBHCkNTcUdTSWIzRFFFQkFRVUFBNElCRHdBd2dnRUtBb0lCQVFDNmY2SjZneElvL2cyMHArNWhybklUaUd5SDh0VW0KWGl1OElaK09UKyt0amd1OXRneXFnbnNsL0dDT1Q3TFo4ejdOVCttTEdKL2RLRFdBV3dvbE5WTDhxMzJIQlpyNwpDaU5hK3BBcWtYR0MzNlQ2NEQyRjl4TEtpVVpuQUVNaFhWOW1oeWVCempscTh1NnBjT1NrY3lJWHRtdU9UQUVXCmErWlp5UlhOY3BoYjJ0NXFUcWZoSDhDNUVDNUIrSm4rS0tXQ2Y1Nm5KZGJQaWduRXh4SFlaMm9TUEc1aXpkbkcKZDRad2d0dTA3NGttaFNtNXQzbjgyNmovK29tL25VeWdBQ24yNmR1K21aZzRPcWdjbUMrdnBYdUEyRm52bk5LLwo5NWErNEI3cGtNTER1bHlmUTMxcjlFcStwdHBkNUR1WWpldVpjS1Bxd3ZVcFUzWVFTRUxVUzBrUkFnTUJBQUdqClB6QTlNQTRHQTFVZER3RUIvd1FFQXdJRm9EQWRCZ05WSFNVRUZqQVVCZ2dyQmdFRkJRY0RBUVlJS3dZQkJRVUgKQXdJd0RBWURWUjBUQVFIL0JBSXdBREFOQmdrcWhraUc5dzBCQVFzRkFBT0NBUUVBQ3JnbExWemhmemZ2aFNvUgowdWNpNndBZDF6LzA3bW52MDRUNmQyTkpjRG80Uzgwa0o4VUJtRzdmZE5qMlJEaWRFbHRKRU1kdDZGa1E1TklOCk84L1hJdENiU0ZWYzRWQ1NNSUdPcnNFOXJDajVwb24vN3JxV3dCbllqYStlbUVYOVpJelEvekJGU3JhcWhud3AKTkc1SmN6bUg5ODRWQUhGZEMvZWU0Z2szTnVoV25rMTZZLzNDTTFsRkxlVC9Cbmk2K1M1UFZoQ0x3VEdmdEpTZgorMERzbzVXVnFud2NPd3A3THl2K3h0VGtnVmdSRU5RdTByU2lWL1F2UkNPMy9DWXdwRTVIRFpjalM5N0I4MW0yCmVScVBENnVoRjVsV3h4NXAyeEd1V2JRSkY0WnJzaktLTW1CMnJrUnR5UDVYV2xWZU1mR1VjbFdjc1gxOW91clMKaWpKSTFnPT0KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
    client-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFcEFJQkFBS0NBUUVBdW4raWVvTVNLUDROdEtmdVlhNXlFNGhzaC9MVkpsNHJ2Q0dmamsvdnJZNEx2YllNCnFvSjdKZnhnamsreTJmTSt6VS9waXhpZjNTZzFnRnNLSlRWUy9LdDlod1dhK3dvald2cVFLcEZ4Z3Qrayt1QTkKaGZjU3lvbEdad0JESVYxZlpvY25nYzQ1YXZMdXFYRGtwSE1pRjdacmprd0JGbXZtV2NrVnpYS1lXOXJlYWs2bgo0Ui9BdVJBdVFmaVovaWlsZ24rZXB5WFd6NG9KeE1jUjJHZHFFanh1WXMzWnhuZUdjSUxidE8rSkpvVXB1YmQ1Ci9OdW8vL3FKdjUxTW9BQXA5dW5idnBtWU9EcW9ISmd2cjZWN2dOaFo3NXpTdi9lV3Z1QWU2WkRDdzdwY24wTjkKYS9SS3ZxYmFYZVE3bUkzcm1YQ2o2c0wxS1ZOMkVFaEMxRXRKRVFJREFRQUJBb0lCQVFDTEVFa3pXVERkYURNSQpGb0JtVGhHNkJ1d0dvMGZWQ0R0TVdUWUVoQTZRTjI4QjB4RzJ3dnpZNGt1TlVsaG10RDZNRVo1dm5iajJ5OWk1CkVTbUxmU3VZUkxlaFNzaTVrR0cwb1VtR3RGVVQ1WGU3cWlHMkZ2bm9GRnh1eVg5RkRiN3BVTFpnMEVsNE9oVkUKTzI0Q1FlZVdEdXc4ZXVnRXRBaGJ3dG1ERElRWFdPSjcxUEcwTnZKRHIwWGpkcW1aeExwQnEzcTJkZTU2YmNjawpPYzV6dmtJNldrb0o1TXN0WkZpU3pVRDYzN3lIbjh2NGd3cXh0bHFoNWhGLzEwV296VmZqVGdWSG0rc01ZaU9SCmNIZ0dMNUVSbDZtVlBsTTQzNUltYnFnU1R2NFFVVGpzQjRvbVBsTlV5Yksvb3pPSWx3RjNPTkJjVVV6eDQ1cGwKSHVJQlQwZ1JBb0dCQU9SR2lYaVBQejdsay9Bc29tNHkxdzFRK2hWb3Yvd3ovWFZaOVVkdmR6eVJ1d3gwZkQ0QgpZVzlacU1hK0JodnB4TXpsbWxYRHJBMklYTjU3UEM3ZUo3enhHMEVpZFJwN3NjN2VmQUN0eDN4N0d0V2pRWGF2ClJ4R2xDeUZxVG9LY3NEUjBhQ0M0Um15VmhZRTdEY0huLy9oNnNzKys3U2tvRVMzNjhpS1RiYzZQQW9HQkFORW0KTHRtUmZieHIrOE5HczhvdnN2Z3hxTUlxclNnb2NmcjZoUlZnYlU2Z3NFd2pMQUs2ZHdQV0xWQmVuSWJ6bzhodApocmJHU1piRnF0bzhwS1Q1d2NxZlpKSlREQnQxYmhjUGNjWlRmSnFmc0VISXc0QW5JMVdRMlVzdzVPcnZQZWhsCmh0ek95cXdBSGZvWjBUTDlseTRJUHRqbXArdk1DQ2NPTHkwanF6NWZBb0dCQUlNNGpRT3hqSkN5VmdWRkV5WTMKc1dsbE9DMGdadVFxV3JPZnY2Q04wY1FPbmJCK01ZRlBOOXhUZFBLeC96OENkVyszT0syK2FtUHBGRUdNSTc5cApVdnlJdUxzTGZMZDVqVysyY3gvTXhaU29DM2Z0ZmM4azJMeXEzQ2djUFA5VjVQQnlUZjBwRU1xUWRRc2hrRG44CkRDZWhHTExWTk8xb3E5OTdscjhMY3A2L0FvR0FYNE5KZC9CNmRGYjRCYWkvS0lGNkFPQmt5aTlGSG9iQjdyVUQKbTh5S2ZwTGhrQk9yNEo4WkJQYUZnU09ENWhsVDNZOHZLejhJa2tNNUVDc0xvWSt4a1lBVEpNT3FUc3ZrOThFRQoyMlo3Qy80TE55K2hJR0EvUWE5Qm5KWDZwTk9XK1ErTWRFQTN6QzdOZ2M3U2U2L1ZuNThDWEhtUmpCeUVTSm13CnI3T1BXNDhDZ1lBVUVoYzV2VnlERXJxVDBjN3lIaXBQbU1wMmljS1hscXNhdC94YWtobENqUjZPZ2I5aGQvNHIKZm1wUHJmd3hjRmJrV2tDRUhJN01EdDJrZXNEZUhRWkFxN2xEdjVFT2k4ZG1uM0ZPNEJWczhCOWYzdm52MytmZwpyV2E3ZGtyWnFudU12cHhpSWlqOWZEak9XbzdxK3hTSFcxWWdSNGV2Q1p2NGxJU0FZRlViemc9PQotLS0tLUVORCBSU0EgUFJJVkFURSBLRVktLS0tLQo=
`
)

func TestProvisioning_ProvisionRuntimeWithDatabase(t *testing.T) {
	//given
	ctx := context.WithValue(context.Background(), middlewares.Tenant, tenant)
	ctx = context.WithValue(ctx, middlewares.SubAccountID, subAccountId)

	cleanupNetwork, err := testutils.EnsureTestNetworkForDB(t, ctx)
	require.NoError(t, err)
	defer cleanupNetwork()

	containerCleanupFunc, connString, err := testutils.InitTestDBContainer(t, ctx, "postgres_database_2")
	require.NoError(t, err)
	defer containerCleanupFunc()

	connection, err := database.InitializeDatabaseConnection(connString, 5)
	require.NoError(t, err)
	require.NotNil(t, connection)
	defer testutils.CloseDatabase(t, connection)

	err = database.SetupSchema(connection, testutils.SchemaFilePath)
	require.NoError(t, err)

	directorServiceMock := &directormock.DirectorClient{}

	mockK8sClientProvider := &mocks.K8sClientProvider{}
	fakeK8sClient := fake.NewSimpleClientset()
	mockK8sClientProvider.On("CreateK8SClient", mockedKubeconfig).Return(fakeK8sClient, nil)

	runtimeConfigurator := runtimeConfig.NewRuntimeConfigurator(mockK8sClientProvider, directorServiceMock)

	auditLogsConfigPath := filepath.Join("testdata", "config.json")
	maintenanceWindowConfigPath := filepath.Join("testdata", "maintwindow.json")

	shootInterface := shoots.NewFakeShootsInterface(t, cfg)
	seedInterface := seeds.NewFakeSeedsInterface(t, cfg)
	secretsInterface := setupSecretsClient(t, cfg)
	secretKey := "qbl92bqtl6zshtjb4bvbwwc2qk7vtw2d"
	dbsFactory, _ := dbsession.NewFactory(connection, secretKey)

	queueCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	provisioningQueue := queue.CreateProvisioningQueue(
		testProvisioningTimeouts(),
		dbsFactory,
		directorServiceMock,
		shootInterface,
		secretsInterface,
		testOperatorRoleBinding(),
		mockK8sClientProvider,
		runtimeConfigurator)
	provisioningQueue.Run(queueCtx.Done())

	deprovisioningQueue := queue.CreateDeprovisioningQueue(testDeprovisioningTimeouts(), dbsFactory, directorServiceMock, shootInterface)
	deprovisioningQueue.Run(queueCtx.Done())

	shootUpgradeQueue := queue.CreateShootUpgradeQueue(testProvisioningTimeouts(), dbsFactory, directorServiceMock, shootInterface, testOperatorRoleBinding(), mockK8sClientProvider, secretsInterface)
	shootUpgradeQueue.Run(queueCtx.Done())

	controler, err := gardener.NewShootController(mgr, dbsFactory, auditLogsConfigPath)
	require.NoError(t, err)

	go func() {
		err := controler.StartShootController()
		require.NoError(t, err)
	}()

	clusterConfigurations := newTestProvisioningConfigs()

	for _, config := range clusterConfigurations {
		t.Run(config.description, func(t *testing.T) {
			if config.seed != nil {
				_, err := seedInterface.Create(context.Background(), config.seed, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			clusterConfig := config.provisioningInput.config
			runtimeInput := config.provisioningInput.runtimeInput

			_ = fakeK8sClient.CoreV1().Secrets(compassSystemNamespace).Delete(context.Background(), runtimeConfig.AgentConfigurationSecretName, metav1.DeleteOptions{})
			_ = fakeK8sClient.CoreV1().ConfigMaps(compassSystemNamespace).Delete(context.Background(), runtimeConfig.AgentConfigurationSecretName, metav1.DeleteOptions{})

			directorServiceMock.Calls = nil
			directorServiceMock.ExpectedCalls = nil

			directorServiceMock.On("CreateRuntime", mock.Anything, mock.Anything).Return(config.runtimeID, nil)
			directorServiceMock.On("RuntimeExists", mock.Anything, mock.Anything).Return(true, nil)
			directorServiceMock.On("DeleteRuntime", mock.Anything, mock.Anything).Return(nil)
			directorServiceMock.On("GetConnectionToken", mock.Anything, mock.Anything).Return(graphql.OneTimeTokenForRuntimeExt{}, nil)

			directorServiceMock.On("GetRuntime", mock.Anything, mock.Anything).Return(graphql.RuntimeExt{
				Runtime: graphql.Runtime{
					ID:          config.runtimeID,
					Name:        runtimeInput.Name,
					Description: runtimeInput.Description,
				},
			}, nil)

			directorServiceMock.On("UpdateRuntime", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			directorServiceMock.On("SetRuntimeStatusCondition", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			uuidGenerator := uuid.NewUUIDGenerator()
			provisioner := gardener.NewProvisioner(namespace, shootInterface, dbsFactory, auditLogPolicyCMName, maintenanceWindowConfigPath)

			inputConverter := provisioning.NewInputConverter(uuidGenerator, "Project", defaultEnableKubernetesVersionAutoUpdate, defaultEnableMachineImageVersionAutoUpdate)
			graphQLConverter := provisioning.NewGraphQLConverter()

			provisioningService := provisioning.NewProvisioningService(inputConverter, graphQLConverter, directorServiceMock, dbsFactory, provisioner, uuidGenerator, gardener.NewShootProvider(shootInterface), provisioningQueue, deprovisioningQueue, shootUpgradeQueue)

			validator := api.NewValidator()

			tenantUpdater := api.NewTenantUpdater(dbsFactory.NewReadWriteSession())

			resolver := api.NewResolver(provisioningService, validator, tenantUpdater)

			fullConfig := gqlschema.ProvisionRuntimeInput{RuntimeInput: &runtimeInput, ClusterConfig: &clusterConfig}

			testProvisionRuntime(t, ctx, resolver, fullConfig, config.runtimeID, shootInterface, secretsInterface, config.auditLogConfig)

			testUpgradeGardenerShoot(t, ctx, resolver, dbsFactory, config.runtimeID, config.upgradeShootInput, shootInterface, inputConverter)

			testDeprovisionRuntime(t, ctx, resolver, dbsFactory, config.runtimeID, shootInterface)
		})
	}
}

func testProvisionRuntime(t *testing.T, ctx context.Context, resolver *api.Resolver, fullConfig gqlschema.ProvisionRuntimeInput, runtimeID string, shootInterface gardener_apis.ShootInterface, secretsInterface v1core.SecretInterface, auditLogConfig *gardener.AuditLogConfig) {

	// when Provisioning Runtime
	provisionRuntime, err := resolver.ProvisionRuntime(ctx, fullConfig)

	// then
	require.NoError(t, err)
	require.NotEmpty(t, provisionRuntime)

	// wait for queue to process operation
	time.Sleep(2 * syncPeriod)

	list, err := shootInterface.List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)

	shoot := &list.Items[0]

	simulateSuccessfulClusterProvisioning(t, shootInterface, secretsInterface, shoot.Name)

	// wait for Shoot to update
	time.Sleep(2 * waitPeriod)

	shoot, err = shootInterface.Get(context.Background(), shoot.Name, metav1.GetOptions{})
	require.NoError(t, err)

	// then
	assert.Equal(t, runtimeID, shoot.Annotations["kcp.provisioner.kyma-project.io/runtime-id"])
	assert.Equal(t, runtimeID, shoot.Annotations["compass.provisioner.kyma-project.io/runtime-id"])
	assert.Equal(t, *provisionRuntime.ID, shoot.Annotations["kcp.provisioner.kyma-project.io/operation-id"])
	assert.Equal(t, *provisionRuntime.ID, shoot.Annotations["compass.provisioner.kyma-project.io/operation-id"])

	ext := gardener.FindExtension(shoot)
	sec := gardener.FindSecret(shoot)

	if auditLogConfig != nil {
		require.NotNil(t, ext)
		extCfg := gardener.AuditlogExtensionConfig{}
		err = json.Unmarshal(ext.ProviderConfig.Raw, &extCfg)
		require.NoError(t, err)
		assert.Equal(t, "auditlog-credentials", extCfg.SecretReferenceName)
		assert.Equal(t, auditLogConfig.TenantID, extCfg.TenantID)
		assert.Equal(t, auditLogConfig.ServiceURL, extCfg.ServiceURL)

		require.NotNil(t, sec)
		assert.Equal(t, auditLogConfig.SecretName, sec.ResourceRef.Name)
		assert.Equal(t, "Secret", sec.ResourceRef.Kind)
	} else {
		assert.Nil(t, ext)
	}

	assert.Equal(t, subAccountId, shoot.Labels[model.SubAccountLabel])

	// when checking Runtime Status
	runtimeStatusProvisioned, err := resolver.RuntimeStatus(ctx, *provisionRuntime.RuntimeID)

	// then
	require.NoError(t, err)
	require.NotNil(t, runtimeStatusProvisioned)
	assert.Equal(t, fixOperationStatusProvisioned(provisionRuntime.RuntimeID, provisionRuntime.ID), runtimeStatusProvisioned.LastOperationStatus)

	var expectedSeed = gardenerGenSeed
	if fullConfig.ClusterConfig.GardenerConfig.Seed != nil {
		expectedSeed = *fullConfig.ClusterConfig.GardenerConfig.Seed
	}

	assert.Equal(t, expectedSeed, *runtimeStatusProvisioned.RuntimeConfiguration.ClusterConfig.Seed)
}

func testUpgradeGardenerShoot(t *testing.T, ctx context.Context, resolver *api.Resolver, dbsFactory dbsession.Factory, runtimeID string, upgradeShootInput gqlschema.UpgradeShootInput, shootInterface gardener_apis.ShootInterface, inputConverter provisioning.InputConverter) {

	list, err := shootInterface.List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	shoot := &list.Items[0]

	readSession := dbsFactory.NewReadSession()
	// when Upgrade Shoot
	runtimeBeforeUpgrade, err := readSession.GetCluster(runtimeID)
	require.NoError(t, err)

	upgradeShootOp, err := resolver.UpgradeShoot(ctx, runtimeID, upgradeShootInput)
	require.NoError(t, err)

	// for wait for shoot new version step
	simulateShootUpgrade(t, shootInterface, shoot.Name)

	// then
	require.NoError(t, err)
	assert.NotEmpty(t, upgradeShootOp.ID)
	assert.Equal(t, gqlschema.OperationTypeUpgradeShoot, upgradeShootOp.Operation)
	assert.Equal(t, gqlschema.OperationStateInProgress, upgradeShootOp.State)
	require.NotNil(t, upgradeShootOp.RuntimeID)
	assert.Equal(t, runtimeID, *upgradeShootOp.RuntimeID)

	// wait for queue to process operation
	time.Sleep(3 * waitPeriod)

	// assert db content
	runtimeAfterUpgrade, err := readSession.GetCluster(runtimeID)
	require.NoError(t, err)
	shootAfterUpgrade := runtimeAfterUpgrade.ClusterConfig

	expectedShootConfig, err := inputConverter.UpgradeShootInputToGardenerConfig(*upgradeShootInput.GardenerConfig, runtimeBeforeUpgrade.ClusterConfig)
	require.NoError(t, err)
	assert.Equal(t, expectedShootConfig, shootAfterUpgrade)

	operation, err := readSession.GetOperation(*upgradeShootOp.ID)
	require.NoError(t, err)
	assert.Equal(t, strings.ToUpper(gqlschema.OperationStateSucceeded.String()), string(operation.State))
}

func testDeprovisionRuntime(t *testing.T, ctx context.Context, resolver *api.Resolver, dbsFactory dbsession.Factory, runtimeID string, shootInterface gardener_apis.ShootInterface) {

	list, err := shootInterface.List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	shoot := &list.Items[0]

	readSession := dbsFactory.NewReadSession()
	runtimeFromDB, err := readSession.GetCluster(runtimeID)
	require.NoError(t, err)

	// when
	deprovisionRuntimeID, err := resolver.DeprovisionRuntime(ctx, runtimeID)
	require.NoError(t, err)
	require.NotEmpty(t, deprovisionRuntimeID)

	// when
	// wait for Shoot to update
	time.Sleep(2 * waitPeriod)
	shoot, err = shootInterface.Get(context.Background(), shoot.Name, metav1.GetOptions{})

	// then
	require.NoError(t, err)
	assert.Equal(t, runtimeID, shoot.Annotations["kcp.provisioner.kyma-project.io/runtime-id"])
	assert.Equal(t, runtimeID, shoot.Annotations["compass.provisioner.kyma-project.io/runtime-id"])

	//when Deprovisioning
	shoot = removeFinalizers(t, shootInterface, shoot)
	time.Sleep(4 * waitPeriod)
	emptyShoot, err := shootInterface.Get(context.Background(), shoot.Name, metav1.GetOptions{})

	// then
	require.Error(t, err, "Shoot %s should not be there!", shoot.Name)
	require.Empty(t, emptyShoot)

	// assert database content
	runtimeFromDB, err = readSession.GetCluster(runtimeID)
	require.NoError(t, err)
	assert.Equal(t, tenant, runtimeFromDB.Tenant)
	assert.Equal(t, subAccountId, util.UnwrapStr(runtimeFromDB.SubAccountId))
	assert.Equal(t, true, runtimeFromDB.Deleted)

	operation, err := readSession.GetOperation(deprovisionRuntimeID)
	require.NoError(t, err)
	assert.Equal(t, strings.ToUpper(gqlschema.OperationStateSucceeded.String()), string(operation.State))
}

func fixOperationStatusProvisioned(runtimeId, operationId *string) *gqlschema.OperationStatus {
	return &gqlschema.OperationStatus{
		ID:        operationId,
		Operation: gqlschema.OperationTypeProvision,
		State:     gqlschema.OperationStateSucceeded,
		RuntimeID: runtimeId,
		Message:   util.StringPtr("Operation succeeded"),
		LastError: &gqlschema.LastError{},
	}
}

func testProvisioningTimeouts() queue.ProvisioningTimeouts {
	return queue.ProvisioningTimeouts{
		ClusterCreation:        5 * time.Minute,
		ClusterDomains:         5 * time.Minute,
		BindingsCreation:       5 * time.Minute,
		InstallationTriggering: 5 * time.Minute,
		Installation:           5 * time.Minute,
		Upgrade:                5 * time.Minute,
		UpgradeTriggering:      5 * time.Minute,
		ShootUpgrade:           5 * time.Minute,
		ShootRefresh:           5 * time.Minute,
		AgentConfiguration:     5 * time.Minute,
		AgentConnection:        5 * time.Minute,
	}
}

func testDeprovisioningTimeouts() queue.DeprovisioningTimeouts {
	return queue.DeprovisioningTimeouts{
		ClusterDeletion:           5 * time.Minute,
		WaitingForClusterDeletion: 5 * time.Minute,
	}
}

func testOperatorRoleBinding() provisioning2.OperatorRoleBinding {
	return provisioning2.OperatorRoleBinding{
		L2SubjectName: "runtimeOperator",
		L3SubjectName: "runtimeAdmin",
	}
}

func removeFinalizers(t *testing.T, shootInterface gardener_apis.ShootInterface, shoot *gardener_types.Shoot) *gardener_types.Shoot {
	shoot.SetFinalizers([]string{})

	update, err := shootInterface.Update(context.Background(), shoot, metav1.UpdateOptions{})
	require.NoError(t, err)
	return update
}

func simulateSuccessfulClusterProvisioning(t *testing.T, f gardener_apis.ShootInterface, s v1core.SecretInterface, shootName string) {
	simulateDNSAdmissionPluginRun(t, f, shootName)
	setShootStatusToSuccessful(t, f, shootName)
	createKubeconfigSecret(t, s, shootName)
	ensureShootSeedName(t, f, shootName)
}

func simulateShootUpgrade(t *testing.T, f gardener_apis.ShootInterface, shootName string) {
	s, err := f.Get(context.Background(), shootName, metav1.GetOptions{})
	require.NoError(t, err)

	s.Status.ObservedGeneration = s.ObjectMeta.Generation + 1

	_, err = f.Update(context.Background(), s, metav1.UpdateOptions{})
	require.NoError(t, err)
}

func ensureShootSeedName(t *testing.T, f gardener_apis.ShootInterface, shootName string) {
	s, err := f.Get(context.Background(), shootName, metav1.GetOptions{})
	require.NoError(t, err)

	if util.IsNilOrEmpty(s.Spec.SeedName) {
		s.Spec.SeedName = util.StringPtr(gardenerGenSeed)

		_, err = f.Update(context.Background(), s, metav1.UpdateOptions{})
		require.NoError(t, err)
	}
}

func simulateDNSAdmissionPluginRun(t *testing.T, f gardener_apis.ShootInterface, shootName string) {
	s, err := f.Get(context.Background(), shootName, metav1.GetOptions{})
	require.NoError(t, err)

	s.Spec.DNS = &gardener_types.DNS{Domain: util.StringPtr("domain")}

	_, err = f.Update(context.Background(), s, metav1.UpdateOptions{})
	require.NoError(t, err)
}

func setShootStatusToSuccessful(t *testing.T, f gardener_apis.ShootInterface, shootName string) {
	s, err := f.Get(context.Background(), shootName, metav1.GetOptions{})
	require.NoError(t, err)

	s.Status.LastOperation = &gardener_types.LastOperation{State: gardener_types.LastOperationStateSucceeded}

	_, err = f.Update(context.Background(), s, metav1.UpdateOptions{})
	require.NoError(t, err)
}

func createKubeconfigSecret(t *testing.T, s v1core.SecretInterface, shootName string) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s.kubeconfig", shootName),
			Namespace: namespace,
		},
		Data: map[string][]byte{"kubeconfig": []byte(mockedKubeconfig)},
	}

	_, err := s.Create(context.Background(), secret, metav1.CreateOptions{})
	require.NoError(t, err)
}

func setupSecretsClient(t *testing.T, config *rest.Config) v1core.SecretInterface {
	coreClient, err := v1core.NewForConfig(config)
	require.NoError(t, err)

	return coreClient.Secrets(namespace)
}
