// web/src/workspace-switcher.js
import { $, esc, api, apiPostJson, S, withKeys } from './state.js';
import { showToast } from './ui.js';

let onSwitchedCallback = null;
let modalEl = null;
let inputEl = null;
let listEl = null;
let rootsChipsEl = null;
let rootInputEl = null;
let countBadgeEl = null;

let wsState = {
  current: '',
  name: '',
  configuredRoots: [],
  repos: [],
  loading: false,
  switching: false,
  selectedIndex: 0,
  filteredItems: [],
};

function injectStyles() {
  if (document.getElementById('ws-switcher-styles')) return;
  const style = document.createElement('style');
  style.id = 'ws-switcher-styles';
  style.textContent = `
    #root-name.ws-root-trigger {
      cursor: pointer;
      display: inline-flex;
      align-items: center;
      gap: 5px;
      padding: 2px 6px;
      margin: -2px -6px;
      border-radius: 5px;
      transition: background 0.12s ease, color 0.12s ease;
      user-select: none;
    }
    #root-name.ws-root-trigger:hover {
      background: var(--row-hover, rgba(128, 128, 128, 0.12));
      color: var(--fg);
    }
    #root-name.ws-root-trigger::after {
      content: '▾';
      font-size: 10px;
      opacity: 0.65;
      line-height: 1;
    }
    #workspace-switcher-modal {
      position: fixed;
      inset: 0;
      z-index: 2200;
      background: rgba(0, 0, 0, 0.52);
      backdrop-filter: blur(2px);
      display: flex;
      align-items: flex-start;
      justify-content: center;
      padding-top: 9vh;
    }
    #workspace-switcher-modal[hidden] {
      display: none !important;
    }
    .ws-modal-card {
      width: min(640px, 92vw);
      max-height: 78vh;
      background: var(--bg-elev, var(--bg));
      border: 1px solid var(--border);
      border-radius: 10px;
      box-shadow: 0 20px 50px rgba(0, 0, 0, 0.45);
      display: flex;
      flex-direction: column;
      overflow: hidden;
      color: var(--fg);
      font-family: var(--font-sans, sans-serif);
    }
    .ws-modal-header {
      display: flex;
      align-items: center;
      gap: 10px;
      padding: 11px 14px;
      border-bottom: 1px solid var(--border);
    }
    .ws-modal-header svg {
      color: var(--fg-dim);
      flex-shrink: 0;
    }
    .ws-search-input {
      flex: 1;
      background: transparent;
      border: none;
      outline: none;
      color: var(--fg);
      font-size: 13.5px;
      font-family: var(--font-sans, sans-serif);
    }
    .ws-search-input::placeholder {
      color: var(--fg-faint, var(--fg-dim));
    }
    .ws-count-badge {
      font-size: 11px;
      color: var(--fg-dim);
      font-family: var(--font-mono, monospace);
      padding: 2px 6px;
      border-radius: 4px;
      background: var(--row-hover, rgba(128, 128, 128, 0.1));
    }
    .ws-repo-list {
      flex: 1;
      overflow-y: auto;
      padding: 6px;
      min-height: 160px;
      max-height: 46vh;
    }
    .ws-repo-item {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 10px;
      padding: 8px 10px;
      border-radius: 6px;
      cursor: pointer;
      transition: background 0.08s ease;
    }
    .ws-repo-item:hover,
    .ws-repo-item.is-selected {
      background: var(--row-sel, var(--row-hover, rgba(128, 128, 128, 0.14)));
    }
    .ws-repo-main {
      display: flex;
      flex-direction: column;
      gap: 2px;
      min-width: 0;
      flex: 1;
    }
    .ws-repo-title-row {
      display: flex;
      align-items: center;
      gap: 7px;
      min-width: 0;
    }
    .ws-repo-name {
      font-size: 13px;
      font-weight: 600;
      color: var(--fg);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .ws-repo-path {
      font-size: 11.5px;
      color: var(--fg-dim);
      font-family: var(--font-mono, monospace);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .ws-repo-meta {
      display: flex;
      align-items: center;
      gap: 6px;
      flex-shrink: 0;
    }
    .ws-badge {
      font-size: 10.5px;
      line-height: 1.2;
      padding: 2px 6px;
      border-radius: 4px;
      font-family: var(--font-mono, monospace);
      border: 1px solid var(--border);
      color: var(--fg-dim);
      background: rgba(128, 128, 128, 0.07);
      white-space: nowrap;
    }
    .ws-badge-current {
      color: var(--accent, #58a6ff);
      border-color: var(--accent, #58a6ff);
      background: rgba(88, 166, 255, 0.1);
      font-weight: 600;
    }
    .ws-badge-worktree {
      color: #d29922;
      border-color: rgba(210, 153, 34, 0.45);
      background: rgba(210, 153, 34, 0.1);
    }
    .ws-badge-branch {
      max-width: 150px;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .ws-empty {
      padding: 26px 14px;
      text-align: center;
      color: var(--fg-dim);
      font-size: 12.5px;
    }
    .ws-roots-footer {
      border-top: 1px solid var(--border);
      padding: 10px 14px;
      background: var(--bg);
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    .ws-roots-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      font-size: 11px;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.04em;
      color: var(--fg-dim);
    }
    .ws-roots-chips {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
    }
    .ws-root-chip {
      display: inline-flex;
      align-items: center;
      gap: 5px;
      font-size: 11.5px;
      font-family: var(--font-mono, monospace);
      padding: 2px 7px;
      border-radius: 5px;
      border: 1px solid var(--border);
      background: var(--bg-elev, var(--bg));
      color: var(--fg);
    }
    .ws-root-chip-remove {
      border: none;
      background: transparent;
      color: var(--fg-dim);
      cursor: pointer;
      padding: 0 2px;
      font-size: 13px;
      line-height: 1;
    }
    .ws-root-chip-remove:hover {
      color: #f85149;
    }
    .ws-roots-add-row {
      display: flex;
      align-items: center;
      gap: 6px;
    }
    .ws-root-input {
      flex: 1;
      background: var(--bg-elev, var(--bg));
      border: 1px solid var(--border);
      border-radius: 5px;
      padding: 5px 8px;
      color: var(--fg);
      font-size: 12px;
      font-family: var(--font-mono, monospace);
      outline: none;
    }
    .ws-root-input:focus {
      border-color: var(--accent, #58a6ff);
    }
    .ws-root-add-btn {
      background: var(--bg-elev, var(--bg));
      border: 1px solid var(--border);
      border-radius: 5px;
      padding: 5px 10px;
      color: var(--fg);
      font-size: 12px;
      cursor: pointer;
      white-space: nowrap;
    }
    .ws-root-add-btn:hover {
      border-color: var(--accent, #58a6ff);
      color: var(--accent, #58a6ff);
    }
  `;
  document.head.appendChild(style);
}

