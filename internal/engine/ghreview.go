package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ReviewThread is a single review conversation on a PR.
type ReviewThread struct {
	ThreadID string          `json:"threadId"`
	Path     string          `json:"path"`
	Line     int             `json:"line"`
	Comments []ThreadComment `json:"comments"`
}

// ThreadComment is a single comment within a review thread.
type ThreadComment struct {
	DatabaseID int    `json:"id"`
	Body       string `json:"body"`
	Author     string `json:"author"`
	CreatedAt  string `json:"createdAt"`
	URL        string `json:"url"`
}

// ghRepoInfo holds the owner and repo name for GraphQL queries.
type ghRepoInfo struct {
	Owner string
	Repo  string
}

// ghGetRepoInfo returns the owner and repo name for the current repository.
func ghGetRepoInfo() (*ghRepoInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "owner,name")
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("could not determine repo: %w", err)
	}

	var result struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("could not parse repo info: %w", err)
	}

	return &ghRepoInfo{Owner: result.Owner.Login, Repo: result.Name}, nil
}

// ghFetchReviewThreads fetches unresolved review threads for a PR.
func ghFetchReviewThreads(repo *ghRepoInfo, prNumber int) ([]ReviewThread, error) {
	query := `
query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) {
        nodes {
          id
          isResolved
          line
          path
          comments(first: 50) {
            nodes {
              databaseId
              body
              author { login }
              createdAt
              url
            }
          }
        }
      }
    }
  }
}`

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-f", fmt.Sprintf("query=%s", query),
		"-f", fmt.Sprintf("owner=%s", repo.Owner),
		"-f", fmt.Sprintf("repo=%s", repo.Repo),
		"-F", fmt.Sprintf("number=%d", prNumber),
	)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("GraphQL query failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}

	var response struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							ID         string `json:"id"`
							IsResolved bool   `json:"isResolved"`
							Line       int    `json:"line"`
							Path       string `json:"path"`
							Comments   struct {
								Nodes []struct {
									DatabaseID int    `json:"databaseId"`
									Body       string `json:"body"`
									Author     struct {
										Login string `json:"login"`
									} `json:"author"`
									CreatedAt string `json:"createdAt"`
									URL       string `json:"url"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		return nil, fmt.Errorf("could not parse GraphQL response: %w", err)
	}

	var threads []ReviewThread
	for _, node := range response.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if node.IsResolved {
			continue
		}

		thread := ReviewThread{
			ThreadID: node.ID,
			Path:     node.Path,
			Line:     node.Line,
		}

		for _, c := range node.Comments.Nodes {
			thread.Comments = append(thread.Comments, ThreadComment{
				DatabaseID: c.DatabaseID,
				Body:       c.Body,
				Author:     c.Author.Login,
				CreatedAt:  c.CreatedAt,
				URL:        c.URL,
			})
		}

		if len(thread.Comments) > 0 {
			threads = append(threads, thread)
		}
	}

	return threads, nil
}

// ghReplyToComment posts a reply to a review comment.
func ghReplyToComment(repo *ghRepoInfo, prNumber, commentID int, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	endpoint := fmt.Sprintf("repos/%s/%s/pulls/%d/comments", repo.Owner, repo.Repo, prNumber)
	cmd := exec.CommandContext(ctx, "gh", "api", endpoint,
		"-f", fmt.Sprintf("body=%s", body),
		"-F", fmt.Sprintf("in_reply_to=%d", commentID),
	)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reply failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return nil
}

// ghResolveThread resolves a review thread via GraphQL mutation.
func ghResolveThread(threadID string) error {
	mutation := `
mutation($threadId: ID!) {
  resolveReviewThread(input: {threadId: $threadId}) {
    thread { isResolved }
  }
}`

	ctx, cancel := context.WithTimeout(context.Background(), ghTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "api", "graphql",
		"-f", fmt.Sprintf("query=%s", mutation),
		"-f", fmt.Sprintf("threadId=%s", threadID),
	)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("resolve failed: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return nil
}
