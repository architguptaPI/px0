package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"px0/internal/settings"
)

// GitWorktree describes a Git worktree belonging to the current repository,
// including main, git-linked, px0-managed, and Claude Desktop worktrees.
type GitWorktree struct {
	Path               string `json:"path"`
	Name               string `json:"name"`
	Branch             string `json:"branch"`
	Head               string `json:"head"`
	Subject            string `json:"subject,omitempty"`
	Age                string `json:"age,omitempty"`
	IsMain             bool   `json:"isMain"`
	IsCurrent          bool   `json:"isCurrent"`
	IsDetached         bool   `json:"isDetached"`
	Source             string `json:"source"` // "main", "claude", "px0", "git"
	DirtyCount         int    `json:"dirtyCount"`
	BaseBranch         string `json:"baseBranch,omitempty"`
	BaseMergeSHA       string `json:"baseMergeSha,omitempty"`
	AheadOfBase        int    `json:"aheadOfBase"`
	ChangedFilesVsBase int    `json:"changedFilesVsBase"`
	Locked             bool   `json:"locked,omitempty"`
	Prunable           bool   `json:"prunable,omitempty"`
}

// runGitNoPrompt runs a git command in root with GIT_TERMINAL_PROMPT=0 so it never blocks.
func runGitNoPrompt(root string, args ...string) ([]byte, error) {
	fullArgs := make([]string, 0, len(args)+2)
	if root != "" {
		fullArgs = append(fullArgs, "-C", root)
	}
	fullArgs = append(fullArgs, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd.Output()
}

// runGitCombinedNoPrompt runs a git command and returns combined stdout+stderr for error reporting.
func runGitCombinedNoPrompt(root string, args ...string) ([]byte, error) {
	fullArgs := make([]string, 0, len(args)+2)
	if root != "" {
		fullArgs = append(fullArgs, "-C", root)
	}
	fullArgs = append(fullArgs, args...)
	cmd := exec.Command("git", fullArgs...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	return cmd.CombinedOutput()
}

// evalWorktreePath normalizes a filesystem path, resolving symlinks when possible
// (such as /var -> /private/var on macOS) for reliable path equality checks.
func evalWorktreePath(p string) string {
	if p == "" {
		return ""
	}
	clean := filepath.Clean(p)
	if abs, err := filepath.Abs(clean); err == nil {
		clean = abs
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return clean
}

// expandWorktreeHome expands a leading ~/ in a configured directory path.
func expandWorktreeHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, p[2:])
		}
	}
	return filepath.Clean(p)
}

// gitRefCommitExists returns true if ref resolves to a valid commit in root.
func gitRefCommitExists(root, ref string) bool {
	if root == "" || ref == "" {
		return false
	}
	out, err := runGitNoPrompt(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return false
	}
	return validSHA(strings.TrimSpace(string(out)))
}

