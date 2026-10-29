package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"px0/internal/render"
	"px0/internal/settings"
)

// WorkspaceRepo describes a discovered git repository or workspace directory.
type WorkspaceRepo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	RootGroup  string `json:"rootGroup,omitempty"`  // e.g. configured root or "Recent" / "Pinned" / "Current"
	Branch     string `json:"branch,omitempty"`     // fast HEAD read if .git exists
	IsCurrent  bool   `json:"isCurrent,omitempty"`
	IsWorktree bool   `json:"isWorktree,omitempty"`
	Pinned     bool   `json:"pinned,omitempty"`
	LastOpened int64  `json:"lastOpened,omitempty"` // unix seconds
}

// workspaceScanSkip lists directory names skipped when walking workspace roots.
var workspaceScanSkip = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
	"venv":         true,
	"__pycache__":  true,
	"Library":      true,
	"Applications": true,
	"AppData":      true,
}

// defaultCandidateSubdirs are checked under the user's home directory when no
// workspace.roots are explicitly configured.
var defaultCandidateSubdirs = []string{
	"code",
	"src",
	"projects",
	"work",
	"repos",
	"dev",
	"workspace",
	"github",
}

// expandWorkspaceHome expands a leading "~" or "~/..." to the user's home directory.
func expandWorkspaceHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Clean(home)
		}
		return p
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// normalizeWorkspacePath expands "~", resolves absolute path and symlinks when possible.
func normalizeWorkspacePath(p string) string {
	expanded := expandWorkspaceHome(p)
	if expanded == "" {
		return ""
	}
	if abs, err := filepath.Abs(expanded); err == nil {
		expanded = abs
	}
	if resolved, err := filepath.EvalSymlinks(expanded); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(expanded)
}

// workspacePathKey returns the key used to de-duplicate workspace paths. The
// default macOS and Windows file systems ignore case, so ~/Workspace and
// ~/workspace are one folder there and must not list every repo twice.
func workspacePathKey(p string) string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// probeRepoFast inspects dir/.git without spawning a git subprocess.
// It supports both standard .git directories and linked worktree .git files.
func probeRepoFast(dir string) (isGit bool, isWorktree bool, branch string) {
	gitPath := filepath.Join(dir, ".git")
	st, err := os.Stat(gitPath)
	if err != nil {
		return false, false, ""
	}
	if st.IsDir() {
		headBytes, err := os.ReadFile(filepath.Join(gitPath, "HEAD"))
		if err != nil {
			return true, false, ""
		}
		return true, false, parseGitHeadBytes(headBytes)
	}
	if st.Mode().IsRegular() {
		data, err := os.ReadFile(gitPath)
		if err != nil {
			return true, true, ""
		}
		gitdir := parseGitdirFile(dir, data)
		if gitdir == "" {
			return true, true, ""
		}
		headBytes, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
		if err != nil {
			return true, true, ""
		}
		return true, true, parseGitHeadBytes(headBytes)
	}
	return false, false, ""
}

func parseGitdirFile(baseDir string, data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gitdir:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			if raw == "" {
				return ""
			}
			if !filepath.IsAbs(raw) {
				raw = filepath.Join(baseDir, raw)
			}
			return filepath.Clean(raw)
		}
	}
	return ""
}

func parseGitHeadBytes(data []byte) string {
	s := strings.TrimSpace(string(data))
	if s == "" {
		return ""
	}
	if line, _, ok := strings.Cut(s, "\n"); ok {
		s = strings.TrimSpace(line)
	}
	if strings.HasPrefix(s, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(s, "ref:"))
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			return strings.TrimPrefix(ref, "refs/heads/")
		case strings.HasPrefix(ref, "refs/remotes/"):
			return strings.TrimPrefix(ref, "refs/remotes/")
		case strings.HasPrefix(ref, "refs/tags/"):
			return strings.TrimPrefix(ref, "refs/tags/")
		case strings.HasPrefix(ref, "refs/"):
			return strings.TrimPrefix(ref, "refs/")
		default:
			return ref
		}
	}
	// Detached HEAD SHA: return short 7-char prefix.
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// scanWorktreeContainers checks standard worktree subdirectories inside a repo
// (such as .claude/worktrees and .worktrees) for linked worktrees.
func scanWorktreeContainers(repoDir, group string, emit func(WorkspaceRepo)) {
	containers := []string{
		filepath.Join(repoDir, ".claude", "worktrees"),
		filepath.Join(repoDir, ".worktrees"),
	}
	for _, container := range containers {
		entries, err := os.ReadDir(container)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			wtDir := filepath.Join(container, entry.Name())
			if isGit, _, branch := probeRepoFast(wtDir); isGit {
				emit(WorkspaceRepo{
					Name:       filepath.Base(wtDir),
					Path:       normalizeWorkspacePath(wtDir),
					RootGroup:  group,
					Branch:     branch,
					IsWorktree: true,
				})
			}
		}
	}
}

