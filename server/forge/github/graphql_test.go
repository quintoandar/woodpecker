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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
)

func TestGraphQLEndpoint(t *testing.T) {
	t.Parallel()

	cloud, err := New(1, Opts{URL: defaultURL})
	require.NoError(t, err)
	cloudClient, ok := cloud.(*client)
	require.True(t, ok)
	assert.Equal(t, "https://api.github.com/graphql", cloudClient.graphqlEndpoint())
	assert.Equal(t, defaultAPI, cloudClient.API)

	ghe, err := New(1, Opts{URL: "https://ghe.example.com/"})
	require.NoError(t, err)
	gheClient, ok := ghe.(*client)
	require.True(t, ok)
	assert.Equal(t, "https://ghe.example.com/api/graphql", gheClient.graphqlEndpoint())
	assert.Equal(t, "https://ghe.example.com/api/v3/", gheClient.API)
}

func withGraphQLServer(t *testing.T, handler http.HandlerFunc) *client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)

	forge, err := New(1, Opts{URL: s.URL, SkipVerify: true})
	require.NoError(t, err)
	forgeClient, ok := forge.(*client)
	require.True(t, ok)
	return forgeClient
}

func TestDirGraphQL(t *testing.T) {
	t.Parallel()

	const yamlA = "steps:\n  - name: a\n    image: alpine\n"
	const yamlB = "steps:\n  - name: b\n    image: alpine\n"

	c := withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/graphql", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "abc123:.woodpecker")
		assert.NotContains(t, string(body), "byteSize")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"repository": {
					"object": {
						"entries": [
							{"name": "a.yml", "path": ".woodpecker/a.yml", "type": "blob", "object": {"text": ` + jsonString(t, yamlA) + `, "isBinary": false, "isTruncated": false}},
							{"name": "b.yaml", "path": ".woodpecker/b.yaml", "type": "blob", "object": {"text": ` + jsonString(t, yamlB) + `, "isBinary": false, "isTruncated": false}},
							{"name": "notes.txt", "path": ".woodpecker/notes.txt", "type": "blob", "object": {"text": "ignore", "isBinary": false, "isTruncated": false}},
							{"name": "subdir", "path": ".woodpecker/subdir", "type": "tree", "object": null},
							{"name": "logo.png", "path": ".woodpecker/logo.png", "type": "blob", "object": {"text": null, "isBinary": true, "isTruncated": false}}
						]
					}
				}
			}
		}`))
	})

	files, err := c.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
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

	c := withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"repository":{"object":null}}}`))
	})

	_, err := c.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.Error(t, err)
	assert.True(t, errors.Is(err, &forge_types.ErrConfigNotFound{}))
}

func TestDirGraphQLNotFoundType(t *testing.T) {
	t.Parallel()

	c := withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {"repository": null},
			"errors": [{"message": "Could not resolve to a Repository with the name 'o/r'.", "type": "NOT_FOUND"}]
		}`))
	})

	_, err := c.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.Error(t, err)
	assert.True(t, errors.Is(err, &forge_types.ErrConfigNotFound{}))
}

func TestDirGraphQLGenericError(t *testing.T) {
	t.Parallel()

	c := withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {"repository": null},
			"errors": [{"message": "API rate limit exceeded", "type": "RATE_LIMITED"}]
		}`))
	})

	_, err := c.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.Error(t, err)
	assert.False(t, errors.Is(err, &forge_types.ErrConfigNotFound{}))
	assert.Contains(t, err.Error(), "rate limit")
}

func TestDirGraphQLTruncatedBlobFallsBackToREST(t *testing.T) {
	t.Parallel()

	const fullYAML = "steps:\n  - name: big\n    image: alpine\n    commands: [echo hi]\n"

	c := withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/graphql":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": {
					"repository": {
						"object": {
							"entries": [
								{"name": "big.yml", "path": ".woodpecker/big.yml", "type": "blob", "object": {"text": "partial", "isBinary": false, "isTruncated": true}}
							]
						}
					}
				}
			}`))
		case strings.Contains(r.URL.Path, "/contents/.woodpecker/big.yml"):
			encoded := base64.StdEncoding.EncodeToString([]byte(fullYAML))
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{
				"name": "big.yml",
				"path": ".woodpecker/big.yml",
				"type": "file",
				"encoding": "base64",
				"content": %s,
				"sha": "abc",
				"size": %d
			}`, jsonString(t, encoded+"\n"), len(fullYAML))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})

	files, err := c.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Equal(t, ".woodpecker/big.yml", files[0].Name)
	assert.Equal(t, fullYAML, string(files[0].Data))
}

func TestDirGraphQLHTTPError(t *testing.T) {
	t.Parallel()

	c := withGraphQLServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("[]"))
	})

	_, err := c.Dir(context.Background(), &model.User{AccessToken: "token"}, &model.Repo{Owner: "o", Name: "r"}, &model.Pipeline{Commit: "abc123"}, ".woodpecker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "502")
}

func jsonString(t testing.TB, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}
