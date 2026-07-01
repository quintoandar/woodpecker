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

package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvironPullRequestDraft(t *testing.T) {
	m := Metadata{
		Sys: System{Name: "wp"},
		Curr: Pipeline{
			Event: EventPull,
			Commit: Commit{
				ChangedFiles:     []string{"readme", "license"},
				Refspec:          "branch-a:branch-b",
				PullRequestDraft: true,
			},
		},
	}

	envs := m.Environ()
	assert.Equal(t, "true", envs["CI_COMMIT_PULL_REQUEST_DRAFT"])

	m = Metadata{
		Sys: System{Name: "wp"},
		Curr: Pipeline{
			Event: EventPull,
			Commit: Commit{
				Refspec: "branch-a:branch-b",
			},
		},
	}

	envs = m.Environ()
	assert.Equal(t, "false", envs["CI_COMMIT_PULL_REQUEST_DRAFT"])
}