// gitResolveCommit resolves a ref or revision expression to a commit SHA in root.
func gitResolveCommit(root, ref string) string {
	if root == "" || ref == "" {
		return ""
	}
	out, err := runGitNoPrompt(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(string(out))
	if !validSHA(sha) {
		return ""
	}
	return sha
}

// detectBaseBranch determines the repository's primary base branch ("main", "master", etc.).
// It first checks refs/remotes/origin/HEAD and then falls back to common base branch names.
func detectBaseBranch(root string) string {
	if !gitAvailable(root) {
		return ""
	}
	if out, err := runGitNoPrompt(root, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		sym := strings.TrimSpace(string(out))
		short := strings.TrimPrefix(sym, "refs/remotes/origin/")
		if short != "" && short != sym {
			if gitRefCommitExists(root, short) {
				return short
			}
			if gitRefCommitExists(root, "origin/"+short) {
				return "origin/" + short
			}
		}
	}
	candidates := []string{"main", "master", "origin/main", "origin/master", "develop"}
	for _, c := range candidates {
		if gitRefCommitExists(root, c) {
			return c
		}
	}
	return ""
}

// classifyWorktreeSource determines whether a worktree is "main", "claude", "px0", or "git".
func classifyWorktreeSource(wtPath, branch string, isMain bool, px0Dirs []string) string {
	if isMain {
		return "main"
	}
	lowPath := strings.ToLower(filepath.ToSlash(wtPath))
	lowBranch := strings.ToLower(branch)

	// Match Claude's own worktree folders and branch names only. A plain
	// substring test on "claude" would tag any repo whose path holds the word.
	if strings.Contains(lowPath, "/.claude/worktrees/") ||
		strings.HasPrefix(lowBranch, "claude/") ||
		strings.Contains(lowBranch, "worktree-") ||
		strings.Contains(lowPath, ".gemini/antigravity/worktrees") {
		return "claude"
	}

	normWt := evalWorktreePath(wtPath)
	for _, dir := range px0Dirs {
		if dir == "" {
			continue
		}
		normDir := evalWorktreePath(dir)
		if normDir != "" && (normWt == normDir || strings.HasPrefix(normWt, normDir+string(filepath.Separator))) {
			return "px0"
		}
	}
	if strings.Contains(lowPath, "/.px0/worktrees/") {
		return "px0"
	}

	return "git"
}

// populateWorktreeDetails fills commit subject/age, dirty file count, and base-branch divergence metrics.
func populateWorktreeDetails(wt *GitWorktree, baseBranch string) {
	info, err := os.Stat(wt.Path)
	if err != nil || !info.IsDir() {
		return
	}

	// Commit subject and relative age
	if out, err := runGitNoPrompt(wt.Path, "log", "-1", "--format=%s%x00%cr"); err == nil {
		parts := bytes.SplitN(bytes.TrimSpace(out), []byte{0}, 2)
		if len(parts) >= 1 {
			wt.Subject = string(parts[0])
		}
		if len(parts) == 2 {
			wt.Age = string(parts[1])
		}
	}

	// Uncommitted dirty file count
	if out, err := runGitNoPrompt(wt.Path, "status", "--porcelain", "-uall"); err == nil {
		count := 0
		for _, line := range bytes.Split(out, []byte{'\n'}) {
			if len(bytes.TrimSpace(line)) > 0 {
				count++
			}
		}
		wt.DirtyCount = count
	}

	// Base-branch comparison when on a non-base branch (or detached HEAD in a linked worktree)
	if baseBranch != "" && wt.Branch != baseBranch && "origin/"+wt.Branch != baseBranch && wt.Branch != "origin/"+baseBranch {
		mergeBase := gitMergeBase(wt.Path, baseBranch, "HEAD")
		if mergeBase != "" {
			wt.BaseBranch = baseBranch
			wt.BaseMergeSHA = mergeBase

			if out, err := runGitNoPrompt(wt.Path, "rev-list", "--count", mergeBase+"..HEAD"); err == nil {
				if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && n >= 0 {
					wt.AheadOfBase = n
				}
			}

			changedSet := make(map[string]struct{})
			if out, err := runGitNoPrompt(wt.Path, "diff", "--name-only", "-z", mergeBase); err == nil {
				for _, entry := range bytes.Split(out, []byte{0}) {
					if len(entry) > 0 {
						changedSet[string(entry)] = struct{}{}
					}
				}
			}
			for _, u := range gitUntracked(wt.Path) {
				if u != "" {
					changedSet[u] = struct{}{}
				}
			}
			wt.ChangedFilesVsBase = len(changedSet)
		}
	}
}

// gitOriginRemoteKey returns a normalized host+path key for origin remote URL comparison.
func gitOriginRemoteKey(root string) string {
	out, err := runGitNoPrompt(root, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return ""
	}
	if host, path, ok := remoteHostPath(raw); ok {
		return strings.ToLower(host + "/" + path)
	}
	return strings.ToLower(raw)
}

// discoverStandaloneClaudeWorktrees checks <mainRoot>/.claude/worktrees and ~/.claude/worktrees
// for standalone git checkouts associated with this repository that are not already in seenPaths.
func discoverStandaloneClaudeWorktrees(mainRoot, normCurrentRoot string, seenPaths map[string]bool) []GitWorktree {
	if mainRoot == "" {
		return nil
	}
	var dirsToScan []string
	localClaude := filepath.Join(mainRoot, ".claude", "worktrees")
	if info, err := os.Stat(localClaude); err == nil && info.IsDir() {
		dirsToScan = append(dirsToScan, localClaude)
	}
	repoName := filepath.Base(mainRoot)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		globalClaudeRepo := filepath.Join(home, ".claude", "worktrees", repoName)
		if info, err := os.Stat(globalClaudeRepo); err == nil && info.IsDir() {
			dirsToScan = append(dirsToScan, globalClaudeRepo)
		}
		globalClaude := filepath.Join(home, ".claude", "worktrees")
		if info, err := os.Stat(globalClaude); err == nil && info.IsDir() {
			dirsToScan = append(dirsToScan, globalClaude)
		}
	}

	if len(dirsToScan) == 0 {
		return nil
	}

	mainOrigin := gitOriginRemoteKey(mainRoot)
	var extra []GitWorktree

	for _, baseDir := range dirsToScan {
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			cand := filepath.Join(baseDir, ent.Name())
			normCand := evalWorktreePath(cand)
			if seenPaths[normCand] {
				continue
			}
			probe := gitProbe(cand)
			if !probe.ok || evalWorktreePath(probe.toplevel) != normCand {
				continue
			}
			// Match either because it's directly inside <mainRoot>/.claude/worktrees,
			// inside ~/.claude/worktrees/<repoName>, or has the same origin remote URL.
			matched := baseDir == localClaude || filepath.Base(baseDir) == repoName
			if !matched && mainOrigin != "" {
				matched = gitOriginRemoteKey(cand) == mainOrigin
			}
			if !matched {
				continue
			}
			seenPaths[normCand] = true
			branch := gitCurrentBranch(cand)
			head := ""
			if out, err := runGitNoPrompt(cand, "rev-parse", "--short=7", "HEAD"); err == nil {
				head = strings.TrimSpace(string(out))
			}
			wt := GitWorktree{
				Path:       cand,
				Name:       filepath.Base(cand),
				Branch:     branch,
				Head:       head,
				IsMain:     false,
				IsCurrent:  normCand == normCurrentRoot,
				IsDetached: branch == "HEAD",
				Source:     "claude",
			}
			extra = append(extra, wt)
		}
	}
	return extra
}