function ensureModalDOM() {
  if (modalEl) return;
  injectStyles();

  modalEl = document.createElement('div');
  modalEl.id = 'workspace-switcher-modal';
  modalEl.className = 'ws-modal-backdrop';
  modalEl.hidden = true;
  modalEl.innerHTML = `
    <div class="ws-modal-card" role="dialog" aria-modal="true" aria-label="Switch Workspace Repository">
      <div class="ws-modal-header">
        <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
          <path d="M2 4.5A1.5 1.5 0 0 1 3.5 3h2.8a1.5 1.5 0 0 1 1.14.52L8.5 4.75h4A1.5 1.5 0 0 1 14 6.25v5.75a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 2 12V4.5Z"/>
        </svg>
        <input type="text" id="ws-switcher-input" class="ws-search-input" placeholder="Switch repository, branch, or enter path (/ or ~/...)..." autocomplete="off" spellcheck="false" />
        <span id="ws-switcher-count" class="ws-count-badge">0</span>
      </div>
      <div id="ws-switcher-list" class="ws-repo-list" role="listbox"></div>
      <div id="ws-switcher-roots" class="ws-roots-footer">
        <div class="ws-roots-header">
          <span>Workspace Roots (workspace.roots)</span>
        </div>
        <div id="ws-roots-chips" class="ws-roots-chips"></div>
        <form id="ws-roots-add-form" class="ws-roots-add-row">
          <input type="text" id="ws-add-root-input" class="ws-root-input" placeholder="Add scan root directory (e.g. ~/code or /path/to/repos)" autocomplete="off" spellcheck="false" />
          <button type="submit" class="ws-root-add-btn">+ Add Root</button>
        </form>
      </div>
    </div>
  `;

  document.body.appendChild(modalEl);

  inputEl = modalEl.querySelector('#ws-switcher-input');
  listEl = modalEl.querySelector('#ws-switcher-list');
  rootsChipsEl = modalEl.querySelector('#ws-roots-chips');
  rootInputEl = modalEl.querySelector('#ws-add-root-input');
  countBadgeEl = modalEl.querySelector('#ws-switcher-count');
  const addFormEl = modalEl.querySelector('#ws-roots-add-form');

  modalEl.addEventListener('mousedown', (e) => {
    if (e.target === modalEl) {
      closeWorkspaceSwitcher();
    }
  });

  inputEl.addEventListener('input', () => {
    wsState.selectedIndex = 0;
    renderRepoList();
  });

  inputEl.addEventListener('keydown', (e) => {
    const len = wsState.filteredItems.length;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (len > 0) {
        wsState.selectedIndex = (wsState.selectedIndex + 1) % len;
        updateSelectionHighlight();
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (len > 0) {
        wsState.selectedIndex = (wsState.selectedIndex - 1 + len) % len;
        updateSelectionHighlight();
      }
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const sel = wsState.filteredItems[wsState.selectedIndex];
      if (sel && sel.path) {
        switchWorkspaceTo(sel.path);
      }
    } else if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      closeWorkspaceSwitcher();
    }
  });

  listEl.addEventListener('click', (e) => {
    const row = e.target.closest('.ws-repo-item');
    if (!row) return;
    const targetPath = row.dataset.path;
    if (targetPath) {
      switchWorkspaceTo(targetPath);
    }
  });

  listEl.addEventListener('mousemove', (e) => {
    const row = e.target.closest('.ws-repo-item');
    if (!row) return;
    const idx = Number(row.dataset.index);
    if (!Number.isNaN(idx) && idx !== wsState.selectedIndex) {
      wsState.selectedIndex = idx;
      updateSelectionHighlight(false);
    }
  });

  rootsChipsEl.addEventListener('click', async (e) => {
    const btn = e.target.closest('.ws-root-chip-remove');
    if (!btn) return;
    const rootPath = btn.dataset.root;
    if (!rootPath) return;
    try {
      const data = await apiPostJson('api/workspaces/roots', { remove: rootPath });
      applyWorkspacesData(data);
      showToast('✓', `Removed root ${rootPath}`);
    } catch (err) {
      showToast('!', err.message || 'Failed to remove workspace root');
    }
  });

  addFormEl.addEventListener('submit', async (e) => {
    e.preventDefault();
    const raw = (rootInputEl.value || '').trim();
    if (!raw) return;
    try {
      const data = await apiPostJson('api/workspaces/roots', { add: raw });
      rootInputEl.value = '';
      applyWorkspacesData(data);
      showToast('✓', `Added workspace root ${raw}`);
    } catch (err) {
      showToast('!', err.message || 'Failed to add workspace root');
    }
  });
}

