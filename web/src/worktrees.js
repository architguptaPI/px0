// web/src/worktrees.js
import { $, esc, S, api, apiPostJson } from './state.js';
import { showToast, copyToClipboard } from './ui.js';
import { refreshTree, expandDirtyDirs, inGitMode, updateSidebarToggleState, setSidebarMode } from './tree.js';
import { reloadOpenTabs, openFile } from './tabs.js';
import { triggerRefresh } from './gitstream.js';
import { syncGitPanelMeta } from './gitpanel.js';

let worktreeState = {
  worktrees: [],
  baseBranch: '',
  currentMode: 'head',
  diffBase: 'HEAD',
  diffBaseRef: '',
  worktreeDir: '',
  autoBaseDiff: true,
};

let switchWorkspaceCallback = null;
let diffBaseChangedCallback = null;
let sectionCollapsed = false;
let activeModalTab = 'list';
let modalFilterText = '';
let initialized = false;

const WORKTREE_ICON_SVG = '<svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="4" cy="3.5" r="1.75"/><circle cx="4" cy="12.5" r="1.75"/><circle cx="12" cy="7.5" r="1.75"/><path d="M4 5.25v5.5"/><path d="M4 5.5c0 2 2.5 2 6.25 2"/></svg>';

function injectWorktreeStyles() {
  if ($('#px0-worktrees-style')) return;
  const style = document.createElement('style');
  style.id = 'px0-worktrees-style';
  style.textContent = `
    .worktree-pill {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      height: 20px;
      padding: 0 7px;
      margin-left: 4px;
      border-radius: 999px;
      border: 1px solid var(--border, rgba(128,128,128,0.25));
      background: var(--bg-elevated, rgba(128,128,128,0.08));
      color: var(--fg-muted, inherit);
      font-size: 11px;
      font-weight: 500;
      line-height: 1;
      cursor: pointer;
      white-space: nowrap;
      max-width: 175px;
      overflow: hidden;
      text-overflow: ellipsis;
      transition: background 0.12s ease, border-color 0.12s ease, color 0.12s ease;
    }
    .worktree-pill:hover {
      background: var(--bg-hover, rgba(128,128,128,0.16));
      color: var(--fg, inherit);
      border-color: var(--accent, rgba(128,128,128,0.45));
    }
    .worktree-pill.is-claude {
      border-color: rgba(217, 119, 87, 0.45);
      background: rgba(217, 119, 87, 0.10);
      color: var(--fg, inherit);
    }
    .worktree-pill.is-base-diff {
      border-color: rgba(59, 130, 246, 0.45);
      background: rgba(59, 130, 246, 0.10);
    }
    .worktree-pill-branch {
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 96px;
    }
    .worktree-pill-badge {
      display: inline-flex;
      align-items: center;
      padding: 1px 4px;
      border-radius: 999px;
      font-size: 9.5px;
      font-weight: 600;
      background: rgba(128,128,128,0.18);
    }
    .worktree-pill.is-claude .worktree-pill-badge {
      background: rgba(217, 119, 87, 0.22);
      color: #d97757;
    }
    .git-diff-base-bar {
      display: flex;
      flex-direction: column;
      gap: 6px;
      padding: 7px 10px;
      border-bottom: 1px solid var(--border, rgba(128,128,128,0.18));
      background: var(--bg-subtle, rgba(128,128,128,0.04));
      font-size: 11px;
    }
    .git-diff-base-top {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 6px;
    }
    .git-diff-base-label {
      font-size: 10px;
      font-weight: 600;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--fg-muted, #888);
      display: inline-flex;
      align-items: center;
      gap: 5px;
    }
    .git-diff-base-seg {
      display: flex;
      gap: 4px;
      width: 100%;
    }
    .git-diff-base-btn {
      flex: 1 1 auto;
      display: inline-flex;
      align-items: center;
      justify-content: center;
      gap: 4px;
      padding: 4px 7px;
      border-radius: 5px;
      border: 1px solid var(--border, rgba(128,128,128,0.22));
      background: transparent;
      color: var(--fg-muted, inherit);
      font-size: 11px;
      font-weight: 500;
      cursor: pointer;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .git-diff-base-btn:hover {
      background: var(--bg-hover, rgba(128,128,128,0.12));
      color: var(--fg, inherit);
    }
    .git-diff-base-btn.active {
      background: var(--accent-subtle, rgba(59, 130, 246, 0.14));
      border-color: var(--accent, #3b82f6);
      color: var(--fg, inherit);
      font-weight: 600;
    }
    .git-diff-base-custom-row {
      display: flex;
      gap: 4px;
      align-items: center;
    }
    .git-diff-base-input {
      flex: 1;
      min-width: 0;
      padding: 3px 6px;
      font-size: 11px;
      border-radius: 4px;
      border: 1px solid var(--border, rgba(128,128,128,0.28));
      background: var(--bg, transparent);
      color: var(--fg, inherit);
      font-family: var(--font-mono, monospace);
    }
    .git-worktrees-section {
      border-top: 1px solid var(--border, rgba(128,128,128,0.16));
      padding: 6px 0 4px;
    }
    .git-worktrees-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 3px 10px;
      cursor: pointer;
      user-select: none;
    }
    .git-worktrees-title-wrap {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      font-size: 10.5px;
      font-weight: 600;
      letter-spacing: 0.04em;
      text-transform: uppercase;
      color: var(--fg-muted, #888);
    }
    .git-worktrees-actions {
      display: inline-flex;
      align-items: center;
      gap: 4px;
    }
    .git-wt-mini-btn {
      padding: 1px 6px;
      font-size: 10.5px;
      border-radius: 4px;
      border: 1px solid var(--border, rgba(128,128,128,0.24));
      background: transparent;
      color: var(--fg-muted, inherit);
      cursor: pointer;
    }
    .git-wt-mini-btn:hover {
      background: var(--bg-hover, rgba(128,128,128,0.14));
      color: var(--fg, inherit);
    }
    .git-worktrees-list {
      display: flex;
      flex-direction: column;
      gap: 3px;
      padding: 4px 8px 6px;
    }
    .git-wt-item {
      display: flex;
      flex-direction: column;
      gap: 3px;
      padding: 6px 8px;
      border-radius: 6px;
      border: 1px solid var(--border, rgba(128,128,128,0.16));
      background: var(--bg-subtle, rgba(128,128,128,0.03));
      cursor: pointer;
      transition: background 0.12s ease, border-color 0.12s ease;
    }
    .git-wt-item:hover {
      background: var(--bg-hover, rgba(128,128,128,0.10));
      border-color: var(--border-strong, rgba(128,128,128,0.32));
    }
    .git-wt-item.is-current {
      border-color: var(--accent, #3b82f6);
      background: var(--accent-subtle, rgba(59, 130, 246, 0.07));
      cursor: default;
    }
    .git-wt-row-top {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 6px;
    }
    .git-wt-branch {
      font-size: 11.5px;
      font-weight: 600;
      color: var(--fg, inherit);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      display: inline-flex;
      align-items: center;
      gap: 5px;
    }
    .git-wt-badges {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      flex-shrink: 0;
    }
    .git-wt-badge {
      font-size: 9.5px;
      font-weight: 600;
      padding: 1px 5px;
      border-radius: 999px;
      background: rgba(128,128,128,0.16);
      color: var(--fg-muted, inherit);
    }
    .git-wt-badge.source-claude {
      background: rgba(217, 119, 87, 0.18);
      color: #d97757;
    }
    .git-wt-badge.source-px0 {
      background: rgba(59, 130, 246, 0.16);
      color: #3b82f6;
    }
    .git-wt-badge.badge-active {
      background: rgba(34, 197, 94, 0.18);
      color: #22c55e;
    }
    .git-wt-row-meta {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 6px;
      font-size: 10.5px;
      color: var(--fg-muted, #888);
    }
    .git-wt-subject {
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      flex: 1;
    }
    .git-wt-remove-btn {
      border: none;
      background: transparent;
      color: var(--fg-muted, #888);
      cursor: pointer;
      padding: 0 3px;
      font-size: 13px;
      line-height: 1;
      border-radius: 3px;
    }
    .git-wt-remove-btn:hover {
      background: rgba(239, 68, 68, 0.16);
      color: #ef4444;
    }
    .worktree-modal-backdrop {
      position: fixed;
      inset: 0;
      z-index: 1200;
      background: rgba(0, 0, 0, 0.45);
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 20px;
    }
    .worktree-modal-backdrop[hidden] {
      display: none !important;
    }
    .worktree-modal-card {
      width: min(640px, 94vw);
      max-height: 82vh;
      display: flex;
      flex-direction: column;
      border-radius: 10px;
      border: 1px solid var(--border, rgba(128,128,128,0.28));
      background: var(--bg-elevated, var(--bg, #1e1e1e));
      color: var(--fg, inherit);
      box-shadow: 0 18px 48px rgba(0, 0, 0, 0.38);
      overflow: hidden;
    }
    .worktree-modal-head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 12px 16px;
      border-bottom: 1px solid var(--border, rgba(128,128,128,0.2));
    }
    .worktree-modal-title {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      font-size: 13.5px;
      font-weight: 600;
    }
    .worktree-modal-tabs {
      display: flex;
      gap: 6px;
      padding: 8px 16px;
      border-bottom: 1px solid var(--border, rgba(128,128,128,0.16));
      background: var(--bg-subtle, rgba(128,128,128,0.04));
    }
    .worktree-modal-tab {
      padding: 4px 10px;
      border-radius: 6px;
      border: 1px solid transparent;
      background: transparent;
      color: var(--fg-muted, inherit);
      font-size: 12px;
      font-weight: 500;
      cursor: pointer;
    }
    .worktree-modal-tab.active {
      background: var(--bg-elevated, rgba(128,128,128,0.14));
      border-color: var(--border, rgba(128,128,128,0.26));
      color: var(--fg, inherit);
      font-weight: 600;
    }
    .worktree-modal-body {
      padding: 14px 16px;
      overflow-y: auto;
      display: flex;
      flex-direction: column;
      gap: 12px;
    }
    .wt-form-field {
      display: flex;
      flex-direction: column;
      gap: 4px;
    }
    .wt-form-field label {
      font-size: 11.5px;
      font-weight: 600;
      color: var(--fg, inherit);
    }
    .wt-form-field input[type="text"] {
      padding: 6px 9px;
      border-radius: 6px;
      border: 1px solid var(--border, rgba(128,128,128,0.28));
      background: var(--bg, transparent);
      color: var(--fg, inherit);
      font-size: 12px;
      font-family: var(--font-mono, monospace);
    }
    .wt-form-help {
      font-size: 11px;
      color: var(--fg-muted, #888);
    }
  `;
  document.head.appendChild(style);
}