// gitListWorktrees returns all Git worktrees associated with the repository at root,
// enriched with source classification, dirty counts, and base-branch divergence metrics.
func gitListWorktrees(root string) []GitWorktree {
	wts, baseBranch := gitListWorktreesBasic(root)
	// Each worktree needs about six git processes. Run them in parallel:
	// a repo with dozens of Claude sessions took seconds one after another.
	var wg sync.WaitGroup
	sem := make(chan struct{}, worktreeDetailWorkers)
	for i := range wts {
		wg.Add(1)
		sem <- struct{}{}
		go func(wt *GitWorktree) {
			defer wg.Done()
			defer func() { <-sem }()
			populateWorktreeDetails(wt, baseBranch)
		}(&wts[i])
	}
	wg.Wait()
	return wts
}

// worktreeDetailWorkers bounds the parallel detail queries in gitListWorktrees.
const worktreeDetailWorkers = 8

// gitListWorktreesBasic lists worktrees and the detected base branch without
// the per-worktree detail queries (log, status, merge-base, diff). Each detail
// query spawns several git processes per worktree, so callers that only need
// paths and flags use this form.
func gitListWorktreesBasic(root string) ([]GitWorktree, string) {
	probe := gitProbe(root)
	if !probe.ok {
		return []GitWorktree{}, ""
	}

	normCurrentRoot := evalWorktreePath(probe.toplevel)
	if normCurrentRoot == "" {
		normCurrentRoot = evalWorktreePath(root)
	}

	out, err := runGitNoPrompt(root, "worktree", "list", "--porcelain")
	if err != nil {
		return []GitWorktree{}, ""
	}

	var px0Dirs []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		px0Dirs = append(px0Dirs, filepath.Join(home, ".px0", "worktrees"))
	}
	if cfg := settings.Read(); cfg.GitWorktreeDir != nil && strings.TrimSpace(*cfg.GitWorktreeDir) != "" {
		px0Dirs = append(px0Dirs, expandWorktreeHome(*cfg.GitWorktreeDir))
	}

	seenPaths := make(map[string]bool)
	var worktrees []GitWorktree
	var mainRoot string

	blocks := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var wt GitWorktree
		isBare := false
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "worktree "):
				wt.Path = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			case strings.HasPrefix(line, "HEAD "):
				sha := strings.TrimSpace(strings.TrimPrefix(line, "HEAD "))
				if len(sha) > 7 {
					sha = sha[:7]
				}
				wt.Head = sha
			case strings.HasPrefix(line, "branch "):
				ref := strings.TrimSpace(strings.TrimPrefix(line, "branch "))
				wt.Branch = strings.TrimPrefix(ref, "refs/heads/")
			case line == "detached":
				wt.IsDetached = true
				if wt.Branch == "" {
					wt.Branch = "HEAD"
				}
			case line == "bare":
				isBare = true
			case line == "locked" || strings.HasPrefix(line, "locked "):
				wt.Locked = true
			case line == "prunable" || strings.HasPrefix(line, "prunable "):
				wt.Prunable = true
			}
		}
		if wt.Path == "" || isBare {
			continue
		}
		isMain := len(worktrees) == 0
		wt.IsMain = isMain
		if isMain {
			mainRoot = wt.Path
		}
		wt.Name = filepath.Base(wt.Path)
		if wt.Branch == "" && wt.IsDetached {
			wt.Branch = "HEAD"
		}
		normWt := evalWorktreePath(wt.Path)
		seenPaths[normWt] = true
		wt.IsCurrent = normWt == normCurrentRoot || evalWorktreePath(root) == normWt
		wt.Source = classifyWorktreeSource(wt.Path, wt.Branch, isMain, px0Dirs)
		worktrees = append(worktrees, wt)
	}

	if mainRoot == "" && len(worktrees) > 0 {
		mainRoot = worktrees[0].Path
	}

	// Append any standalone Claude worktrees not tracked in git worktree list
	extra := discoverStandaloneClaudeWorktrees(mainRoot, normCurrentRoot, seenPaths)
	worktrees = append(worktrees, extra...)

	baseBranch := detectBaseBranch(root)
	if baseBranch == "" && mainRoot != "" && mainRoot != root {
		baseBranch = detectBaseBranch(mainRoot)
	}
	if baseBranch == "" && len(worktrees) > 0 && worktrees[0].Branch != "" && worktrees[0].Branch != "HEAD" {
		baseBranch = worktrees[0].Branch
	}

	return worktrees, baseBranch
}

