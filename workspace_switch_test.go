package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeRepoFast(t *testing.T) {
	tmp := t.TempDir()

	// 1. Plain directory without .git
	plainDir := filepath.Join(tmp, "plain")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if isGit, isWt, branch := probeRepoFast(plainDir); isGit || isWt || branch != "" {
		t.Fatalf("expected non-git dir, got isGit=%v isWt=%v branch=%q", isGit, isWt, branch)
	}

	// 2. Standard .git directory with branch ref
	repoDir := filepath.Join(tmp, "repo")
	gitDir := filepath.Join(repoDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feat/hot-swap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	isGit, isWt, branch := probeRepoFast(repoDir)
	if !isGit || isWt || branch != "feat/hot-swap" {
		t.Fatalf("expected standard git repo on feat/hot-swap, got isGit=%v isWt=%v branch=%q", isGit, isWt, branch)
	}

	// 3. Detached HEAD SHA
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	isGit, isWt, branch = probeRepoFast(repoDir)
	if !isGit || isWt || branch != "a1b2c3d" {
		t.Fatalf("expected detached HEAD short SHA a1b2c3d, got isGit=%v isWt=%v branch=%q", isGit, isWt, branch)
	}

	// 4. Linked worktree with .git file pointing to gitdir
	wtGitDir := filepath.Join(gitDir, "worktrees", "wt-one")
	if err := os.MkdirAll(wtGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "HEAD"), []byte("ref: refs/heads/wt-branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wtDir := filepath.Join(tmp, "wt-one")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, ".git"), []byte("gitdir: "+wtGitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	isGit, isWt, branch = probeRepoFast(wtDir)
	if !isGit || !isWt || branch != "wt-branch" {
		t.Fatalf("expected worktree on wt-branch, got isGit=%v isWt=%v branch=%q", isGit, isWt, branch)
	}
}

func TestWorkspaceDiscoveryAndRoots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	tmp := t.TempDir()
	currentDir := filepath.Join(tmp, "current-repo")
	if err := os.MkdirAll(filepath.Join(currentDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentDir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(currentDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rootsContainer := filepath.Join(tmp, "projects")
	repoAlpha := filepath.Join(rootsContainer, "repo-alpha")
	repoBeta := filepath.Join(rootsContainer, "nested", "repo-beta")
	for _, r := range []struct {
		dir    string
		branch string
	}{
		{repoAlpha, "main"},
		{repoBeta, "develop"},
	} {
		if err := os.MkdirAll(filepath.Join(r.dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.dir, ".git", "HEAD"), []byte("ref: refs/heads/"+r.branch+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a linked worktree inside repoAlpha/.claude/worktrees/wt-fix
	wtMetaDir := filepath.Join(repoAlpha, ".git", "worktrees", "wt-fix")
	if err := os.MkdirAll(wtMetaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtMetaDir, "HEAD"), []byte("ref: refs/heads/fix/bug\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wtCheckout := filepath.Join(repoAlpha, ".claude", "worktrees", "wt-fix")
	if err := os.MkdirAll(wtCheckout, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtCheckout, ".git"), []byte("gitdir: "+wtMetaDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ix := NewIndex(currentDir)
	ix.Build()
	srv := NewServer(ix, newLSPManager(currentDir, false), "")

	// Add rootsContainer via POST /api/workspaces/roots
	req := newLocalPostRequest(t, "/api/workspaces/roots", map[string]any{"add": rootsContainer})
	rec := httptest.NewRecorder()
	srv.handleWorkspaceRoots(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleWorkspaceRoots add status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Current         string          `json:"current"`
		Name            string          `json:"name"`
		ConfiguredRoots []string        `json:"configuredRoots"`
		Repos           []WorkspaceRepo `json:"repos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.ConfiguredRoots) != 1 {
		t.Fatalf("expected 1 configured root, got %v", resp.ConfiguredRoots)
	}

	found := make(map[string]WorkspaceRepo)
	for _, r := range resp.Repos {
		found[r.Name] = r
	}
	if _, ok := found["current-repo"]; !ok || !found["current-repo"].IsCurrent {
		t.Fatalf("expected current-repo with IsCurrent=true, got %+v", found["current-repo"])
	}
	if r, ok := found["repo-alpha"]; !ok || r.Branch != "main" {
		t.Fatalf("expected repo-alpha on main, got %+v", r)
	}
	if r, ok := found["repo-beta"]; !ok || r.Branch != "develop" {
		t.Fatalf("expected repo-beta on develop, got %+v", r)
	}
	if r, ok := found["wt-fix"]; !ok || !r.IsWorktree || r.Branch != "fix/bug" {
		t.Fatalf("expected worktree wt-fix on fix/bug, got %+v", r)
	}

	// Remove rootsContainer via POST /api/workspaces/roots
	reqRem := newLocalPostRequest(t, "/api/workspaces/roots", map[string]any{"remove": rootsContainer})
	recRem := httptest.NewRecorder()
	srv.handleWorkspaceRoots(recRem, reqRem)
	if recRem.Code != http.StatusOK {
		t.Fatalf("handleWorkspaceRoots remove status=%d body=%s", recRem.Code, recRem.Body.String())
	}
	if err := json.Unmarshal(recRem.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.ConfiguredRoots) != 0 {
		t.Fatalf("expected 0 configured roots after remove, got %v", resp.ConfiguredRoots)
	}
}

func TestWorkspaceSwitchInPlace(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	tmp := t.TempDir()
	repoA := filepath.Join(tmp, "repo-a")
	repoB := filepath.Join(tmp, "repo-b")

	if err := os.MkdirAll(repoA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoA, "alpha.txt"), []byte("hello from alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(repoB, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoB, ".git", "HEAD"), []byte("ref: refs/heads/next\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoB, "beta.txt"), []byte("hello from beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ix := NewIndex(repoA)
	ix.Build()
	srv := NewServer(ix, newLSPManager(repoA, false), "")

	// Switch to repoB via handleWorkspaceSwitch
	req := newLocalPostRequest(t, "/api/workspaces/switch", map[string]any{"path": repoB})
	rec := httptest.NewRecorder()
	srv.handleWorkspaceSwitch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleWorkspaceSwitch status=%d body=%s", rec.Code, rec.Body.String())
	}

	var switchResp struct {
		OK   bool           `json:"ok"`
		Root string         `json:"root"`
		Name string         `json:"name"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &switchResp); err != nil {
		t.Fatal(err)
	}
	if !switchResp.OK || switchResp.Name != "repo-b" {
		t.Fatalf("unexpected switch response: %+v", switchResp)
	}
	if metaName, _ := switchResp.Meta["name"].(string); metaName != "repo-b" {
		t.Fatalf("expected meta.name=repo-b, got %v", switchResp.Meta["name"])
	}

	// Verify handleTree now serves repoB's files
	reqTree := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	recTree := httptest.NewRecorder()
	srv.handleTree(recTree, reqTree)
	if recTree.Code != http.StatusOK {
		t.Fatalf("handleTree status=%d", recTree.Code)
	}
	var treeResp struct {
		Children []struct {
			Path string `json:"path"`
		} `json:"children"`
	}
	if err := json.Unmarshal(recTree.Body.Bytes(), &treeResp); err != nil {
		t.Fatal(err)
	}
	if len(treeResp.Children) != 1 || treeResp.Children[0].Path != "beta.txt" {
		t.Fatalf("expected [beta.txt] after switch, got %+v", treeResp.Children)
	}
}