function applyWorkspacesData(data) {
  if (!data) return;
  wsState.current = data.current || '';
  wsState.name = data.name || '';
  wsState.configuredRoots = Array.isArray(data.configuredRoots) ? data.configuredRoots : [];
  wsState.repos = Array.isArray(data.repos) ? data.repos : [];
  renderRootsFooter();
  renderRepoList();
}

function looksLikeFilesystemPath(q) {
  return q.startsWith('/') || q.startsWith('~/') || q === '~' || q.startsWith('./') || q.startsWith('../');
}

function computeFilteredItems() {
  const q = (inputEl?.value || '').trim();
  const qLower = q.toLowerCase();
  const repos = wsState.repos || [];

  let matched = repos;
  if (qLower) {
    matched = repos
      .map((r) => {
        const name = (r.name || '').toLowerCase();
        const path = (r.path || '').toLowerCase();
        const branch = (r.branch || '').toLowerCase();
        const group = (r.rootGroup || '').toLowerCase();
        let score = 0;
        if (name === qLower) score = 100;
        else if (name.startsWith(qLower)) score = 80;
        else if (name.includes(qLower)) score = 60;
        else if (branch.includes(qLower)) score = 45;
        else if (path.includes(qLower)) score = 35;
        else if (group.includes(qLower)) score = 20;
        return { repo: r, score };
      })
      .filter((entry) => entry.score > 0)
      .sort((a, b) => b.score - a.score)
      .map((entry) => entry.repo);
  }

  const items = [...matched];
  if (q && looksLikeFilesystemPath(q)) {
    const exactExists = items.some((r) => r.path === q);
    if (!exactExists) {
      items.unshift({
        name: `Switch to directory: ${q}`,
        path: q,
        rootGroup: 'Direct Path',
        isDirectPath: true,
      });
    }
  }

  return items;
}