// defaultWorktreeParentDir returns the clean parent directory where new worktrees
// should be created for mainRoot, respecting settings and existing .claude/worktrees.
func defaultWorktreeParentDir(mainRoot string) string {
	if cfg := settings.Read(); cfg.GitWorktreeDir != nil && strings.TrimSpace(*cfg.GitWorktreeDir) != "" {
		return expandWorktreeHome(*cfg.GitWorktreeDir)
	}
	// Never default into <repo>/.claude/worktrees: Claude Desktop owns that
	// folder, and px0 worktrees there would show up as Claude sessions.
	repoName := "repo"
	if mainRoot != "" {
		repoName = filepath.Base(filepath.Clean(mainRoot))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".px0", "worktrees", repoName)
	}
	return filepath.Join(os.TempDir(), "px0-worktrees", repoName)
}

// validateWorktreeBranch checks that a branch name is safe to pass to git worktree add.
func validateWorktreeBranch(branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("branch name is required")
	}
	if strings.HasPrefix(branch, "-") {
		return errors.New("branch name cannot start with '-'")
	}
	if strings.Contains(branch, "..") || strings.ContainsAny(branch, " \t\r\n~^:?*[\\") {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	return nil
}

// sanitizeWorktreeSlug converts a branch name into a clean filesystem directory name.
func sanitizeWorktreeSlug(branch string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(branch) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-.")
	if slug == "" {
		slug = "worktree"
	}
	return slug
}

