package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.woodpecker-ci.org/woodpecker/v3/server"
	"go.woodpecker-ci.org/woodpecker/v3/server/api"
	mocks_forge "go.woodpecker-ci.org/woodpecker/v3/server/forge/mocks"
	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
	mocks_config_service "go.woodpecker-ci.org/woodpecker/v3/server/services/config/mocks"
	mocks_services "go.woodpecker-ci.org/woodpecker/v3/server/services/mocks"
	"go.woodpecker-ci.org/woodpecker/v3/server/services/permissions"
	mocks_registry_service "go.woodpecker-ci.org/woodpecker/v3/server/services/registry/mocks"
	mocks_secret_service "go.woodpecker-ci.org/woodpecker/v3/server/services/secret/mocks"
	mocks_store "go.woodpecker-ci.org/woodpecker/v3/server/store/mocks"
	"go.woodpecker-ci.org/woodpecker/v3/shared/token"
)

type hookFixture struct {
	c             *gin.Context
	w             *httptest.ResponseRecorder
	manager       *mocks_services.Manager
	forge         *mocks_forge.Forge
	store         *mocks_store.Store
	configService *mocks_config_service.Service
	user          *model.User
	repo          *model.Repo
	pipeline      *model.Pipeline
}

func newHookFixture(t *testing.T, syncTimeout time.Duration) *hookFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	f := &hookFixture{
		manager:       mocks_services.NewManager(t),
		forge:         mocks_forge.NewForge(t),
		store:         mocks_store.NewStore(t),
		configService: mocks_config_service.NewService(t),
		w:             httptest.NewRecorder(),
		user:          &model.User{ID: 123},
	}
	f.repo = &model.Repo{
		ID:            123,
		ForgeRemoteID: "123",
		Owner:         "owner",
		Name:          "name",
		FullName:      "owner/name",
		IsActive:      true,
		UserID:        f.user.ID,
		Hash:          "secret-123-this-is-a-secret",
	}
	f.pipeline = &model.Pipeline{
		ID:     123,
		RepoID: f.repo.ID,
		Event:  model.EventPush,
	}

	server.Config.Services.Manager = f.manager
	server.Config.Permissions.Open = true
	server.Config.Permissions.Orgs = permissions.NewOrgs(nil)
	server.Config.Permissions.Admins = permissions.NewAdmins(nil)
	server.Config.Server.WebhookSyncTimeout = syncTimeout

	f.c, _ = gin.CreateTestContext(f.w)
	f.c.Set("store", f.store)

	repoToken := token.New(token.HookToken)
	repoToken.Set("repo-id", fmt.Sprintf("%d", f.repo.ID))
	signedToken, err := repoToken.Sign(f.repo.Hash)
	require.NoError(t, err)

	f.c.Request = &http.Request{
		Header: http.Header{"Authorization": []string{fmt.Sprintf("Bearer %s", signedToken)}},
		URL:    &url.URL{Scheme: "https"},
	}

	f.manager.On("ForgeFromRepo", f.repo).Return(f.forge, nil)
	f.forge.On("Hook", mock.Anything, mock.Anything).Return(f.repo, f.pipeline, nil)
	f.store.On("GetRepo", f.repo.ID).Return(f.repo, nil)
	f.store.On("GetUser", f.user.ID).Return(f.user, nil)
	f.store.On("UpdateRepo", f.repo).Return(nil)
	f.store.On("CreatePipeline", mock.Anything).Return(nil)
	f.manager.On("ConfigServiceFromRepo", f.repo).Return(f.configService)

	return f
}

func (f *hookFixture) expectFilteredCreatePath(t *testing.T) {
	t.Helper()
	secretService := mocks_secret_service.NewService(t)
	registryService := mocks_registry_service.NewService(t)
	f.configService.On("Fetch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	f.forge.On("Netrc", mock.Anything, mock.Anything).Return(&model.Netrc{}, nil)
	f.store.On("GetPipelineLastBefore", mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	f.manager.On("SecretServiceFromRepo", f.repo).Return(secretService)
	secretService.On("SecretListPipeline", f.repo, mock.Anything, mock.Anything).Return(nil, nil)
	f.manager.On("RegistryServiceFromRepo", f.repo).Return(registryService)
	registryService.On("RegistryListPipeline", f.repo, mock.Anything).Return(nil, nil)
	f.manager.On("EnvironmentService").Return(nil)
	f.store.On("DeletePipeline", mock.Anything).Return(nil)
}

func TestHook(t *testing.T) {
	f := newHookFixture(t, 0) // fully wait for create before responding
	f.expectFilteredCreatePath(t)

	api.PostHook(f.c)

	assert.Equal(t, http.StatusNoContent, f.c.Writer.Status())
	assert.Equal(t, "true", f.w.Header().Get("Pipeline-Filtered"))
}

func TestHookAsyncAcceptedWhenCreateExceedsSyncTimeout(t *testing.T) {
	f := newHookFixture(t, 50*time.Millisecond)

	var fetchStarted sync.WaitGroup
	fetchStarted.Add(1)
	createDone := make(chan struct{})

	f.configService.On("Fetch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			fetchStarted.Done()
			time.Sleep(200 * time.Millisecond)
		}).
		Return(nil, &forge_types.ErrConfigNotFound{Configs: []string{".woodpecker/"}})
	f.store.On("DeletePipeline", mock.Anything).Run(func(args mock.Arguments) {
		close(createDone)
	}).Return(nil)

	start := time.Now()
	api.PostHook(f.c)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusAccepted, f.c.Writer.Status())
	assert.Less(t, elapsed, 150*time.Millisecond, "handler should return soon after sync timeout")

	fetchStarted.Wait()
	select {
	case <-createDone:
	case <-time.After(2 * time.Second):
		t.Fatal("background pipeline create did not finish")
	}
}

func TestHookBackgroundCreateIgnoresRequestContextCancel(t *testing.T) {
	f := newHookFixture(t, 30*time.Millisecond)

	reqCtx, reqCancel := context.WithCancelCause(context.Background())
	f.c.Request = f.c.Request.WithContext(reqCtx)

	var sawLiveCtx sync.WaitGroup
	sawLiveCtx.Add(1)
	createDone := make(chan struct{})

	f.configService.On("Fetch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			ctx, ok := args.Get(0).(context.Context)
			require.True(t, ok)
			reqCancel(nil)
			select {
			case <-ctx.Done():
				t.Errorf("background create context was canceled by request end")
			case <-time.After(80 * time.Millisecond):
				sawLiveCtx.Done()
			}
		}).
		Return(nil, &forge_types.ErrConfigNotFound{Configs: []string{".woodpecker/"}})
	f.store.On("DeletePipeline", mock.Anything).Run(func(args mock.Arguments) {
		close(createDone)
	}).Return(nil)

	api.PostHook(f.c)
	assert.Equal(t, http.StatusAccepted, f.c.Writer.Status())

	sawLiveCtx.Wait()
	select {
	case <-createDone:
	case <-time.After(2 * time.Second):
		t.Fatal("background pipeline create did not finish")
	}
}
