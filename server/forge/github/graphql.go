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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
)

const (
	graphqlErrorTypeNotFound = "NOT_FOUND"

	dirGraphQLQuery = `
query($owner: String!, $name: String!, $expression: String!) {
  repository(owner: $owner, name: $name) {
    object(expression: $expression) {
      ... on Tree {
        entries {
          name
          path
          type
          object {
            ... on Blob {
              text
              isBinary
              isTruncated
            }
          }
        }
      }
    }
  }
}
`
)

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphqlError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type dirBlob struct {
	Text        *string `json:"text"`
	IsBinary    *bool   `json:"isBinary"`
	IsTruncated bool    `json:"isTruncated"`
}

type dirEntry struct {
	Name   string   `json:"name"`
	Path   string   `json:"path"`
	Type   string   `json:"type"`
	Object *dirBlob `json:"object"`
}

type dirTree struct {
	Entries []dirEntry `json:"entries"`
}

type dirRepository struct {
	Object *dirTree `json:"object"`
}

type dirGraphQLResponse struct {
	Data struct {
		Repository *dirRepository `json:"repository"`
	} `json:"data"`
	Errors []graphqlError `json:"errors"`
}

// graphqlEndpoint returns the GraphQL API URL for this forge client.
// GitHub.com uses https://api.github.com/graphql; GitHub Enterprise uses {url}/api/graphql.
func (c *client) graphqlEndpoint() string {
	if c.url == defaultURL || c.API == defaultAPI {
		return "https://api.github.com/graphql"
	}
	return strings.TrimSuffix(c.url, "/") + "/api/graphql"
}

// dirGraphQL fetches a single directory level of files via the GitHub GraphQL API,
// returning blob contents inline to avoid the REST Contents N+1 fan-out.
// Truncated or incomplete blobs fall back to REST File() for that path only.
func (c *client) dirGraphQL(ctx context.Context, u *model.User, r *model.Repo, b *model.Pipeline, dirPath string) ([]*forge_types.FileMeta, error) {
	dirPath = strings.Trim(dirPath, "/")
	expression := b.Commit + ":" + dirPath

	body, err := json.Marshal(graphqlRequest{
		Query: dirGraphQLQuery,
		Variables: map[string]any{
			"owner":      r.Owner,
			"name":       r.Name,
			"expression": expression,
		},
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlEndpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	httpClient, err := c.newOAuthHTTPClient(ctx, u.AccessToken)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github graphql: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed dirGraphQLResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("github graphql: decode response: %w", err)
	}
	if err := graphQLResponseError(parsed.Errors, dirPath); err != nil {
		return nil, err
	}
	if parsed.Data.Repository == nil || parsed.Data.Repository.Object == nil {
		return nil, &forge_types.ErrConfigNotFound{Configs: []string{dirPath}}
	}

	return c.entriesToFileMeta(ctx, u, r, b, dirPath, parsed.Data.Repository.Object.Entries)
}

func graphQLResponseError(errs []graphqlError, dirPath string) error {
	if len(errs) == 0 {
		return nil
	}

	msgs := make([]string, 0, len(errs))
	notFound := false
	for _, e := range errs {
		msgs = append(msgs, e.Message)
		if e.Type == graphqlErrorTypeNotFound {
			notFound = true
		}
	}
	joined := strings.Join(msgs, "; ")
	if notFound {
		return errors.Join(fmt.Errorf("github graphql: %s", joined), &forge_types.ErrConfigNotFound{Configs: []string{dirPath}})
	}
	return fmt.Errorf("github graphql: %s", joined)
}

func entryPath(dirPath string, entry dirEntry) string {
	if entry.Path != "" {
		return entry.Path
	}
	return path.Join(dirPath, entry.Name)
}

func (c *client) entriesToFileMeta(ctx context.Context, u *model.User, r *model.Repo, b *model.Pipeline, dirPath string, entries []dirEntry) ([]*forge_types.FileMeta, error) {
	var files []*forge_types.FileMeta
	for _, entry := range entries {
		if entry.Type != "blob" {
			continue
		}
		fullName := entryPath(dirPath, entry)
		if entry.Object != nil && entry.Object.IsBinary != nil && *entry.Object.IsBinary {
			continue
		}

		needsREST := entry.Object == nil || entry.Object.IsTruncated || entry.Object.Text == nil
		if needsREST {
			content, err := c.File(ctx, u, r, b, fullName)
			if err != nil {
				return nil, fmt.Errorf("github graphql: rest fallback for %s: %w", fullName, err)
			}
			files = append(files, &forge_types.FileMeta{Name: fullName, Data: content})
			continue
		}

		files = append(files, &forge_types.FileMeta{
			Name: fullName,
			Data: []byte(*entry.Object.Text),
		})
	}
	return files, nil
}