// gitMainWorktreeRoot returns the main worktree path for the repository containing root.
func gitMainWorktreeRoot(root string) string {
	wts, _ := gitListWorktreesBasic(root)
	for _, wt := range wts {
		if wt.IsMain && wt.Path != "" {
			return wt.Path
		}
	}
	if probe := gitProbe(root); probe.ok {
		return probe.toplevel
	}
	return root
}

// gitCreateWorktree creates a new git worktree for branch (starting at baseRef if new)
// inside targetDir (or defaultWorktreeParentDir when targetDir is empty).
func gitCreateWorktree(root, branch, baseRef, targetDir string) (string, error) {
	if !gitAvailable(root) {
		return "", errors.New("workspace is not a git repository")
	}
	branch = strings.TrimSpace(branch)
	if err := validateWorktreeBranch(branch); err != nil {
		return "", err
	}
	baseRef = strings.TrimSpace(baseRef)
	if strings.HasPrefix(baseRef, "-") {
		return "", errors.New("invalid base ref")
	}

	mainRoot := gitMainWorktreeRoot(root)
	parentDir := strings.TrimSpace(targetDir)
	if parentDir != "" {
		parentDir = expandWorktreeHome(parentDir)
	} else {
		parentDir = defaultWorktreeParentDir(mainRoot)
	}

	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create worktree parent directory: %w", err)
	}

	slug := sanitizeWorktreeSlug(branch)
	wtPath := filepath.Join(parentDir, slug)
	if _, err := os.Stat(wtPath); err == nil {
		return "", fmt.Errorf("worktree directory already exists: %s", wtPath)
	}

	// Check whether the branch already exists locally.
	_, localErr := runGitNoPrompt(root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	var out []byte
	var err error
	if localErr == nil {
		out, err = runGitCombinedNoPrompt(root, "worktree", "add", wtPath, branch)
	} else {
		startPoint := baseRef
		if startPoint == "" {
			// If remote tracking branch exists, use it; otherwise use detected base branch or HEAD.
			if _, remErr := runGitNoPrompt(root, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch); remErr == nil {
				startPoint = "origin/" + branch
			} else if detected := detectBaseBranch(root); detected != "" {
				startPoint = detected
			} else {
				startPoint = "HEAD"
			}
		}
		out, err = runGitCombinedNoPrompt(root, "worktree", "add", "-b", branch, wtPath, startPoint)
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git worktree add failed: %s", msg)
	}

	return filepath.Clean(wtPath), nil
}

// gitRemoveWorktree safely removes a linked worktree belonging to root.
// It refuses to remove the main worktree or the currently active workspace.
func gitRemoveWorktree(root, targetPath string, force bool) error {
	if !gitAvailable(root) {
		return errors.New("workspace is not a git repository")
	}
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return errors.New("worktree path is required")
	}
	normTarget := evalWorktreePath(targetPath)
	cleanTarget := filepath.Clean(targetPath)

	wts, _ := gitListWorktreesBasic(root)
	var matched *GitWorktree
	for i := range wts {
		wt := &wts[i]
		if evalWorktreePath(wt.Path) == normTarget || filepath.Clean(wt.Path) == cleanTarget {
			matched = wt
			break
		}
	}
	if matched == nil {
		return fmt.Errorf("worktree %q does not belong to this repository", targetPath)
	}
	if matched.IsMain {
		return errors.New("cannot remove the main repository worktree")
	}
	if matched.IsCurrent {
		return errors.New("cannot remove the currently active worktree; switch to another worktree first")
	}

	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, matched.Path)

	out, err := runGitCombinedNoPrompt(root, args...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git worktree remove failed: %s", msg)
	}
	return nil
}

// gitPruneWorktrees prunes stale worktree administrative files in root.
func gitPruneWorktrees(root string) error {
	if !gitAvailable(root) {
		return errors.New("workspace is not a git repository")
	}
	out, err := runGitCombinedNoPrompt(root, "worktree", "prune")
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git worktree prune failed: %s", msg)
	}
	return nil
}

// setDiffBaseMode updates the server's diff base mode ("head", "base", or "custom")
// and refreshes the index and git watcher so file diffs, gutter markers, and the
// Git sidebar reflect the chosen base.
func (s *Server) setDiffBaseMode(mode, ref string) (map[string]any, error) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	return s.setDiffBaseModeLocked(mode, ref)
}

