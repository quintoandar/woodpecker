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
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2"

	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
)

const dirGraphQLQuery = `
query($owner: String!, $name: String!, $expression: String!) {
  repository(owner: $owner, name: $name) {
    object(expression: $expression) {
      ... on Tree {
        entries {
          name
          type
          object {
            ... on Blob {
              text
              isBinary
              isTruncated
              byteSize
            }
          }
        }
      }
    }
  }
}
`

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type dirGraphQLResponse struct {
	Data struct {
		Repository *struct {
			Object *struct {
				Entries []struct {
					Name   string `json:"name"`
					Type   string `json:"type"`
					Object *struct {
						Text         *string `json:"text"`
						IsBinary     *bool   `json:"isBinary"`
						IsTruncated  bool    `json:"isTruncated"`
						ByteSize     int     `json:"byteSize"`
					} `json:"object"`
				} `json:"entries"`
			} `json:"object"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// graphqlEndpoint returns the GraphQL API URL for this forge client.
// GitHub.com uses https://api.github.com/graphql; GitHub Enterprise uses {url}/api/graphql.
func (c *client) graphqlEndpoint() string {
	if c.API == defaultAPI || strings.HasPrefix(c.API, "https://api.github.com") {
		return "https://api.github.com/graphql"
	}
	return strings.TrimSuffix(c.url, "/") + "/api/graphql"
}

func (c *client) newHTTPClient(ctx context.Context, token string) *http.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	if c.SkipVerify {
		tp, _ := tc.Transport.(*oauth2.Transport)
		tp.Base = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
			},
		}
	}
	return tc
}

// dirGraphQL fetches a single directory level of files via the GitHub GraphQL API,
// returning blob contents inline to avoid the REST Contents N+1 fan-out.
func (c *client) dirGraphQL(ctx context.Context, token, owner, name, commit, path string) ([]*forge_types.FileMeta, error) {
	path = strings.Trim(path, "/")
	expression := commit + ":" + path

	body, err := json.Marshal(graphqlRequest{
		Query: dirGraphQLQuery,
		Variables: map[string]any{
			"owner":      owner,
			"name":       name,
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

	resp, err := c.newHTTPClient(ctx, token).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, &forge_types.ErrConfigNotFound{Configs: []string{path}}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github graphql: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed dirGraphQLResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("github graphql: decode response: %w", err)
	}
	if len(parsed.Errors) > 0 {
		msgs := make([]string, 0, len(parsed.Errors))
		for _, e := range parsed.Errors {
			msgs = append(msgs, e.Message)
		}
		joined := strings.Join(msgs, "; ")
		lower := strings.ToLower(joined)
		if strings.Contains(lower, "could not resolve") || strings.Contains(lower, "not found") {
			return nil, errors.Join(fmt.Errorf("github graphql: %s", joined), &forge_types.ErrConfigNotFound{Configs: []string{path}})
		}
		return nil, fmt.Errorf("github graphql: %s", joined)
	}

	if parsed.Data.Repository == nil || parsed.Data.Repository.Object == nil {
		return nil, &forge_types.ErrConfigNotFound{Configs: []string{path}}
	}

	var files []*forge_types.FileMeta
	for _, entry := range parsed.Data.Repository.Object.Entries {
		if entry.Type != "blob" {
			continue
		}
		fullName := path + "/" + entry.Name
		if entry.Object == nil {
			return nil, fmt.Errorf("github graphql: missing blob object for %s", fullName)
		}
		if entry.Object.IsBinary != nil && *entry.Object.IsBinary {
			continue
		}
		if entry.Object.IsTruncated {
			return nil, fmt.Errorf("github graphql: blob %s is truncated", fullName)
		}
		if entry.Object.Text == nil {
			return nil, fmt.Errorf("github graphql: blob %s has no text content", fullName)
		}
		files = append(files, &forge_types.FileMeta{
			Name: fullName,
			Data: []byte(*entry.Object.Text),
		})
	}

	return files, nil
}