function getCurrentWorktree() {
  return worktreeState.worktrees.find(w => w.isCurrent) || null;
}

async function performWorkspaceSwitch(targetPath) {
  if (!targetPath) return;
  if (typeof switchWorkspaceCallback === 'function') {
    await switchWorkspaceCallback(targetPath);
    await refreshWorktrees();
    return;
  }
  try {
    const res = await apiPostJson('/api/workspaces/switch', { path: targetPath });
    if (res && res.meta) {
      S.meta = res.meta;
    } else {
      S.meta = await api('/api/meta');
    }
    if (S.meta?.name) {
      document.title = S.meta.name + ' - px0';
      const rn = $('#root-name');
      if (rn) {
        rn.textContent = S.meta.name;
        rn.title = S.meta.root || targetPath;
      }
    }
    updateSidebarToggleState();
    syncGitPanelMeta();
    await refreshTree();
    await reloadOpenTabs({ onlyIfChanged: false });
    await triggerRefresh();
    await refreshWorktrees();
    showToast('✓', `Switched to worktree ${targetPath.split('/').pop()}`);
  } catch (err) {
    showToast('!', err?.message || 'Failed to switch worktree');
  }
}

export async function setDiffBaseMode(mode, ref = '') {
  try {
    const res = await apiPostJson('/api/git/diff-base', { mode, ref });
    worktreeState.currentMode = res.diffMode || mode;
    worktreeState.diffBase = res.diffBase || 'HEAD';
    worktreeState.diffBaseRef = res.diffBaseRef || '';

    if (S.meta) {
      S.meta.diffMode = worktreeState.currentMode;
      S.meta.diffBase = worktreeState.diffBase;
      S.meta.diffBaseRef = worktreeState.diffBaseRef;
      if (typeof res.gitChanges === 'number') S.meta.gitChanges = res.gitChanges;
      if (Array.isArray(res.gitFiles)) S.meta.gitFiles = res.gitFiles;
    }

    // Invalidate cached per-tab diffs so open tabs reload against the new base
    for (const tab of S.tabs) {
      if (!tab.diffRef) {
        tab.diffText = undefined;
        tab.diffHunks = undefined;
        if (worktreeState.currentMode !== 'head') {
          tab.diffDismissed = false;
        }
      }
    }

    updateSidebarToggleState();
    syncGitPanelMeta();
    await refreshTree();
    if (inGitMode() && worktreeState.currentMode !== 'head') {
      await expandDirtyDirs();
    }
    await reloadOpenTabs({ onlyIfChanged: false });
    await triggerRefresh();

    renderWorktreePill();
    renderDiffBaseBar();
    renderGitPanelWorktrees();
    renderModalContent();

    if (typeof diffBaseChangedCallback === 'function') {
      await diffBaseChangedCallback(res);
    }

    const label = worktreeState.currentMode === 'base'
      ? `Comparing against ${worktreeState.diffBaseRef || worktreeState.baseBranch || 'base'}`
      : worktreeState.currentMode === 'custom'
        ? `Comparing against ${worktreeState.diffBaseRef || worktreeState.diffBase.slice(0, 7)}`
        : 'Showing uncommitted working tree changes (HEAD)';
    showToast('✓', label);
    return res;
  } catch (err) {
    showToast('!', err?.message || 'Failed to change diff base');
    throw err;
  }
}