// setDiffBaseModeLocked is setDiffBaseMode for callers that already hold wsMu.
func (s *Server) setDiffBaseModeLocked(mode, ref string) (map[string]any, error) {
	if s.ix == nil {
		return nil, errors.New("workspace index not initialized")
	}
	root := s.ix.Root()
	if !gitAvailable(root) {
		return nil, errors.New("workspace is not a git repository")
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "head"
	}
	ref = strings.TrimSpace(ref)

	switch mode {
	case "head":
		s.diffMode = "head"
		s.diffBase = "HEAD"
		s.diffBaseRef = ""
		s.ix.SetDiffBase("HEAD")

	case "base":
		baseBranch := ref
		if baseBranch == "" {
			baseBranch = detectBaseBranch(root)
		}
		if baseBranch == "" {
			return nil, errors.New("could not detect a base branch (main/master)")
		}
		mergeBase := gitMergeBase(root, baseBranch, "HEAD")
		if mergeBase == "" {
			mergeBase = gitResolveCommit(root, baseBranch)
		}
		if mergeBase == "" {
			return nil, fmt.Errorf("could not compute merge-base against %q", baseBranch)
		}
		s.diffMode = "base"
		s.diffBase = mergeBase
		s.diffBaseRef = baseBranch
		s.ix.SetDiffBase(mergeBase)

	case "custom":
		if ref == "" {
			return nil, errors.New("custom git ref is required")
		}
		if strings.HasPrefix(ref, "-") {
			return nil, errors.New("invalid git ref")
		}
		var resolved string
		if strings.Contains(ref, "...") {
			parts := strings.SplitN(ref, "...", 2)
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])
			if right == "" {
				right = "HEAD"
			}
			resolved = gitMergeBase(root, left, right)
		} else {
			resolved = gitResolveCommit(root, ref)
		}
		if resolved == "" {
			return nil, fmt.Errorf("could not resolve git ref %q", ref)
		}
		s.diffMode = "custom"
		s.diffBase = resolved
		s.diffBaseRef = ref
		s.ix.SetDiffBase(resolved)

	default:
		return nil, fmt.Errorf("invalid diff mode %q (expected \"head\", \"base\", or \"custom\")", mode)
	}

	s.ix.UpdateGitStatus()
	if s.gitWatcher != nil {
		s.gitWatcher.Refresh()
	}

	gitChanges, gitFiles := s.ix.GitChanges()
	return map[string]any{
		"ok":          true,
		"diffMode":    s.diffMode,
		"diffBase":    s.diffBase,
		"diffBaseRef": s.diffBaseRef,
		"gitChanges":  gitChanges,
		"gitFiles":    gitFiles,
	}, nil
}

// diffState returns the diff mode, base commit, and base ref under wsMu.
func (s *Server) diffState() (mode, base, ref string) {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	mode, base, ref = s.diffMode, s.diffBase, s.diffBaseRef
	if mode == "" {
		mode = "head"
	}
	if base == "" {
		base = "HEAD"
	}
	return mode, base, ref
}

// autoConfigureWorktreeDiffBase automatically enables base-branch diff mode when
// opening or switching into a linked worktree that has commits ahead of the base
// branch (unless disabled by the git.worktreeAutoBaseDiff setting).
func (s *Server) autoConfigureWorktreeDiffBase() {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	s.autoConfigureWorktreeDiffBaseLocked()
}

// autoConfigureWorktreeDiffBaseLocked is autoConfigureWorktreeDiffBase for
// callers that already hold wsMu.
func (s *Server) autoConfigureWorktreeDiffBaseLocked() {
	if s.ix == nil || s.pr != nil {
		return
	}
	root := s.ix.Root()
	if !gitAvailable(root) {
		return
	}
	cfg := settings.Read()
	autoBase := cfg.GitWorktreeAutoBaseDiff == nil || *cfg.GitWorktreeAutoBaseDiff

	// Only the current worktree needs detail queries.
	wts, baseBranch := gitListWorktreesBasic(root)
	var current *GitWorktree
	for i := range wts {
		if wts[i].IsCurrent {
			current = &wts[i]
			populateWorktreeDetails(current, baseBranch)
			break
		}
	}

	if current != nil && !current.IsMain && current.AheadOfBase > 0 && current.BaseMergeSHA != "" && autoBase {
		_, _ = s.setDiffBaseModeLocked("base", current.BaseBranch)
		return
	}
	if s.diffMode == "base" && (current == nil || current.IsMain || current.AheadOfBase == 0) {
		_, _ = s.setDiffBaseModeLocked("head", "")
	}
}