function renderRepoList() {
  if (!listEl) return;
  const items = computeFilteredItems();
  wsState.filteredItems = items;
  if (wsState.selectedIndex >= items.length) {
    wsState.selectedIndex = Math.max(0, items.length - 1);
  }

  if (countBadgeEl) {
    countBadgeEl.textContent = `${wsState.repos.length} repo${wsState.repos.length === 1 ? '' : 's'}`;
  }

  if (wsState.loading && items.length === 0) {
    listEl.innerHTML = `<div class="ws-empty">Scanning workspace roots…</div>`;
    return;
  }

  if (items.length === 0) {
    listEl.innerHTML = `<div class="ws-empty">No matching repositories found. Type a path like <code>~/code/my-repo</code> and press Enter, or add a scan root below.</div>`;
    return;
  }

  listEl.innerHTML = items
    .map((r, idx) => {
      const selectedClass = idx === wsState.selectedIndex ? ' is-selected' : '';
      const badges = [];
      if (r.isCurrent) {
        badges.push(`<span class="ws-badge ws-badge-current">Current</span>`);
      }
      if (r.isWorktree) {
        badges.push(`<span class="ws-badge ws-badge-worktree">Worktree</span>`);
      }
      if (r.pinned) {
        badges.push(`<span class="ws-badge">★ Pinned</span>`);
      }
      if (r.branch) {
        badges.push(`<span class="ws-badge ws-badge-branch" title="Branch: ${esc(r.branch)}">⎇ ${esc(r.branch)}</span>`);
      }
      if (r.rootGroup && !r.isCurrent) {
        badges.push(`<span class="ws-badge">${esc(r.rootGroup)}</span>`);
      }

      return `
        <div class="ws-repo-item${selectedClass}" role="option" data-index="${idx}" data-path="${esc(r.path)}">
          <div class="ws-repo-main">
            <div class="ws-repo-title-row">
              <span class="ws-repo-name">${esc(r.name || r.path)}</span>
            </div>
            <div class="ws-repo-path">${esc(r.path)}</div>
          </div>
          <div class="ws-repo-meta">
            ${badges.join('')}
          </div>
        </div>
      `;
    })
    .join('');

  updateSelectionHighlight(true);
}

function updateSelectionHighlight(scrollIntoView = true) {
  if (!listEl) return;
  const rows = listEl.querySelectorAll('.ws-repo-item');
  rows.forEach((row, idx) => {
    const active = idx === wsState.selectedIndex;
    row.classList.toggle('is-selected', active);
    if (active && scrollIntoView) {
      row.scrollIntoView({ block: 'nearest' });
    }
  });
}