export async function toggleWorktreeBaseDiff() {
  const nextMode = worktreeState.currentMode === 'base' ? 'head' : 'base';
  const baseRef = worktreeState.baseBranch || 'main';
  return setDiffBaseMode(nextMode, nextMode === 'base' ? baseRef : '');
}

function ensureWorktreePill() {
  let pill = $('#worktree-pill');
  if (pill) return pill;
  const rootName = $('#root-name');
  if (!rootName || !rootName.parentElement) return null;
  pill = document.createElement('button');
  pill.id = 'worktree-pill';
  pill.type = 'button';
  pill.className = 'worktree-pill';
  pill.hidden = true;
  pill.addEventListener('click', (e) => {
    e.stopPropagation();
    openWorktreeModal('list');
  });
  rootName.after(pill);
  return pill;
}

function renderWorktreePill() {
  const pill = ensureWorktreePill();
  if (!pill) return;

  const wts = worktreeState.worktrees || [];
  const cur = getCurrentWorktree();
  const show = S.meta?.git && (wts.length > 1 || (cur && !cur.isMain) || worktreeState.currentMode !== 'head');
  if (!show) {
    pill.hidden = true;
    return;
  }

  const isClaude = cur?.source === 'claude';
  const isBaseDiff = worktreeState.currentMode !== 'head';
  pill.classList.toggle('is-claude', !!isClaude);
  pill.classList.toggle('is-base-diff', !!isBaseDiff);

  const branchLabel = cur?.branch || S.meta?.branch || 'worktree';
  let badgeHtml = '';
  if (isClaude) {
    badgeHtml = '<span class="worktree-pill-badge">Claude</span>';
  } else if (wts.length > 1) {
    badgeHtml = `<span class="worktree-pill-badge">${wts.length}</span>`;
  }
  if (isBaseDiff) {
    const refLabel = worktreeState.diffBaseRef || worktreeState.baseBranch || 'base';
    badgeHtml += `<span class="worktree-pill-badge" title="Diffing against ${esc(refLabel)}">vs ${esc(refLabel)}</span>`;
  }

  pill.innerHTML = `${WORKTREE_ICON_SVG}<span class="worktree-pill-branch">${esc(branchLabel)}</span>${badgeHtml}`;
  pill.title = cur
    ? `Worktree: ${cur.name} (${cur.branch}) — Click to manage worktrees & base diff`
    : 'Manage Git worktrees & base-branch diff';
  pill.hidden = false;
}