// ---------------------------------------------------------------- HTTP Handlers

// handleGitWorktrees handles GET /api/git/worktrees.
func (s *Server) handleGitWorktrees(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	root := ""
	if s.ix != nil {
		root = s.ix.Root()
	}
	cfg := settings.Read()
	autoBaseDiff := cfg.GitWorktreeAutoBaseDiff == nil || *cfg.GitWorktreeAutoBaseDiff

	mode, diffBase, diffBaseRef := s.diffState()

	if root == "" || !gitAvailable(root) {
		writeJSON(w, map[string]any{
			"worktrees":    []GitWorktree{},
			"baseBranch":   "",
			"currentMode":  mode,
			"diffBase":     diffBase,
			"diffBaseRef":  diffBaseRef,
			"worktreeDir":  "",
			"autoBaseDiff": autoBaseDiff,
		})
		return
	}

	wts := gitListWorktrees(root)
	baseBranch := detectBaseBranch(root)
	mainRoot := root
	for _, wt := range wts {
		if wt.IsMain && wt.Path != "" {
			mainRoot = wt.Path
			break
		}
	}
	if baseBranch == "" && mainRoot != root {
		baseBranch = detectBaseBranch(mainRoot)
	}

	writeJSON(w, map[string]any{
		"worktrees":    wts,
		"baseBranch":   baseBranch,
		"currentMode":  mode,
		"diffBase":     diffBase,
		"diffBaseRef":  diffBaseRef,
		"worktreeDir":  defaultWorktreeParentDir(mainRoot),
		"autoBaseDiff": autoBaseDiff,
	})
}

// handleGitWorktreeCreate handles POST /api/git/worktrees/create.
func (s *Server) handleGitWorktreeCreate(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		Branch      string `json:"branch"`
		BaseRef     string `json:"baseRef"`
		WorktreeDir string `json:"worktreeDir"`
		SwitchTo    bool   `json:"switchTo"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	root := s.ix.Root()
	wtPath, err := gitCreateWorktree(root, req.Branch, req.BaseRef, req.WorktreeDir)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	wts := gitListWorktrees(root)
	var created *GitWorktree
	normCreated := evalWorktreePath(wtPath)
	for i := range wts {
		if evalWorktreePath(wts[i].Path) == normCreated || filepath.Clean(wts[i].Path) == filepath.Clean(wtPath) {
			created = &wts[i]
			break
		}
	}

	writeJSON(w, map[string]any{
		"ok":        true,
		"path":      wtPath,
		"worktree":  created,
		"worktrees": wts,
		"switchTo":  req.SwitchTo,
	})
}

// handleGitWorktreeRemove handles POST /api/git/worktrees/remove.
func (s *Server) handleGitWorktreeRemove(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		Path  string `json:"path"`
		Force bool   `json:"force"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	root := s.ix.Root()
	if err := gitRemoveWorktree(root, req.Path, req.Force); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, map[string]any{
		"ok":        true,
		"worktrees": gitListWorktrees(root),
	})
}

// handleGitWorktreePrune handles POST /api/git/worktrees/prune.
func (s *Server) handleGitWorktreePrune(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	root := s.ix.Root()
	if err := gitPruneWorktrees(root); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, map[string]any{
		"ok":        true,
		"worktrees": gitListWorktrees(root),
	})
}

// handleGitDiffBase handles POST /api/git/diff-base.
func (s *Server) handleGitDiffBase(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		Mode string `json:"mode"`
		Ref  string `json:"ref"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	res, err := s.setDiffBaseMode(req.Mode, req.Ref)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, res)
}