// scanWorkspaceRoot walks rootDir up to maxDepth levels looking for git repositories.
func scanWorkspaceRoot(rootDir, group string, maxDepth int, emit func(WorkspaceRepo)) {
	rootDir = normalizeWorkspacePath(rootDir)
	if rootDir == "" {
		return
	}
	st, err := os.Stat(rootDir)
	if err != nil || !st.IsDir() {
		return
	}

	// Check if rootDir itself is a git repository.
	if isGit, isWt, branch := probeRepoFast(rootDir); isGit {
		emit(WorkspaceRepo{
			Name:       filepath.Base(rootDir),
			Path:       rootDir,
			RootGroup:  group,
			Branch:     branch,
			IsWorktree: isWt,
		})
		scanWorktreeContainers(rootDir, group, emit)
	}

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") || workspaceScanSkip[name] {
				continue
			}
			sub := filepath.Join(dir, name)
			if isGit, isWt, branch := probeRepoFast(sub); isGit {
				normSub := normalizeWorkspacePath(sub)
				emit(WorkspaceRepo{
					Name:       filepath.Base(normSub),
					Path:       normSub,
					RootGroup:  group,
					Branch:     branch,
					IsWorktree: isWt,
				})
				scanWorktreeContainers(normSub, group, emit)
				// Do not descend into normal subdirectories of a git repo.
				continue
			}
			if depth < maxDepth {
				walk(sub, depth+1)
			}
		}
	}

	walk(rootDir, 1)
}