function ensureDiffBaseBar() {
  let bar = $('#git-diff-base-bar');
  if (bar) return bar;
  const panelBody = $('#git-panel-body') || $('#git-panel');
  if (!panelBody) return null;
  bar = document.createElement('div');
  bar.id = 'git-diff-base-bar';
  bar.className = 'git-diff-base-bar';
  bar.hidden = true;
  panelBody.prepend(bar);
  return bar;
}

function renderDiffBaseBar() {
  const bar = ensureDiffBaseBar();
  if (!bar) return;
  if (!S.meta?.git || S.meta?.pr) {
    bar.hidden = true;
    return;
  }

  const cur = getCurrentWorktree();
  const baseBranch = cur?.baseBranch || worktreeState.baseBranch || 'main';
  const ahead = cur?.aheadOfBase || 0;
  const changedVsBase = cur?.changedFilesVsBase || 0;
  const mode = worktreeState.currentMode || S.meta?.diffMode || 'head';
  const isLinkedOrAhead = (cur && !cur.isMain) || ahead > 0 || (worktreeState.worktrees?.length > 1) || mode !== 'head';

  if (!isLinkedOrAhead && !baseBranch) {
    bar.hidden = true;
    return;
  }
  bar.hidden = false;

  const baseSubLabel = ahead > 0
    ? `+${ahead} commit${ahead === 1 ? '' : 's'}${changedVsBase > 0 ? ` · ${changedVsBase}f` : ''}`
    : (changedVsBase > 0 ? `${changedVsBase} file${changedVsBase === 1 ? '' : 's'}` : '');

  const showCustomInput = bar.dataset.customOpen === '1' || mode === 'custom';

  bar.innerHTML = `
    <div class="git-diff-base-top">
      <span class="git-diff-base-label">
        ${WORKTREE_ICON_SVG}
        <span>Diff Scope</span>
        ${cur?.source === 'claude' ? '<span class="git-wt-badge source-claude">Claude Worktree</span>' : ''}
      </span>
      <button type="button" class="git-wt-mini-btn" data-action="open-wt-modal" title="Manage Worktrees">Worktrees (${worktreeState.worktrees.length || 1})</button>
    </div>
    <div class="git-diff-base-seg">
      <button type="button" class="git-diff-base-btn ${mode === 'head' ? 'active' : ''}" data-diff-mode="head" title="Show only uncommitted changes in the working tree (HEAD)">
        <span>Working Tree</span>
      </button>
      <button type="button" class="git-diff-base-btn ${mode === 'base' ? 'active' : ''}" data-diff-mode="base" data-base-ref="${esc(baseBranch)}" title="Show all changes on this branch since diverging from ${esc(baseBranch)}">
        <span>vs ${esc(baseBranch)}</span>
        ${baseSubLabel ? `<span class="git-wt-badge">${esc(baseSubLabel)}</span>` : ''}
      </button>
      <button type="button" class="git-diff-base-btn ${mode === 'custom' ? 'active' : ''}" data-diff-mode="custom-toggle" title="Compare against any branch, tag, or commit SHA">
        <span>${mode === 'custom' && worktreeState.diffBaseRef ? esc(worktreeState.diffBaseRef) : 'Custom…'}</span>
      </button>
    </div>
    ${showCustomInput ? `
      <form class="git-diff-base-custom-row" id="git-diff-base-custom-form">
        <input type="text" class="git-diff-base-input" id="git-diff-base-custom-input" placeholder="Branch, tag, or SHA (e.g. main, HEAD~3)" value="${esc(worktreeState.diffBaseRef || baseBranch)}" />
        <button type="submit" class="git-wt-mini-btn">Apply</button>
      </form>
    ` : ''}
  `;

  bar.querySelectorAll('[data-diff-mode]').forEach(btn => {
    btn.addEventListener('click', async (e) => {
      e.stopPropagation();
      const m = btn.dataset.diffMode;
      if (m === 'head') {
        bar.dataset.customOpen = '0';
        await setDiffBaseMode('head', '');
      } else if (m === 'base') {
        bar.dataset.customOpen = '0';
        await setDiffBaseMode('base', btn.dataset.baseRef || baseBranch);
      } else if (m === 'custom-toggle') {
        bar.dataset.customOpen = bar.dataset.customOpen === '1' ? '0' : '1';
        renderDiffBaseBar();
        $('#git-diff-base-custom-input')?.focus();
      }
    });
  });

  bar.querySelector('[data-action="open-wt-modal"]')?.addEventListener('click', (e) => {
    e.stopPropagation();
    openWorktreeModal('list');
  });

  const customForm = bar.querySelector('#git-diff-base-custom-form');
  if (customForm) {
    customForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const val = $('#git-diff-base-custom-input')?.value?.trim();
      if (!val) return;
      await setDiffBaseMode('custom', val);
    });
  }
}

function ensureGitPanelWorktreesSection() {
  let sec = $('#git-worktrees-section');
  if (sec) return sec;
  const panelBody = $('#git-panel-body') || $('#git-panel');
  if (!panelBody) return null;
  sec = document.createElement('div');
  sec.id = 'git-worktrees-section';
  sec.className = 'git-worktrees-section';
  sec.hidden = true;

  const recentCommits = $('#git-recent-commits');
  if (recentCommits && recentCommits.parentElement === panelBody) {
    panelBody.insertBefore(sec, recentCommits);
  } else {
    panelBody.appendChild(sec);
  }
  return sec;
}

