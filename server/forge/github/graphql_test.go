// Copyright 2026 Woodpecker Authors
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

package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
)

func TestGraphQLEndpoint(t *testing.T) {
	t.Parallel()

	cloud, err := New(Opts{URL: defaultURL})
	require.NoError(t, err)
	assert.Equal(t, "https://api.github.com/graphql", cloud.(*client).graphqlEndpoint())
	assert.Equal(t, defaultAPI, cloud.(*client).API)

	ghe, err := New(Opts{URL: "https://ghe.example.com"})
	require.NoError(t, err)
	assert.Equal(t, "https://ghe.example.com/api/graphql", ghe.(*client).graphqlEndpoint())
	assert.Equal(t, "https://ghe.example.com/api/v3/", ghe.(*client).API)
}

func TestDirGraphQL(t *testing.T) {
	t.Parallel()

	const yamlA = "steps:\n  - name: a\n    image: alpine\n"
	const yamlB = "steps:\n  - name: b\n    image: alpine\n"

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/graphql", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "abc123:.woodpecker")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"repository": {
					"object": {
						"entries": [
							{"name": "a.yml", "type": "blob", "object": {"text": ` + jsonString(yamlA) + `, "isBinary": false, "isTruncated": false, "byteSize": 10}},
							{"name": "b.yaml", "type": "blob", "object": {"text": ` + jsonString(yamlB) + `, "isBinary": false, "isTruncated": false, "byteSize": 10}},
							{"name": "notes.txt", "type": "blob", "object": {"text": "ignore", "isBinary": false, "isTruncated": false, "byteSize": 6}},
							{"name": "subdir", "type": "tree", "object": null},
							{"name": "logo.png", "type": "blob", "object": {"text": null, "isBinary": true, "isTruncated": false, "byteSize": 100}}
						]
					}
				}
			}
		}`))
	}))
	defer s.Close()

	forge, err := New(Opts{URL: s.URL, SkipVerify: true})
	require.NoError(t, err)

	files, err := forge.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.NoError(t, err)

	byName := map[string]string{}
	for _, f := range files {
		byName[f.Name] = string(f.Data)
	}
	assert.Equal(t, yamlA, byName[".woodpecker/a.yml"])
	assert.Equal(t, yamlB, byName[".woodpecker/b.yaml"])
	assert.Equal(t, "ignore", byName[".woodpecker/notes.txt"])
	assert.NotContains(t, byName, ".woodpecker/subdir")
	assert.NotContains(t, byName, ".woodpecker/logo.png")
}

func TestDirGraphQLMissingTree(t *testing.T) {
	t.Parallel()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"repository":{"object":null}}}`))
	}))
	defer s.Close()

	forge, err := New(Opts{URL: s.URL, SkipVerify: true})
	require.NoError(t, err)

	_, err = forge.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.Error(t, err)
	assert.True(t, errors.Is(err, &forge_types.ErrConfigNotFound{}))
}

func TestDirGraphQLTruncatedBlob(t *testing.T) {
	t.Parallel()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"repository": {
					"object": {
						"entries": [
							{"name": "big.yml", "type": "blob", "object": {"text": "partial", "isBinary": false, "isTruncated": true, "byteSize": 999999}}
						]
					}
				}
			}
		}`))
	}))
	defer s.Close()

	forge, err := New(Opts{URL: s.URL, SkipVerify: true})
	require.NoError(t, err)

	_, err = forge.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "truncated")
}

func TestDirGraphQLHTTPError(t *testing.T) {
	t.Parallel()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("[]"))
	}))
	defer s.Close()

	forge, err := New(Opts{URL: s.URL, SkipVerify: true})
	require.NoError(t, err)

	_, err = forge.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
