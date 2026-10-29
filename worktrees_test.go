package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initTestWorktreeRepo(t *testing.T) (string, func(dir string, args ...string)) {
	t.Helper()
	if !gitInstalled() {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	runIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_TERMINAL_PROMPT=0",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
		}
	}

	runIn(root, "init", "-b", "main")
	runIn(root, "config", "user.email", "test@example.com")
	runIn(root, "config", "user.name", "Test User")
	runIn(root, "config", "commit.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Base Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(root, "add", "README.md")
	runIn(root, "commit", "-m", "initial commit on main")

	return root, runIn
}

func newLocalPostRequest(t *testing.T, url string, payload any) *http.Request {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestGitListWorktreesAndClassification(t *testing.T) {
	mainRoot, runIn := initTestWorktreeRepo(t)

	// Create a Claude-style worktree under <mainRoot>/.claude/worktrees/claude-session
	claudeWT := filepath.Join(mainRoot, ".claude", "worktrees", "claude-session")
	if err := os.MkdirAll(filepath.Dir(claudeWT), 0o755); err != nil {
		t.Fatal(err)
	}
	runIn(mainRoot, "worktree", "add", "-b", "claude/feature-x", claudeWT, "main")

	// Commit one file in the Claude worktree and leave one dirty untracked file
	if err := os.WriteFile(filepath.Join(claudeWT, "committed.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(claudeWT, "add", "committed.go")
	runIn(claudeWT, "commit", "-m", "claude committed feature")

	if err := os.WriteFile(filepath.Join(claudeWT, "untracked.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if gotBase := detectBaseBranch(mainRoot); gotBase != "main" {
		t.Fatalf("detectBaseBranch = %q, want %q", gotBase, "main")
	}

	wts := gitListWorktrees(claudeWT)
	if len(wts) != 2 {
		t.Fatalf("expected 2 worktrees, got %d: %+v", len(wts), wts)
	}

	var mainEntry, claudeEntry *GitWorktree
	for i := range wts {
		if wts[i].IsMain {
			mainEntry = &wts[i]
		} else {
			claudeEntry = &wts[i]
		}
	}
	if mainEntry == nil || claudeEntry == nil {
		t.Fatalf("missing main or claude worktree entry: %+v", wts)
	}

	if mainEntry.Source != "main" {
		t.Errorf("mainEntry.Source = %q, want \"main\"", mainEntry.Source)
	}
	if mainEntry.IsCurrent {
		t.Errorf("mainEntry.IsCurrent = true when querying from claudeWT")
	}

	if !claudeEntry.IsCurrent {
		t.Errorf("claudeEntry.IsCurrent = false, want true")
	}
	if claudeEntry.Source != "claude" {
		t.Errorf("claudeEntry.Source = %q, want \"claude\"", claudeEntry.Source)
	}
	if claudeEntry.Branch != "claude/feature-x" {
		t.Errorf("claudeEntry.Branch = %q, want \"claude/feature-x\"", claudeEntry.Branch)
	}
	if claudeEntry.DirtyCount != 1 {
		t.Errorf("claudeEntry.DirtyCount = %d, want 1", claudeEntry.DirtyCount)
	}
	if claudeEntry.BaseBranch != "main" {
		t.Errorf("claudeEntry.BaseBranch = %q, want \"main\"", claudeEntry.BaseBranch)
	}
	if claudeEntry.AheadOfBase != 1 {
		t.Errorf("claudeEntry.AheadOfBase = %d, want 1", claudeEntry.AheadOfBase)
	}
	if claudeEntry.ChangedFilesVsBase != 2 {
		t.Errorf("claudeEntry.ChangedFilesVsBase = %d, want 2 (1 committed + 1 untracked)", claudeEntry.ChangedFilesVsBase)
	}
}

func TestHandleGitWorktreeCRUD(t *testing.T) {
	mainRoot, _ := initTestWorktreeRepo(t)
	wtParent := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(wtParent); err == nil {
		wtParent = resolved
	}

	ix := NewIndex(mainRoot)
	ix.Build()
	srv := &Server{
		ix:       ix,
		diffBase: "HEAD",
		diffMode: "head",
	}

	// 1. Create a new worktree via POST /api/git/worktrees/create
	createReq := newLocalPostRequest(t, "/api/git/worktrees/create", map[string]any{
		"branch":      "feat/new-wt",
		"baseRef":     "main",
		"worktreeDir": wtParent,
		"switchTo":    false,
	})
	createRec := httptest.NewRecorder()
	srv.handleGitWorktreeCreate(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("handleGitWorktreeCreate status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	var createResp struct {
		OK       bool         `json:"ok"`
		Path     string       `json:"path"`
		Worktree *GitWorktree `json:"worktree"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	if !createResp.OK || createResp.Path == "" || createResp.Worktree == nil {
		t.Fatalf("unexpected create response: %+v", createResp)
	}
	if createResp.Worktree.Branch != "feat/new-wt" {
		t.Errorf("created worktree branch = %q, want \"feat/new-wt\"", createResp.Worktree.Branch)
	}

	// 2. List worktrees via GET /api/git/worktrees
	listReq := httptest.NewRequest(http.MethodGet, "/api/git/worktrees", nil)
	listRec := httptest.NewRecorder()
	srv.handleGitWorktrees(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("handleGitWorktrees status = %d", listRec.Code)
	}
	var listResp struct {
		Worktrees  []GitWorktree `json:"worktrees"`
		BaseBranch string        `json:"baseBranch"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(listResp.Worktrees))
	}

	// 3. Refuse removing the main / active worktree
	badRmReq := newLocalPostRequest(t, "/api/git/worktrees/remove", map[string]any{
		"path": mainRoot,
	})
	badRmRec := httptest.NewRecorder()
	srv.handleGitWorktreeRemove(badRmRec, badRmReq)
	if badRmRec.Code == http.StatusOK {
		t.Fatalf("expected removing main worktree to fail, got 200 OK")
	}

	// 4. Remove the linked worktree via POST /api/git/worktrees/remove
	rmReq := newLocalPostRequest(t, "/api/git/worktrees/remove", map[string]any{
		"path":  createResp.Path,
		"force": false,
	})
	rmRec := httptest.NewRecorder()
	srv.handleGitWorktreeRemove(rmRec, rmReq)
	if rmRec.Code != http.StatusOK {
		t.Fatalf("handleGitWorktreeRemove status = %d, body = %s", rmRec.Code, rmRec.Body.String())
	}

	// 5. Prune via POST /api/git/worktrees/prune
	pruneReq := newLocalPostRequest(t, "/api/git/worktrees/prune", map[string]any{})
	pruneRec := httptest.NewRecorder()
	srv.handleGitWorktreePrune(pruneRec, pruneReq)
	if pruneRec.Code != http.StatusOK {
		t.Fatalf("handleGitWorktreePrune status = %d, body = %s", pruneRec.Code, pruneRec.Body.String())
	}
}

func TestHandleGitDiffBaseAndClaudeReview(t *testing.T) {
	mainRoot, runIn := initTestWorktreeRepo(t)
	wtParent := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(wtParent); err == nil {
		wtParent = resolved
	}

	wtPath, err := gitCreateWorktree(mainRoot, "claude/committed-branch", "main", wtParent)
	if err != nil {
		t.Fatalf("gitCreateWorktree failed: %v", err)
	}

	// Commit a new file and modify README.md in the linked worktree; leave working tree clean.
	if err := os.WriteFile(filepath.Join(wtPath, "agent_output.go"), []byte("package main\nfunc Agent() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtPath, "README.md"), []byte("# Base Repo\nUpdated by Claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(wtPath, "add", "-A")
	runIn(wtPath, "commit", "-m", "claude finished task")

	ix := NewIndex(wtPath)
	ix.Build()
	srv := &Server{
		ix:       ix,
		diffBase: "HEAD",
		diffMode: "head",
	}

	// In HEAD mode, working tree is clean so GitChanges is 0.
	if count, _ := srv.ix.GitChanges(); count != 0 {
		t.Fatalf("expected 0 uncommitted changes in HEAD mode, got %d", count)
	}

	// Switch to "base" mode via POST /api/git/diff-base
	baseReq := newLocalPostRequest(t, "/api/git/diff-base", map[string]any{
		"mode": "base",
		"ref":  "main",
	})
	baseRec := httptest.NewRecorder()
	srv.handleGitDiffBase(baseRec, baseReq)
	if baseRec.Code != http.StatusOK {
		t.Fatalf("handleGitDiffBase(base) status = %d, body = %s", baseRec.Code, baseRec.Body.String())
	}

	if srv.diffMode != "base" {
		t.Errorf("srv.diffMode = %q, want \"base\"", srv.diffMode)
	}
	if srv.diffBaseRef != "main" {
		t.Errorf("srv.diffBaseRef = %q, want \"main\"", srv.diffBaseRef)
	}

	// Now Index.GitChanges() must include both committed branch files!
	count, files := srv.ix.GitChanges()
	if count != 2 {
		t.Fatalf("expected 2 changed files vs main in base mode, got %d (%v)", count, files)
	}

	// Switch back to "head" mode
	headReq := newLocalPostRequest(t, "/api/git/diff-base", map[string]any{
		"mode": "head",
	})
	headRec := httptest.NewRecorder()
	srv.handleGitDiffBase(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("handleGitDiffBase(head) status = %d, body = %s", headRec.Code, headRec.Body.String())
	}
	if count, _ := srv.ix.GitChanges(); count != 0 {
		t.Fatalf("expected 0 changes after switching back to HEAD mode, got %d", count)
	}

	// Test autoConfigureWorktreeDiffBase automatically enables "base" mode for a linked worktree ahead of base
	srv.autoConfigureWorktreeDiffBase()
	if srv.diffMode != "base" {
		t.Errorf("autoConfigureWorktreeDiffBase: srv.diffMode = %q, want \"base\"", srv.diffMode)
	}
	if count, _ := srv.ix.GitChanges(); count != 2 {
		t.Errorf("autoConfigureWorktreeDiffBase: expected 2 git changes vs main, got %d", count)
	}
}
