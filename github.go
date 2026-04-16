package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type PR struct {
	Repo    string
	Number  int
	Title   string
	Author  string
	HeadSHA string
}

type prListItem struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	HeadRefOid string `json:"headRefOid"`
	Author     struct {
		Login string `json:"login"`
	} `json:"author"`
}

func listPRs(repo string) ([]PR, error) {
	out, err := exec.Command("gh", "pr", "list",
		"--repo", repo,
		"--json", "number,title,author,headRefOid",
		"--limit", "50",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr list %s: %w", repo, err)
	}

	var items []prListItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, err
	}

	prs := make([]PR, 0, len(items))
	for _, it := range items {
		prs = append(prs, PR{
			Repo:    repo,
			Number:  it.Number,
			Title:   it.Title,
			Author:  it.Author.Login,
			HeadSHA: it.HeadRefOid,
		})
	}
	return prs, nil
}

// fetchPR returns metadata for a single PR via REST. Used to render
// in-progress PRs immediately without waiting for a full repo listing.
// `gh api repos/.../pulls/N` is a single REST call — faster than `gh pr view`,
// which goes through GraphQL with extra round-trips.
func fetchPR(repo string, number int) (PR, error) {
	out, err := exec.Command("gh", "api",
		fmt.Sprintf("repos/%s/pulls/%d", repo, number),
	).Output()
	if err != nil {
		return PR{}, fmt.Errorf("gh api pulls %s#%d: %w", repo, number, err)
	}
	var it struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Sha string `json:"sha"`
		} `json:"head"`
	}
	if err := json.Unmarshal(out, &it); err != nil {
		return PR{}, err
	}
	return PR{
		Repo:    repo,
		Number:  it.Number,
		Title:   it.Title,
		Author:  it.User.Login,
		HeadSHA: it.Head.Sha,
	}, nil
}

type FileChange struct {
	Path      string
	Additions int
	Deletions int
	Blob      string // blob SHA at the PR's current head
	Patch     string // unified diff vs base (may be empty for binary or huge files)
}

type ghAPIFile struct {
	Sha       string `json:"sha"`
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
}

// listPRFiles fetches the PR's changed files via `gh api`, returning per-file
// blob SHAs and patches in a single call.
func listPRFiles(repo string, number int) ([]FileChange, error) {
	out, err := exec.Command("gh", "api",
		"--paginate",
		fmt.Sprintf("repos/%s/pulls/%d/files", repo, number),
	).Output()
	if err != nil {
		return nil, fmt.Errorf("gh api pulls files %s#%d: %w", repo, number, err)
	}
	var items []ghAPIFile
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("parse files json: %w", err)
	}
	files := make([]FileChange, 0, len(items))
	for _, it := range items {
		files = append(files, FileChange{
			Path:      it.Filename,
			Additions: it.Additions,
			Deletions: it.Deletions,
			Blob:      it.Sha,
			Patch:     it.Patch,
		})
	}
	return files, nil
}