// listWorkspaces discovers available repositories across configured roots,
// default candidate directories, and recent/pinned workbench history.
func (s *Server) listWorkspaces() map[string]any {
	s.wsMu.Lock()
	currentRoot := ""
	if s.ix != nil {
		currentRoot = normalizeWorkspacePath(s.ix.Root())
	}
	s.wsMu.Unlock()

	rawConfigured := settings.WorkspaceRootPaths()
	configuredRoots := make([]string, 0, len(rawConfigured))
	seenRoots := make(map[string]bool, len(rawConfigured))
	for _, r := range rawConfigured {
		norm := normalizeWorkspacePath(r)
		if norm == "" || seenRoots[workspacePathKey(norm)] {
			continue
		}
		seenRoots[workspacePathKey(norm)] = true
		configuredRoots = append(configuredRoots, norm)
	}

	wb := (&wbStore{path: workbenchStorePath()}).Load()
	wbByPath := make(map[string]wbItem, len(wb.Items))
	for _, it := range wb.Items {
		if it.Kind != "dir" || it.Path == "" {
			continue
		}
		norm := normalizeWorkspacePath(it.Path)
		if norm != "" {
			it.Path = norm
			wbByPath[workspacePathKey(norm)] = it
		}
	}

	byPath := make(map[string]*WorkspaceRepo)
	upsert := func(cand WorkspaceRepo) {
		if cand.Path == "" {
			return
		}
		norm := normalizeWorkspacePath(cand.Path)
		if norm == "" {
			return
		}
		cand.Path = norm
		if cand.Name == "" {
			cand.Name = filepath.Base(norm)
		}
		key := workspacePathKey(norm)
		if existing, ok := byPath[key]; ok {
			if existing.RootGroup == "" || existing.RootGroup == "Recent" || existing.RootGroup == "Pinned" {
				if cand.RootGroup != "" {
					existing.RootGroup = cand.RootGroup
				}
			}
			if existing.Branch == "" && cand.Branch != "" {
				existing.Branch = cand.Branch
			}
			if cand.IsWorktree {
				existing.IsWorktree = true
			}
			if cand.IsCurrent {
				existing.IsCurrent = true
			}
			if cand.Pinned {
				existing.Pinned = true
			}
			if cand.LastOpened > existing.LastOpened {
				existing.LastOpened = cand.LastOpened
			}
			return
		}
		copyCand := cand
		byPath[key] = &copyCand
	}

	// 1. Scan configured roots (or default candidate roots if none configured).
	if len(configuredRoots) > 0 {
		for _, root := range configuredRoots {
			group := tildePath(root)
			scanWorkspaceRoot(root, group, 3, upsert)
		}
	} else {
		var candidates []string
		seenCand := make(map[string]bool)
		addCand := func(dir string) {
			norm := normalizeWorkspacePath(dir)
			if norm == "" || norm == "/" || seenCand[workspacePathKey(norm)] {
				return
			}
			if home, err := os.UserHomeDir(); err == nil && workspacePathKey(norm) == workspacePathKey(filepath.Clean(home)) {
				return
			}
			seenCand[workspacePathKey(norm)] = true
			candidates = append(candidates, norm)
		}
		if currentRoot != "" {
			addCand(filepath.Dir(currentRoot))
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			for _, sub := range defaultCandidateSubdirs {
				addCand(filepath.Join(home, sub))
			}
		}
		for _, cand := range candidates {
			scanWorkspaceRoot(cand, tildePath(cand), 2, upsert)
		}
	}

	// 2. Merge pinned and recent directories from workbench history.
	for _, it := range wbByPath {
		normPath := it.Path
		st, err := os.Stat(normPath)
		if err != nil || !st.IsDir() {
			continue
		}
		_, isWt, branch := probeRepoFast(normPath)
		group := "Recent"
		if it.Pinned {
			group = "Pinned"
		}
		name := it.Title
		if name == "" {
			name = filepath.Base(normPath)
		}
		upsert(WorkspaceRepo{
			Name:       name,
			Path:       normPath,
			RootGroup:  group,
			Branch:     branch,
			IsWorktree: isWt,
			Pinned:     it.Pinned,
			LastOpened: it.LastOpened,
		})
	}

	// 3. Always include the current workspace root and any linked worktrees inside it.
	if currentRoot != "" {
		_, isWt, branch := probeRepoFast(currentRoot)
		upsert(WorkspaceRepo{
			Name:       filepath.Base(currentRoot),
			Path:       currentRoot,
			RootGroup:  "Current",
			Branch:     branch,
			IsCurrent:  true,
			IsWorktree: isWt,
		})
		scanWorktreeContainers(currentRoot, tildePath(currentRoot), upsert)
	}

	// 4. Enrich all discovered repos with workbench Pinned / LastOpened metadata.
	repos := make([]WorkspaceRepo, 0, len(byPath))
	currentKey := workspacePathKey(currentRoot)
	for key, repo := range byPath {
		if currentRoot != "" && key == currentKey {
			repo.IsCurrent = true
		}
		if it, ok := wbByPath[key]; ok {
			if it.Pinned {
				repo.Pinned = true
			}
			if it.LastOpened > repo.LastOpened {
				repo.LastOpened = it.LastOpened
			}
		}
		repos = append(repos, *repo)
	}

	sort.Slice(repos, func(i, j int) bool {
		a, b := repos[i], repos[j]
		if a.IsCurrent != b.IsCurrent {
			return a.IsCurrent
		}
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if a.LastOpened != b.LastOpened {
			return a.LastOpened > b.LastOpened
		}
		an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if an != bn {
			return an < bn
		}
		return a.Path < b.Path
	})

	currentName := ""
	if currentRoot != "" {
		currentName = filepath.Base(currentRoot)
	}

	return map[string]any{
		"current":         currentRoot,
		"name":            currentName,
		"configuredRoots": configuredRoots,
		"repos":           repos,
	}
}