function sourceBadgeHtml(wt) {
  if (wt.source === 'claude') {
    return '<span class="git-wt-badge source-claude" title="Claude Desktop worktree">Claude</span>';
  }
  if (wt.isMain || wt.source === 'main') {
    return '<span class="git-wt-badge" title="Primary repository checkout">Main</span>';
  }
  if (wt.source === 'px0') {
    return '<span class="git-wt-badge source-px0" title="Managed by px0">px0</span>';
  }
  return '<span class="git-wt-badge">Worktree</span>';
}

function renderGitPanelWorktrees() {
  const sec = ensureGitPanelWorktreesSection();
  if (!sec) return;
  if (!S.meta?.git || S.meta?.pr) {
    sec.hidden = true;
    return;
  }

  const wts = worktreeState.worktrees || [];
  sec.hidden = false;
  const hasPrunable = wts.some(w => w.prunable);

  sec.innerHTML = `
    <div class="git-worktrees-head" id="git-worktrees-toggle">
      <span class="git-worktrees-title-wrap">
        <span>${sectionCollapsed ? '▸' : '▾'}</span>
        <span>Worktrees</span>
        <span class="git-wt-badge" id="git-worktrees-count">${wts.length}</span>
      </span>
      <span class="git-worktrees-actions">
        ${hasPrunable ? '<button type="button" class="git-wt-mini-btn" data-wt-action="prune" title="Prune stale worktrees">Prune</button>' : ''}
        <button type="button" class="git-wt-mini-btn" data-wt-action="new" title="Create a new Git worktree">+ New</button>
      </span>
    </div>
    <div class="git-worktrees-list" ${sectionCollapsed ? 'hidden' : ''}>
      ${wts.map(wt => {
        const metaParts = [];
        if (wt.dirtyCount > 0) metaParts.push(`${wt.dirtyCount} dirty`);
        if (wt.aheadOfBase > 0 && wt.baseBranch) metaParts.push(`+${wt.aheadOfBase} vs ${wt.baseBranch}`);
        else if (wt.changedFilesVsBase > 0 && wt.baseBranch) metaParts.push(`${wt.changedFilesVsBase}f vs ${wt.baseBranch}`);
        if (wt.age) metaParts.push(wt.age);
        const metaSummary = metaParts.join(' · ') || wt.head || '';

        return `
          <div class="git-wt-item ${wt.isCurrent ? 'is-current' : ''}" data-wt-path="${esc(wt.path)}" title="${esc(wt.path)}">
            <div class="git-wt-row-top">
              <span class="git-wt-branch">
                ${WORKTREE_ICON_SVG}
                <span>${esc(wt.branch || wt.name)}</span>
              </span>
              <span class="git-wt-badges">
                ${sourceBadgeHtml(wt)}
                ${wt.isCurrent ? '<span class="git-wt-badge badge-active">Active</span>' : ''}
                ${!wt.isMain && !wt.isCurrent ? `<button type="button" class="git-wt-remove-btn" data-wt-remove="${esc(wt.path)}" data-wt-dirty="${wt.dirtyCount || 0}" title="Remove worktree ${esc(wt.name)}">×</button>` : ''}
              </span>
            </div>
            <div class="git-wt-row-meta">
              <span class="git-wt-subject">${esc(wt.subject || wt.name)}</span>
              <span>${esc(metaSummary)}</span>
            </div>
          </div>
        `;
      }).join('')}
    </div>
  `;

  sec.querySelector('#git-worktrees-toggle')?.addEventListener('click', (e) => {
    if (e.target.closest('button')) return;
    sectionCollapsed = !sectionCollapsed;
    renderGitPanelWorktrees();
  });

  sec.querySelector('[data-wt-action="new"]')?.addEventListener('click', (e) => {
    e.stopPropagation();
    openWorktreeModal('create');
  });

  sec.querySelector('[data-wt-action="prune"]')?.addEventListener('click', async (e) => {
    e.stopPropagation();
    await handlePruneWorktrees();
  });

  sec.querySelectorAll('.git-wt-item').forEach(row => {
    row.addEventListener('click', async (e) => {
      const rmBtn = e.target.closest('[data-wt-remove]');
      if (rmBtn) {
        e.stopPropagation();
        await handleRemoveWorktree(rmBtn.dataset.wtRemove, Number(rmBtn.dataset.wtDirty) > 0);
        return;
      }
      const targetPath = row.dataset.wtPath;
      if (!targetPath || row.classList.contains('is-current')) return;
      await performWorkspaceSwitch(targetPath);
    });
  });
}

async function handleRemoveWorktree(path, isDirty = false) {
  if (!path) return;
  const name = path.split('/').pop();
  const promptMsg = isDirty
    ? `Worktree "${name}" has uncommitted changes. Force remove it and discard its working tree?`
    : `Remove worktree "${name}" (${path})?`;
  if (!confirm(promptMsg)) return;
  try {
    await apiPostJson('/api/git/worktrees/remove', { path, force: isDirty });
    showToast('✓', `Removed worktree ${name}`);
    await refreshWorktrees();
  } catch (err) {
    if (!isDirty && confirm(`${err?.message || 'Failed to remove worktree'}.\n\nForce remove anyway?`)) {
      try {
        await apiPostJson('/api/git/worktrees/remove', { path, force: true });
        showToast('✓', `Removed worktree ${name}`);
        await refreshWorktrees();
      } catch (err2) {
        showToast('!', err2?.message || 'Failed to force remove worktree');
      }
    } else {
      showToast('!', err?.message || 'Failed to remove worktree');
    }
  }
}