function renderRootsFooter() {
  if (!rootsChipsEl) return;
  const roots = wsState.configuredRoots || [];
  if (roots.length === 0) {
    rootsChipsEl.innerHTML = `<span style="font-size:11.5px;color:var(--fg-dim);">No custom roots configured (scanning default ~/code, ~/src, ~/projects, etc.)</span>`;
    return;
  }
  rootsChipsEl.innerHTML = roots
    .map(
      (r) => `
      <span class="ws-root-chip" title="${esc(r)}">
        <span>${esc(r)}</span>
        <button type="button" class="ws-root-chip-remove" data-root="${esc(r)}" title="Remove root ${esc(r)}" aria-label="Remove root ${esc(r)}">×</button>
      </span>
    `
    )
    .join('');
}

export async function fetchWorkspaces() {
  wsState.loading = true;
  try {
    const data = await api('api/workspaces');
    wsState.loading = false;
    applyWorkspacesData(data);
    return data;
  } catch (err) {
    wsState.loading = false;
    renderRepoList();
    throw err;
  }
}

export async function openWorkspaceSwitcher({ focusRoots = false } = {}) {
  ensureModalDOM();
  modalEl.hidden = false;
  wsState.selectedIndex = 0;
  if (inputEl) {
    inputEl.value = '';
    if (!focusRoots) inputEl.focus();
  }
  if (focusRoots && rootInputEl) {
    rootInputEl.focus();
  }
  renderRepoList();
  try {
    await fetchWorkspaces();
  } catch (err) {
    showToast('!', err.message || 'Failed to load workspaces');
  }
}

export function closeWorkspaceSwitcher() {
  if (!modalEl) return;
  modalEl.hidden = true;
}

// Under the workbench (`px0` with no folder), each repo runs as its own child
// at /w/<id>/. A child must not change its own root, or its URL would name
// the wrong repo. Ask the workbench to open the target and go to its page.
async function openInWorkbench(target) {
  const base = S.meta.workbench.endsWith('/') ? S.meta.workbench : S.meta.workbench + '/';
  const res = await fetch(base + 'api/wb/open', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ target }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok || !data.href) throw new Error(data.error || `Could not open ${target}`);
  closeWorkspaceSwitcher();
  window.location.href = data.href;
  return data;
}

export async function switchWorkspaceTo(path) {
  const target = (path || '').trim();
  if (!target || wsState.switching) return null;
  wsState.switching = true;
  try {
    if (S.meta?.workbench) return await openInWorkbench(target);
    const res = await apiPostJson('api/workspaces/switch', { path: target });
    closeWorkspaceSwitcher();

    const newName = res?.name || res?.meta?.name || target.split('/').filter(Boolean).pop() || target;
    const rootNameEl = $('#root-name');
    if (rootNameEl) {
      rootNameEl.textContent = newName;
    }
    if (res?.meta) {
      S.meta = res.meta;
    }
    document.title = `${newName} — px0`;
    showToast('✓', `Switched workspace to ${newName}`);

    if (typeof onSwitchedCallback === 'function') {
      await onSwitchedCallback(res?.meta || S.meta);
    }
    return res;
  } catch (err) {
    showToast('!', err.message || 'Failed to switch workspace');
    throw err;
  } finally {
    wsState.switching = false;
  }
}

// True while this tab has a switch request in flight.
export function isWorkspaceSwitching() {
  return wsState.switching;
}

export function initWorkspaceSwitcher({ onSwitched } = {}) {
  if (typeof onSwitched === 'function') {
    onSwitchedCallback = onSwitched;
  }
  ensureModalDOM();

  const rootNameEl = $('#root-name');
  if (rootNameEl && !rootNameEl.dataset.wsSwitcherBound) {
    rootNameEl.dataset.wsSwitcherBound = '1';
    rootNameEl.classList.add('ws-root-trigger');
    rootNameEl.setAttribute('role', 'button');
    rootNameEl.setAttribute('tabindex', '0');
    rootNameEl.setAttribute('title', withKeys('Switch workspace repository ({Alt+O})'));
    rootNameEl.addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
      if (modalEl && !modalEl.hidden) {
        closeWorkspaceSwitcher();
      } else {
        openWorkspaceSwitcher();
      }
    });
    rootNameEl.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        openWorkspaceSwitcher();
      }
    });
  }
}
