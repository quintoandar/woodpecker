// Copyright 2024 Woodpecker Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

func TestHook(t *testing.T) {
	gin.SetMode(gin.TestMode)

	_manager := mocks_services.NewManager(t)
	_forge := mocks_forge.NewForge(t)
	_store := mocks_store.NewStore(t)
	_configService := mocks_config_service.NewService(t)
	_secretService := mocks_secret_service.NewService(t)
	_registryService := mocks_registry_service.NewService(t)
	server.Config.Services.Manager = _manager
	server.Config.Permissions.Open = true
	server.Config.Permissions.Orgs = permissions.NewOrgs(nil)
	server.Config.Permissions.Admins = permissions.NewAdmins(nil)
	server.Config.Server.WebhookSyncTimeout = 0 // fully synchronous for this test
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("store", _store)
	user := &model.User{
		ID: 123,
	}
	repo := &model.Repo{
		ID:            123,
		ForgeRemoteID: "123",
		Owner:         "owner",
		Name:          "name",
		IsActive:      true,
		UserID:        user.ID,
		Hash:          "secret-123-this-is-a-secret",
	}
	pipeline := &model.Pipeline{
		ID:     123,
		RepoID: repo.ID,
		Event:  model.EventPush,
	}

	repoToken := token.New(token.HookToken)
	repoToken.Set("repo-id", fmt.Sprintf("%d", repo.ID))
	signedToken, err := repoToken.Sign("secret-123-this-is-a-secret")
	assert.NoError(t, err)

	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", signedToken))
	c.Request = &http.Request{
		Header: header,
		URL: &url.URL{
			Scheme: "https",
		},
	}

	_manager.On("ForgeFromRepo", repo).Return(_forge, nil)
	_forge.On("Hook", mock.Anything, mock.Anything).Return(repo, pipeline, nil)
	_store.On("GetRepo", repo.ID).Return(repo, nil)
	_store.On("GetUser", user.ID).Return(user, nil)
	_store.On("UpdateRepo", repo).Return(nil)
	_store.On("CreatePipeline", mock.Anything).Return(nil)
	_manager.On("ConfigServiceFromRepo", repo).Return(_configService)
	_configService.On("Fetch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	_forge.On("Netrc", mock.Anything, mock.Anything).Return(&model.Netrc{}, nil)
	_store.On("GetPipelineLastBefore", mock.Anything, mock.Anything, mock.Anything).Return(nil, nil)
	_manager.On("SecretServiceFromRepo", repo).Return(_secretService)
	_secretService.On("SecretListPipeline", repo, mock.Anything, mock.Anything).Return(nil, nil)
	_manager.On("RegistryServiceFromRepo", repo).Return(_registryService)
	_registryService.On("RegistryListPipeline", repo, mock.Anything).Return(nil, nil)
	_manager.On("EnvironmentService").Return(nil)
	_store.On("DeletePipeline", mock.Anything).Return(nil)

	api.PostHook(c)

	assert.Equal(t, http.StatusNoContent, c.Writer.Status())
	assert.Equal(t, "true", w.Header().Get("Pipeline-Filtered"))
}

func TestHookAsyncAcceptedWhenCreateExceedsSyncTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	_manager := mocks_services.NewManager(t)
	_forge := mocks_forge.NewForge(t)
	_store := mocks_store.NewStore(t)
	_configService := mocks_config_service.NewService(t)
	server.Config.Services.Manager = _manager
	server.Config.Permissions.Open = true
	server.Config.Permissions.Orgs = permissions.NewOrgs(nil)
	server.Config.Permissions.Admins = permissions.NewAdmins(nil)
	server.Config.Server.WebhookSyncTimeout = 50 * time.Millisecond

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("store", _store)

	user := &model.User{ID: 123}
	repo := &model.Repo{
		ID:            123,
		ForgeRemoteID: "123",
		Owner:         "owner",
		Name:          "name",
		FullName:      "owner/name",
		IsActive:      true,
		UserID:        user.ID,
		Hash:          "secret-123-this-is-a-secret",
	}
	pipeline := &model.Pipeline{
		ID:     123,
		RepoID: repo.ID,
		Event:  model.EventPush,
	}

	repoToken := token.New(token.HookToken)
	repoToken.Set("repo-id", fmt.Sprintf("%d", repo.ID))
	signedToken, err := repoToken.Sign("secret-123-this-is-a-secret")
	require.NoError(t, err)

	c.Request = &http.Request{
		Header: http.Header{"Authorization": []string{fmt.Sprintf("Bearer %s", signedToken)}},
		URL:    &url.URL{Scheme: "https"},
	}

	var fetchStarted sync.WaitGroup
	fetchStarted.Add(1)
	createDone := make(chan struct{})

	_manager.On("ForgeFromRepo", repo).Return(_forge, nil)
	_forge.On("Hook", mock.Anything, mock.Anything).Return(repo, pipeline, nil)
	_store.On("GetRepo", repo.ID).Return(repo, nil)
	_store.On("GetUser", user.ID).Return(user, nil)
	_store.On("UpdateRepo", repo).Return(nil)
	_store.On("CreatePipeline", mock.Anything).Return(nil)
	_manager.On("ConfigServiceFromRepo", repo).Return(_configService)
	_configService.On("Fetch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			fetchStarted.Done()
			// Block long enough that the sync timeout fires, then finish.
			time.Sleep(200 * time.Millisecond)
		}).
		Return(nil, &forge_types.ErrConfigNotFound{Configs: []string{".woodpecker/"}})
	_store.On("DeletePipeline", mock.Anything).Run(func(args mock.Arguments) {
		close(createDone)
	}).Return(nil)

	start := time.Now()
	api.PostHook(c)
	elapsed := time.Since(start)

	assert.Equal(t, http.StatusAccepted, c.Writer.Status())
	assert.Less(t, elapsed, 150*time.Millisecond, "handler should return soon after sync timeout")

	// Background create must still run (Fetch was started) and complete.
	fetchStarted.Wait()
	select {
	case <-createDone:
	case <-time.After(2 * time.Second):
		t.Fatal("background pipeline create did not finish")
	}
}