async function handlePruneWorktrees() {
  try {
    await apiPostJson('/api/git/worktrees/prune', {});
    showToast('✓', 'Pruned stale worktree entries');
    await refreshWorktrees();
  } catch (err) {
    showToast('!', err?.message || 'Failed to prune worktrees');
  }
}

function ensureWorktreeModal() {
  let modal = $('#worktree-modal');
  if (modal) return modal;
  modal = document.createElement('div');
  modal.id = 'worktree-modal';
  modal.className = 'worktree-modal-backdrop';
  modal.hidden = true;
  modal.addEventListener('click', (e) => {
    if (e.target === modal) {
      modal.hidden = true;
    }
  });
  document.body.appendChild(modal);
  return modal;
}

function renderModalContent() {
  const modal = $('#worktree-modal');
  if (!modal || modal.hidden) return;

  const wts = worktreeState.worktrees || [];
  const baseBranch = worktreeState.baseBranch || 'main';
  const mode = worktreeState.currentMode || 'head';
  const filtered = modalFilterText
    ? wts.filter(w =>
        (w.branch || '').toLowerCase().includes(modalFilterText.toLowerCase()) ||
        (w.name || '').toLowerCase().includes(modalFilterText.toLowerCase()) ||
        (w.path || '').toLowerCase().includes(modalFilterText.toLowerCase()) ||
        (w.source || '').toLowerCase().includes(modalFilterText.toLowerCase())
      )
    : wts;

  modal.innerHTML = `
    <div class="worktree-modal-card" role="dialog" aria-modal="true" aria-label="Git Worktrees & Branch Review">
      <div class="worktree-modal-head">
        <span class="worktree-modal-title">
          ${WORKTREE_ICON_SVG}
          <span>Git Worktrees & Claude Branch Review</span>
        </span>
        <button type="button" class="git-wt-mini-btn" data-modal-close="1">Esc</button>
      </div>
      <div class="worktree-modal-tabs">
        <button type="button" class="worktree-modal-tab ${activeModalTab === 'list' ? 'active' : ''}" data-modal-tab="list">
          Worktrees (${wts.length})
        </button>
        <button type="button" class="worktree-modal-tab ${activeModalTab === 'create' ? 'active' : ''}" data-modal-tab="create">
          + New Worktree
        </button>
        <button type="button" class="worktree-modal-tab ${activeModalTab === 'review' ? 'active' : ''}" data-modal-tab="review">
          Diff Base & Settings
        </button>
      </div>
      <div class="worktree-modal-body">
        ${activeModalTab === 'list' ? `
          <div style="display:flex; gap:6px; align-items:center;">
            <input type="text" class="git-diff-base-input" id="wt-modal-filter" placeholder="Filter worktrees by branch, path, or Claude session…" value="${esc(modalFilterText)}" style="padding:6px 9px; font-size:12px;" />
            <button type="button" class="git-wt-mini-btn" id="wt-modal-prune" title="Run git worktree prune">Prune Stale</button>
          </div>
          <div style="display:flex; flex-direction:column; gap:6px;">
            ${filtered.length === 0 ? '<div class="wt-form-help">No matching worktrees found.</div>' : filtered.map(wt => `
              <div class="git-wt-item ${wt.isCurrent ? 'is-current' : ''}" data-modal-wt-path="${esc(wt.path)}">
                <div class="git-wt-row-top">
                  <span class="git-wt-branch">
                    ${WORKTREE_ICON_SVG}
                    <span>${esc(wt.branch || wt.name)}</span>
                    <span class="wt-form-help" style="font-weight:400;">(${esc(wt.head || '')})</span>
                  </span>
                  <span class="git-wt-badges">
                    ${sourceBadgeHtml(wt)}
                    ${wt.isCurrent ? '<span class="git-wt-badge badge-active">Current Workspace</span>' : ''}
                    ${wt.dirtyCount > 0 ? `<span class="git-wt-badge">${wt.dirtyCount} dirty</span>` : ''}
                    ${wt.aheadOfBase > 0 ? `<span class="git-wt-badge">+${wt.aheadOfBase} vs ${esc(wt.baseBranch || baseBranch)} (${wt.changedFilesVsBase} files)</span>` : ''}
                  </span>
                </div>
                <div class="git-wt-row-meta" style="margin-top:2px;">
                  <span class="git-wt-subject" style="font-family:var(--font-mono, monospace);">${esc(wt.path)}</span>
                  <span style="display:inline-flex; gap:4px; flex-shrink:0;">
                    <button type="button" class="git-wt-mini-btn" data-copy-wt="${esc(wt.path)}">Copy Path</button>
                    ${!wt.isCurrent ? `<button type="button" class="git-wt-mini-btn" data-switch-wt="${esc(wt.path)}">Switch</button>` : ''}
                    ${wt.isCurrent && !wt.isMain && baseBranch ? `<button type="button" class="git-wt-mini-btn" data-review-base="${esc(wt.baseBranch || baseBranch)}">${mode === 'base' ? 'Diffing vs ' + esc(wt.baseBranch || baseBranch) : 'Review vs ' + esc(wt.baseBranch || baseBranch)}</button>` : ''}
                    ${!wt.isMain && !wt.isCurrent ? `<button type="button" class="git-wt-mini-btn" data-remove-wt="${esc(wt.path)}" data-dirty="${wt.dirtyCount || 0}">Remove</button>` : ''}
                  </span>
                </div>
                ${wt.subject ? `<div class="wt-form-help">${esc(wt.subject)} ${wt.age ? `· ${esc(wt.age)}` : ''}</div>` : ''}
              </div>
            `).join('')}
          </div>
        ` : activeModalTab === 'create' ? `
          <form id="wt-create-form" style="display:flex; flex-direction:column; gap:10px;">
            <div class="wt-form-field">
              <label for="wt-create-branch">Branch Name</label>
              <input type="text" id="wt-create-branch" placeholder="feat/claude-session or fix/issue-42" required />
              <span class="wt-form-help">If the branch already exists, it will be checked out; otherwise a new branch is created from the Base Ref.</span>
            </div>
            <div class="wt-form-field">
              <label for="wt-create-base">Base Ref (Start Point)</label>
              <input type="text" id="wt-create-base" placeholder="${esc(baseBranch || 'HEAD')}" value="${esc(baseBranch || 'HEAD')}" />
            </div>
            <div class="wt-form-field">
              <label for="wt-create-dir">Parent Directory for Worktrees</label>
              <input type="text" id="wt-create-dir" value="${esc(worktreeState.worktreeDir || '')}" placeholder="~/.px0/worktrees/repo" />
              <span class="wt-form-help">Worktrees are placed outside your main checkout by default so they never litter your repository.</span>
            </div>
            <label style="display:inline-flex; align-items:center; gap:6px; font-size:12px; cursor:pointer;">
              <input type="checkbox" id="wt-create-switch" checked />
              <span>Switch workspace to the new worktree immediately</span>
            </label>
            <div style="display:flex; justify-content:flex-end; gap:8px; margin-top:4px;">
              <button type="button" class="git-wt-mini-btn" data-modal-tab="list" style="padding:5px 12px; font-size:12px;">Cancel</button>
              <button type="submit" class="git-diff-base-btn active" style="flex:0 0 auto; padding:5px 14px; font-size:12px;">Create Worktree</button>
            </div>
          </form>
        ` : `
          <div class="wt-form-field">
            <label>Active Diff Base Mode</label>
            <span class="wt-form-help">When reviewing a Claude Desktop worktree or feature branch whose changes are already committed, switch to <b>Branch vs ${esc(baseBranch)}</b> so all modified files appear in the Git sidebar and editor diff view.</span>
            <div class="git-diff-base-seg" style="margin-top:6px;">
              <button type="button" class="git-diff-base-btn ${mode === 'head' ? 'active' : ''}" data-modal-diff="head">Working Tree (HEAD)</button>
              <button type="button" class="git-diff-base-btn ${mode === 'base' ? 'active' : ''}" data-modal-diff="base" data-ref="${esc(baseBranch)}">Branch vs ${esc(baseBranch)}</button>
            </div>
          </div>
          <form id="wt-modal-custom-diff-form" class="wt-form-field" style="margin-top:4px;">
            <label for="wt-modal-custom-ref">Custom Diff Base Ref (Branch, Tag, or Commit SHA)</label>
            <div style="display:flex; gap:6px;">
              <input type="text" id="wt-modal-custom-ref" style="flex:1;" placeholder="e.g. main, origin/main, HEAD~5" value="${esc(worktreeState.diffBaseRef || baseBranch)}" />
              <button type="submit" class="git-wt-mini-btn" style="padding:5px 12px;">Compare</button>
            </div>
          </form>
          <hr style="border:none; border-top:1px solid var(--border, rgba(128,128,128,0.18)); margin:4px 0;" />
          <label style="display:inline-flex; align-items:center; gap:7px; font-size:12px; cursor:pointer;">
            <input type="checkbox" id="wt-setting-auto-base" ${worktreeState.autoBaseDiff ? 'checked' : ''} />
            <span>Automatically enable <b>Branch vs ${esc(baseBranch)}</b> diff mode when opening a linked worktree with commits ahead of base</span>
          </label>
        `}
      </div>
    </div>
  `;

  modal.querySelector('[data-modal-close]')?.addEventListener('click', () => {
    modal.hidden = true;
  });

  modal.querySelectorAll('[data-modal-tab]').forEach(tabBtn => {
    tabBtn.addEventListener('click', () => {
      activeModalTab = tabBtn.dataset.modalTab;
      renderModalContent();
    });
  });

  const filterInput = modal.querySelector('#wt-modal-filter');
  if (filterInput) {
    filterInput.addEventListener('input', () => {
      modalFilterText = filterInput.value;
      const pos = filterInput.selectionStart;
      renderModalContent();
      const nextInput = $('#wt-modal-filter');
      if (nextInput) {
        nextInput.focus();
        nextInput.setSelectionRange(pos, pos);
      }
    });
  }

  modal.querySelector('#wt-modal-prune')?.addEventListener('click', async () => {
    await handlePruneWorktrees();
  });

  modal.querySelectorAll('[data-copy-wt]').forEach(btn => {
    btn.addEventListener('click', async (e) => {
      e.stopPropagation();
      await copyToClipboard(btn.dataset.copyWt, 'Copied worktree path');
    });
  });

  modal.querySelectorAll('[data-switch-wt]').forEach(btn => {
    btn.addEventListener('click', async (e) => {
      e.stopPropagation();
      modal.hidden = true;
      await performWorkspaceSwitch(btn.dataset.switchWt);
    });
  });

  modal.querySelectorAll('[data-review-base]').forEach(btn => {
    btn.addEventListener('click', async (e) => {
      e.stopPropagation();
      await toggleWorktreeBaseDiff();
      await setSidebarMode('git', { expandDirty: true });
      modal.hidden = true;
    });
  });

  modal.querySelectorAll('[data-remove-wt]').forEach(btn => {
    btn.addEventListener('click', async (e) => {
      e.stopPropagation();
      await handleRemoveWorktree(btn.dataset.removeWt, Number(btn.dataset.dirty) > 0);
    });
  });

  const createForm = modal.querySelector('#wt-create-form');
  if (createForm) {
    createForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const branch = $('#wt-create-branch')?.value?.trim();
      const baseRef = $('#wt-create-base')?.value?.trim();
      const worktreeDir = $('#wt-create-dir')?.value?.trim();
      const switchTo = !!$('#wt-create-switch')?.checked;
      if (!branch) return;

      try {
        const res = await apiPostJson('/api/git/worktrees/create', {
          branch,
          baseRef,
          worktreeDir,
          switchTo,
        });
        showToast('✓', `Created worktree for ${branch}`);
        await refreshWorktrees();
        if (switchTo && res?.path) {
          modal.hidden = true;
          await performWorkspaceSwitch(res.path);
        } else {
          activeModalTab = 'list';
          renderModalContent();
        }
      } catch (err) {
        showToast('!', err?.message || 'Failed to create worktree');
      }
    });
  }

  modal.querySelectorAll('[data-modal-diff]').forEach(btn => {
    btn.addEventListener('click', async () => {
      const m = btn.dataset.modalDiff;
      await setDiffBaseMode(m, btn.dataset.ref || '');
    });
  });

  const customDiffForm = modal.querySelector('#wt-modal-custom-diff-form');
  if (customDiffForm) {
    customDiffForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const ref = $('#wt-modal-custom-ref')?.value?.trim();
      if (!ref) return;
      await setDiffBaseMode('custom', ref);
    });
  }

  const autoBaseCheckbox = modal.querySelector('#wt-setting-auto-base');
  if (autoBaseCheckbox) {
    autoBaseCheckbox.addEventListener('change', async () => {
      const enabled = !!autoBaseCheckbox.checked;
      worktreeState.autoBaseDiff = enabled;
      try {
        await apiPostJson('/api/settings', { 'git.worktreeAutoBaseDiff': enabled });
        showToast('✓', enabled ? 'Auto base-diff enabled for worktrees' : 'Auto base-diff disabled');
      } catch (err) {
        showToast('!', err?.message || 'Failed to update setting');
      }
    });
  }
}

