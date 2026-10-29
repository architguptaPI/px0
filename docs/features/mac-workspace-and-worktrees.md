# Use px0 on a Mac: one workspace for all repos, and Claude worktree review

This guide is for this fork. It covers two features:

1. One px0 window for all your repositories. You switch between them in place.
2. Git worktrees. You open a Claude Desktop session's worktree and see all of its changes against the base branch.

## 1. Build and install

You need Go, Node and Git. Homebrew gives you all three.

```bash
cd ~/Workspace/px0
make build
```

`make build` bundles the web code and builds `./px0`. Then it replaces each `px0` binary that it finds on your `PATH` (for example `~/.local/bin/px0`).

If you have no `px0` on your `PATH` yet, install it to `~/.local/bin`:

```bash
make install
```

Check the result:

```bash
px0 -v
```

### Stop automatic updates

px0 checks the upstream project for new releases. When it finds one, it replaces its own binary. That removes the features of this fork. Always start the fork with `-no-update`. Add this line to `~/.zshrc`:

```bash
alias px0='command px0 -no-update'
```

Then open a new terminal tab, or run `source ~/.zshrc`.

## 2. Start one workspace

Start px0 on any repository. Use the same window for all repositories after that.

```bash
px0 ~/Workspace/px0
```

px0 opens `http://127.0.0.1:7777` in your browser. Keep the terminal tab open. Press Ctrl+C in it to stop px0.

To run px0 without a terminal tab, start it in the background:

```bash
nohup px0 -no-open ~/Workspace/px0 > ~/.px0/server.log 2>&1 &
```

Then open `http://127.0.0.1:7777` yourself. To stop it, run `pkill -x px0`.

## 3. Tell px0 where your repositories are

px0 looks for Git repositories two folder levels down. With no settings, it looks in the parent folder of the current repository and in `~/code`, `~/src`, `~/projects`, `~/work`, `~/repos`, `~/dev`, `~/workspace` and `~/github`.

To set your own folders, do one of these:

- Press Option+O. In the switcher, add a folder in the **Workspace Roots** area.
- Open Settings (Cmd+,), find **Workspace Repository Roots**, and type one folder on each line.
- Edit `~/.px0/settings.json` yourself:

```json
{
  "workspace.roots": "~/Workspace\n~/Workspace/Personal"
}
```

The value is one string. Put a line break (`\n`) or a comma between folders.

## 4. Switch repositories

1. Press Option+O. You can also click the repository name in the top bar, or use the command palette (Cmd+Shift+P) and run **Workspace: Switch Repository…**.
2. Type part of a name, branch or path. Or type a full path that starts with `~/` or `/`.
3. Press Return.

px0 changes the repository in place. The URL and the port stay the same. Other browser tabs on the same px0 also change to the new repository.

## 5. Review a Claude Desktop session

Claude Desktop puts each session in its own worktree, at `<repo>/.claude/worktrees/<name>`. A worktree is a second checkout of the same repository, on its own branch.

1. Open the repository in px0.
2. Press Option+Shift+W. The **Git Worktrees & Claude Branch Review** window lists each worktree. It shows the branch, the number of commits ahead of the base branch, and the number of uncommitted files. Claude sessions have a **Claude** label.
3. Click the session that you want to review.
4. px0 switches to that worktree. Because the branch has commits ahead of `main` or `master`, px0 compares the worktree with the point where it left the base branch (the merge-base). The Git panel shows **vs master**.
5. The Git Changes list now holds every file that the session changed: committed and uncommitted. Open a file to see the diff and the gutter marks.
6. When you finish, press Option+Shift+W again and click the next session, or click the main worktree.

To change what px0 compares against, use the diff scope bar in the Git panel:

- **Working Tree**: changes since the last commit only (`HEAD`).
- **vs master**: all changes on the branch.
- **Custom…**: any branch, tag or commit.

To stop the automatic switch to **vs master**, set this in `~/.px0/settings.json`:

```json
{
  "git.worktreeAutoBaseDiff": false
}
```

## 6. Create and remove worktrees

In the Git Worktrees window:

- **New**: type a branch name. px0 makes the worktree in `~/.px0/worktrees/<repo>/<branch>`. To use a different folder, set `git.worktreeDir`. px0 does not put new worktrees in `.claude/worktrees`, because Claude Desktop owns that folder.
- **Remove**: removes a linked worktree. px0 does not remove the main worktree or the worktree that is open now.
- **Prune** (or **Prune Stale**): removes Git's records of worktrees whose folders no longer exist.

## Shortcuts

| Keys | Action |
| :--- | :--- |
| Option+O | Switch repository |
| Option+Shift+W | Switch or manage worktrees |
| Cmd+Shift+P | Command palette |
| Cmd+P | Go to file |
| Cmd+, | Settings |

## Known limits

- When you switch, px0 indexes the new repository before it replies. A very large folder makes the switch slow.
- Most request handlers do not lock during a switch. A request that runs during a switch can answer once for the old repository. Reload the page if a panel looks wrong after a switch.
- px0 calculates the merge-base when you open a worktree. If the session rebases its branch after that, switch to the worktree again to recalculate it.