// SwitchWorkspace hot-swaps the active workspace root in-place without restarting the server.
func (s *Server) SwitchWorkspace(targetPath string) (map[string]any, error) {
	cleaned := expandWorkspaceHome(targetPath)
	if cleaned == "" {
		return nil, fmt.Errorf("missing workspace path")
	}

	if !filepath.IsAbs(cleaned) {
		s.wsMu.Lock()
		base := ""
		if s.ix != nil {
			base = s.ix.Root()
		}
		s.wsMu.Unlock()
		if base != "" {
			cleaned = filepath.Join(base, cleaned)
		} else if abs, err := filepath.Abs(cleaned); err == nil {
			cleaned = abs
		}
	}

	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace path %s: %w", targetPath, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	abs = filepath.Clean(abs)

	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace path does not exist: %s", abs)
	}
	if !st.IsDir() {
		if st.Mode().IsRegular() {
			abs = filepath.Dir(abs)
		} else {
			return nil, fmt.Errorf("workspace path is not a directory: %s", abs)
		}
	}

	s.wsMu.Lock()
	currentRoot := ""
	if s.ix != nil {
		currentRoot = filepath.Clean(s.ix.Root())
	}

	if workspacePathKey(abs) != workspacePathKey(currentRoot) {
		lspEnabled := true
		if s.lsp != nil {
			lspEnabled = s.lsp.Enabled()
		}

		hadAgent := s.agent != nil
		pinnedAgentCmd := ""
		if s.agent != nil && s.agent.Pinned() {
			pinnedAgentCmd = s.agent.Name()
		}

		if s.reviewFlow != nil {
			s.reviewFlow.Stop()
		}
		s.CloseThreads()
		if s.agent != nil {
			s.agent.Close()
		}
		if s.lsp != nil {
			s.lsp.Close()
		}
		if s.pr != nil {
			s.pr.Close()
			s.pr = nil
		}

		render.EvictAll()

		newIx := NewIndex(abs)
		newLSP := newLSPManager(abs, lspEnabled)

		s.ix = newIx
		s.lsp = newLSP
		s.diffBase = "HEAD"
		s.diffMode = "head"
		s.diffBaseRef = ""
		s.prHeadSHA = ""
		s.session = newSessionManager(s.basePath, abs)
		s.reviewFlow = newReviewWorkflowManager(s)

		if hadAgent {
			if newAgent, err := newAgentManager(abs, pinnedAgentCmd, newLSP); err == nil {
				s.SetAgent(newAgent)
			} else {
				s.agent = nil
				s.threads = nil
			}
		}

		newIx.Build()
		s.autoConfigureWorktreeDiffBaseLocked()

		if s.gitWatcher != nil {
			s.gitWatcher.Reset(newIx)
		}

		go recordRecent(wbDirItem(abs))
	}
	s.wsMu.Unlock()

	if s.gitWatcher != nil {
		s.gitWatcher.BroadcastEvent("workspace-switched", map[string]any{
			"root": abs,
			"name": filepath.Base(abs),
		})
	}

	return s.buildMetaMap(), nil
}

// handleWorkspaces serves GET /api/workspaces.
func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.listWorkspaces())
}

// handleWorkspaceSwitch serves POST /api/workspaces/switch.
func (s *Server) handleWorkspaceSwitch(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		fail(w, http.StatusBadRequest, "missing path")
		return
	}
	meta, err := s.SwitchWorkspace(req.Path)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	root := ""
	if s.ix != nil {
		root = s.ix.Root()
	}
	writeJSON(w, map[string]any{
		"ok":   true,
		"root": root,
		"name": filepath.Base(root),
		"meta": meta,
	})
}

// handleWorkspaceRoots serves POST /api/workspaces/roots.
func (s *Server) handleWorkspaceRoots(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req struct {
		Roots  []string `json:"roots"`
		Add    string   `json:"add"`
		Remove string   `json:"remove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid json")
		return
	}

	var baseRoots []string
	if req.Roots != nil {
		baseRoots = req.Roots
	} else {
		baseRoots = settings.WorkspaceRootPaths()
	}

	normalized := make([]string, 0, len(baseRoots)+1)
	seen := make(map[string]bool, len(baseRoots)+1)
	addRoot := func(raw string) {
		norm := normalizeWorkspacePath(raw)
		if norm == "" || seen[workspacePathKey(norm)] {
			return
		}
		seen[workspacePathKey(norm)] = true
		normalized = append(normalized, norm)
	}

	for _, rPath := range baseRoots {
		addRoot(rPath)
	}

	if addRaw := strings.TrimSpace(req.Add); addRaw != "" {
		normAdd := normalizeWorkspacePath(addRaw)
		if normAdd == "" {
			fail(w, http.StatusBadRequest, "invalid root path")
			return
		}
		st, err := os.Stat(normAdd)
		if err != nil || !st.IsDir() {
			fail(w, http.StatusBadRequest, fmt.Sprintf("root directory does not exist: %s", addRaw))
			return
		}
		addRoot(normAdd)
	}

	if remRaw := strings.TrimSpace(req.Remove); remRaw != "" {
		normRem := normalizeWorkspacePath(remRaw)
		filtered := make([]string, 0, len(normalized))
		for _, rPath := range normalized {
			if workspacePathKey(rPath) == workspacePathKey(normRem) || rPath == remRaw || tildePath(rPath) == remRaw {
				continue
			}
			filtered = append(filtered, rPath)
		}
		normalized = filtered
	}

	// Store a newline-separated string: Settings.WorkspaceRoots and the
	// settings schema both type this key as a string, not an array.
	var stored any = strings.Join(normalized, "\n")
	if len(normalized) == 0 {
		stored = nil
	}
	if err := settings.UpdateMap(map[string]any{
		"workspace.roots": stored,
	}); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, s.listWorkspaces())
}