export async function openWorktreeModal(initialTab = 'list') {
  activeModalTab = initialTab || 'list';
  const modal = ensureWorktreeModal();
  modal.hidden = false;
  await refreshWorktrees();
  renderModalContent();
  if (activeModalTab === 'create') {
    $('#wt-create-branch')?.focus();
  } else if (activeModalTab === 'list') {
    $('#wt-modal-filter')?.focus();
  }
}

export async function refreshWorktrees() {
  if (!S.meta?.git || S.meta?.pr) {
    worktreeState.worktrees = [];
    renderWorktreePill();
    renderDiffBaseBar();
    renderGitPanelWorktrees();
    return worktreeState;
  }
  try {
    const data = await api('/api/git/worktrees');
    worktreeState = {
      worktrees: Array.isArray(data.worktrees) ? data.worktrees : [],
      baseBranch: data.baseBranch || '',
      currentMode: data.currentMode || S.meta?.diffMode || 'head',
      diffBase: data.diffBase || S.meta?.diffBase || 'HEAD',
      diffBaseRef: data.diffBaseRef || S.meta?.diffBaseRef || '',
      worktreeDir: data.worktreeDir || '',
      autoBaseDiff: data.autoBaseDiff !== false,
    };
    if (S.meta) {
      S.meta.diffMode = worktreeState.currentMode;
      S.meta.diffBase = worktreeState.diffBase;
      S.meta.diffBaseRef = worktreeState.diffBaseRef;
    }
  } catch {
    // Keep existing state on transient error
  }
  renderWorktreePill();
  renderDiffBaseBar();
  renderGitPanelWorktrees();
  renderModalContent();
  return worktreeState;
}