func TestHookBackgroundCreateIgnoresRequestContextCancel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	_manager := mocks_services.NewManager(t)
	_forge := mocks_forge.NewForge(t)
	_store := mocks_store.NewStore(t)
	_configService := mocks_config_service.NewService(t)
	server.Config.Services.Manager = _manager
	server.Config.Permissions.Open = true
	server.Config.Permissions.Orgs = permissions.NewOrgs(nil)
	server.Config.Permissions.Admins = permissions.NewAdmins(nil)
	server.Config.Server.WebhookSyncTimeout = 30 * time.Millisecond

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("store", _store)

	user := &model.User{ID: 123}
	repo := &model.Repo{
		ID:            123,
		ForgeRemoteID: "123",
		Owner:         "owner",
		Name:          "name",
		FullName:      "owner/name",
		IsActive:      true,
		UserID:        user.ID,
		Hash:          "secret-123-this-is-a-secret",
	}
	pipeline := &model.Pipeline{
		ID:     123,
		RepoID: repo.ID,
		Event:  model.EventPush,
	}

	repoToken := token.New(token.HookToken)
	repoToken.Set("repo-id", fmt.Sprintf("%d", repo.ID))
	signedToken, err := repoToken.Sign("secret-123-this-is-a-secret")
	require.NoError(t, err)

	reqCtx, reqCancel := context.WithCancel(context.Background())
	c.Request = (&http.Request{
		Header: http.Header{"Authorization": []string{fmt.Sprintf("Bearer %s", signedToken)}},
		URL:    &url.URL{Scheme: "https"},
	}).WithContext(reqCtx)

	var sawLiveCtx sync.WaitGroup
	sawLiveCtx.Add(1)
	createDone := make(chan struct{})

	_manager.On("ForgeFromRepo", repo).Return(_forge, nil)
	_forge.On("Hook", mock.Anything, mock.Anything).Return(repo, pipeline, nil)
	_store.On("GetRepo", repo.ID).Return(repo, nil)
	_store.On("GetUser", user.ID).Return(user, nil)
	_store.On("UpdateRepo", repo).Return(nil)
	_store.On("CreatePipeline", mock.Anything).Return(nil)
	_manager.On("ConfigServiceFromRepo", repo).Return(_configService)
	_configService.On("Fetch", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			reqCancel() // cancel the HTTP request context while Fetch is in flight
			select {
			case <-ctx.Done():
				t.Errorf("background create context was cancelled by request end")
			case <-time.After(80 * time.Millisecond):
				sawLiveCtx.Done()
			}
		}).
		Return(nil, &forge_types.ErrConfigNotFound{Configs: []string{".woodpecker/"}})
	_store.On("DeletePipeline", mock.Anything).Run(func(args mock.Arguments) {
		close(createDone)
	}).Return(nil)

	api.PostHook(c)
	assert.Equal(t, http.StatusAccepted, c.Writer.Status())

	sawLiveCtx.Wait()
	select {
	case <-createDone:
	case <-time.After(2 * time.Second):
		t.Fatal("background pipeline create did not finish")
	}
}