// Git status events arrive up to once a second, and each /api/git/worktrees
// call runs several git processes per worktree. Coalesce event-driven
// refreshes to at most one per WORKTREE_REFRESH_MS.
const WORKTREE_REFRESH_MS = 5000;
let worktreeRefreshTimer = null;
let lastWorktreeRefresh = 0;

export function scheduleWorktreesRefresh() {
  if (worktreeRefreshTimer) return;
  const wait = Math.max(0, lastWorktreeRefresh + WORKTREE_REFRESH_MS - Date.now());
  worktreeRefreshTimer = setTimeout(async () => {
    try {
      await refreshWorktrees();
    } finally {
      lastWorktreeRefresh = Date.now();
      worktreeRefreshTimer = null;
    }
  }, wait);
}

export function getWorktreesState() {
  return worktreeState;
}

export function initWorktrees({ onSwitchWorkspace, onDiffBaseChanged } = {}) {
  if (onSwitchWorkspace) switchWorkspaceCallback = onSwitchWorkspace;
  if (onDiffBaseChanged) diffBaseChangedCallback = onDiffBaseChanged;

  injectWorktreeStyles();
  ensureWorktreePill();
  ensureDiffBaseBar();
  ensureGitPanelWorktreesSection();
  ensureWorktreeModal();

  if (!initialized) {
    initialized = true;
    window.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        const modal = $('#worktree-modal');
        if (modal && !modal.hidden) {
          e.stopPropagation();
          modal.hidden = true;
        }
      }
    });
  }
}
