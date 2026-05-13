# Terminal-Bench evaluation

[Terminal-Bench](https://www.tbench.ai/) (T-Bench) is a task suite plus Docker/tmux harness for comparing terminal agents. Official docs: [installation](https://www.tbench.ai/docs/installation), [first steps](https://www.tbench.ai/docs/first-steps), [leaderboard submission](https://www.tbench.ai/docs/submitting-to-leaderboard).

## Prerequisites

1. **Git** and **Docker** (Engine + CLI) — the harness builds and runs per-task containers. Confirm with `docker version`. **Docker Desktop on Linux:** also run `export DOCKER_HOST="unix://${HOME}/.docker/desktop/docker.sock"` (or whatever URL `docker context inspect` shows for the active context) so the Python `docker` library used by `tb` talks to the **same** daemon as the `docker` / `docker compose` CLI; otherwise you can get `No such container` 404s after a successful compose up.
2. **Python 3.12 or 3.13** — the `tb` CLI from PyPI currently hits annotation errors on **Python 3.14** with Typer. Use [uv](https://docs.astral.sh/uv/) to install a supported runtime: `uv python install 3.12`.
3. An **API key** for your OpenAI-compatible endpoint (passed into the container for the bundled codient agent). Load it from the environment (e.g. `export OPENAI_API_KEY="$(cat ~/.config/secrets/openai.txt)"`); **do not paste keys into shell history, chat, or screenshots** — rotate any key that was exposed. For a **non-default API host**, set **`CODIENT_BASE_URL`** or **`OPENAI_BASE_URL`** on the host to the full OpenAI-compatible base including `/v1` (see [Run with codient](#run-terminal-bench-with-the-codient-cli)); codient’s Go CLI does not read `OPENAI_BASE_URL` itself — the benchmark adapter writes `~/.codient/config.json` `base_url` from those env vars before running `codient`.

## Dataset version (`head` vs pinned)

The registry’s **`terminal-bench-core==head`** clone follows the repo **`main`** branch. Upstream **removed the top-level `tasks/` tree from `main`**, so the harness can fail with:

`FileNotFoundError: ... '/tmp/.../tasks'`

Use a **pinned** dataset until the registry’s `head` target is updated, for example:

`--dataset terminal-bench-core==0.1.1`

That revision uses the `dataset/terminal-bench-core/v0.1.x` branch, which still contains `tasks/` (including `hello-world`). This matches the [leaderboard](https://www.tbench.ai/docs/submitting-to-leaderboard) task set.

## Install the `tb` CLI

From the `codient` repo root (or any directory):

```bash
export PATH="$HOME/.local/bin:$PATH"   # if you use the official uv installer
uv python install 3.12
uv venv .venv-tbench --python 3.12
uv pip install --python .venv-tbench/bin/python terminal-bench
```

Sanity check:

```bash
.venv-tbench/bin/tb --help
.venv-tbench/bin/tb datasets list
```

Alternative: `uv tool install terminal-bench --python 3.12` (installs an isolated `tb` on your PATH).

## Run a smoke task (built-in agent)

Set [LiteLLM-style](https://docs.litellm.ai/docs/providers) model env vars as required by your provider (see `tb run --help`). Example using the reference **Terminus** agent on one task:

```bash
export OPENAI_API_KEY=sk-...
.venv-tbench/bin/tb run \
  --dataset terminal-bench-core==0.1.1 \
  --agent terminus \
  --model openai/gpt-4o \
  --task-id hello-world
```

Results are written under `./runs/` by default (`--output-path` to change).

## Run Terminal-Bench with the **codient** CLI

This repository ships a small harness adapter: [`benchmarks/terminal_bench_codient/agent.py`](../benchmarks/terminal_bench_codient/agent.py). It:

1. Downloads a **Linux amd64** `codient` release from [GitHub Releases](https://github.com/magusoftdev/codient/releases) into the task container (pin with `--agent-kwarg release_tag=vX.Y.Z`).
2. Writes `~/.codient/config.json` in the container from your host env.
3. Runs a single headless turn: `codient -print -yes -force -auto-approve all -workspace .` with the task instruction as `-prompt`.

From the **`codient/`** Go repo directory (path ends in `.../codient/codient`, not the parent workspace folder alone):

```bash
cd ~/dev/codient/codient   # adjust if your clone differs
export OPENAI_API_KEY=...  # or CODIENT_API_KEY; prefer a file/env manager, not pasting into chat
# Optional API base (must include `/v1` path): default https://api.openai.com/v1
# export CODIENT_BASE_URL=https://api.openai.com/v1
# export OPENAI_BASE_URL=https://...  # used if CODIENT_BASE_URL unset (Terminal-Bench adapter only; codient itself reads config.json)

export PYTHONPATH="$(pwd)/benchmarks"
.venv-tbench/bin/tb run \
  --dataset terminal-bench-core==0.1.1 \
  --agent-import-path terminal_bench_codient.agent:CodientTBAgent \
  --model openai/gpt-4o \
  --task-id hello-world
```

Useful `--agent-kwarg` options (see `CodientTBAgent.__init__`):

| Kwarg | Default | Meaning |
|-------|---------|---------|
| `model_name` | `gpt-4o` | Chat model id for `config.json` (low + high tiers). The harness passes `--model` (e.g. `openai/gpt-4o`); the segment after the last `/` is stored (same idea as the Codex adapter). Override with `--agent-kwarg model_name=...` if you need the full provider id inside codient. |
| `release_tag` | `latest` | GitHub release tag for the binary (e.g. `v0.42.0`). |
| `codient_timeout` | `120m` | Per-invocation `-timeout` passed to codient (raised for full-suite runs; override with `-k codient_timeout=60m` for quick smoke). |
| `sandbox` | `off` | Subprocess sandbox flag (`off` avoids nested isolation issues in Docker). |
| `enable_bench_hints` | `true` | When true (default), passes a short **`-system`** hint tuned for graded benchmark tasks (act with tools; tests run after). Set `false` if it hurts a specific workflow. |
| `bench_system` | *(empty)* | If non-empty, used as **`-system`** instead of the default benchmark hint (escape carefully; prefer short plain text). |
| `instruction_prefix` | *(empty)* | Prepended to the task instruction before the optional Jinja prompt template is applied. |
| `disable_intent_heuristic` | `false` | When true, writes **`disable_intent_heuristic: true`** into the container `config.json` so every turn uses the supervisor model for intent (extra latency/cost; can reduce wrong routing on atypical prompts). |

**Note:** Each benchmark task is one **`-print`** invocation (one user turn, possibly many tool rounds inside codient). Very large Terminal-Bench tasks may need a larger `-timeout` and enough model context.

## Strategy: score vs model vs codient vs harness

**Model choice matters a lot.** `gpt-4o` is a reasonable default, but Terminal-Bench includes hard security, ML, and systems tasks. Stronger OpenAI-compatible models (e.g. newer GPT family, high-capacity reasoning models, or Claude-class endpoints via your own `CODIENT_BASE_URL`) usually move accuracy more than small CLI tweaks. Run the same suite with **`--model openai/…`** (or another provider slug) and compare `results.json`.

**Built-in harness agents are a ceiling check.** Running **`--agent terminus`** (or `oracle` for “tests only”) on the same **`--dataset`** / **`--model`** isolates “suite + infra” from “codient-only”. If Terminus also scores low, the bottleneck is often **model + task difficulty**, not codient.

**Codient product gaps** (tooling, orchestration, sandboxing, prompts) absolutely exist on adversarial tasks, but separating them requires A/B runs: same adapter flags, different **`codient` builds** or config, or comparing **`-print`** vs interactive **REPL** on a single failing task.

**Harness limits:** the bundled adapter is one **`-print`** call per task (many internal tool rounds, but a single user directive). It is **not** parity with multi-episode agents like Terminus. Pushing accuracy further may require **product** work (headless multi-turn, benchmark-specific modes) beyond this adapter.

## Full benchmark run (all tasks) and comparing agents

To run the **entire** `terminal-bench-core==0.1.1` suite, **omit `--task-id`** (and `--n-tasks` unless you want a random subset). Use a **dedicated `--output-path`** per agent so results do not overwrite each other.

**Shared setup** (adjust paths; keep the same for every agent you compare):

```bash
cd ~/dev/codient/codient
# Clear any stray T_BENCH_* from the environment (or use a fresh terminal).
for v in $(env | awk -F= '/^T_BENCH_/ {print $1}' | sort -u); do unset "$v"; done 2>/dev/null || true
export DOCKER_HOST="unix://${HOME}/.docker/desktop/docker.sock"   # Docker Desktop on Linux; skip if not using Desktop
export OPENAI_API_KEY=...   # or CODIENT_API_KEY
# API base (OpenAI-compatible `/v1` URL). Optional; default is https://api.openai.com/v1
# export CODIENT_BASE_URL=https://api.openai.com/v1
# export OPENAI_BASE_URL=https://...   # also honored if CODIENT_BASE_URL is unset (adapter only)
```

**Full run with codient (recommended wrapper):** from the repo root, use [`scripts/run-terminal-bench-codient.sh`](../scripts/run-terminal-bench-codient.sh) so you get safer defaults (lower concurrency, longer **global** test/agent timeouts, longer codient `-timeout`):

```bash
chmod +x scripts/run-terminal-bench-codient.sh   # once
./scripts/run-terminal-bench-codient.sh
# or: make terminal-bench-codient ARGS='--task-id hello-world'
```

Environment overrides for the script (all optional): `TB_BIN` (path to `tb`), `TBENCH_MODEL`, `TBENCH_OUTPUT`, `TBENCH_N_CONCURRENT` (default `2`), `TBENCH_GLOBAL_TEST_TIMEOUT_SEC` (default `600`), `TBENCH_GLOBAL_AGENT_TIMEOUT_SEC` (default `720`), `TBENCH_CODIENT_TIMEOUT` (default `120m`), `TBENCH_SUPERVISOR_EVERY_TURN` (set to `1` to pass `disable_intent_heuristic=true`), `TBENCH_NO_BENCH_HINTS` (set to `1` to pass `enable_bench_hints=false`). Extra `tb run` flags can be appended: `./scripts/run-terminal-bench-codient.sh --exclude-task-id security-vulhub-minio`.

**Full run with codient (manual `tb` invocation):**

```bash
export PYTHONPATH="$(pwd)/benchmarks"
~/dev/codient/tbench-venv/bin/tb run \
  --dataset terminal-bench-core==0.1.1 \
  --agent-import-path terminal_bench_codient.agent:CodientTBAgent \
  --model openai/gpt-4o \
  --output-path runs/tb-codient-gpt4o-$(date +%F_%H%M) \
  --n-concurrent 2 \
  --global-test-timeout-sec 600 \
  --global-agent-timeout-sec 720 \
  --agent-kwarg codient_timeout=120m \
  --no-upload-results
```

Tune **`--n-concurrent`** (default is 4 in `tb`; **2 or 1** reduces Docker Hub pulls and port conflicts for multi-service tasks like `security-vulhub-minio`). **`--global-test-timeout-sec`** overrides each task’s `max_test_timeout_sec` in the harness (fixes many **“Test command timed out after 60.0s”** lines for tasks that ship `max_test_timeout_sec: 60` but need longer wall time after agent work). **`--global-agent-timeout-sec`** caps how long the agent phase may run per trial.

**Heavy Compose / image pulls:** before a full sweep, optionally pre-pull images used by failing stacks, e.g. `docker pull vulhub/minio:2023-02-27T18-10-45Z` (see that task’s `docker-compose.yaml` under `~/.cache/terminal-bench/.../security-vulhub-minio/`). If a task is consistently infra-broken on your machine, add **`--exclude-task-id <id>`** (repeatable) so it does not abort a long run.

**Full run with another harness agent** (same dataset and model string for a fair comparison):

```bash
.venv-tbench/bin/tb run \
  --dataset terminal-bench-core==0.1.1 \
  --agent terminus \
  --model openai/gpt-4o \
  --output-path runs/tb-terminus-gpt4o-$(date +%F_%H%M) \
  --n-concurrent 4 \
  --no-upload-results
```

Swap **`--agent terminus`** for any built-in name from `tb run --help` (e.g. `codex`, `claude-code`, `openhands`) where your keys and docs match [LiteLLM providers](https://docs.litellm.ai/docs/providers).

**Comparing results:** each run writes a top-level **`results.json`** under `--output-path` (plus per-task folders). Compare **Resolved trials**, **Accuracy**, and cost/latency from those files, or use the [Terminal-Bench dashboard](https://www.tbench.ai/docs/dashboard) / [leaderboard flow](https://www.tbench.ai/docs/submitting-to-leaderboard) if you opt into uploads.

## Leaderboard / frozen task sets

For comparable numbers to the public leaderboard, keep using **`terminal-bench-core==0.1.1`** (or the equivalent `--dataset` / `--dataset-version` split from the [submission guide](https://www.tbench.ai/docs/submitting-to-leaderboard)). Run `tb datasets list` if you need other registry pins.

## Troubleshooting

| Issue | What to do |
|-------|------------|
| `docker.errors.NotFound` / `No such container: hello-world-1-of-1-...` | **1)** Clear stray `T_BENCH_*` variables (see the `FileNotFoundError` row above). **2)** **Docker Desktop on Linux:** the `docker` CLI follows your **context** (e.g. `desktop-linux` → `unix://~/.docker/desktop/docker.sock`), but the **Python** `docker` SDK used by `tb` often defaults to a **different** socket, so Compose creates the container while `containers.get(...)` hits the wrong engine and returns 404. Fix: `export DOCKER_HOST="unix://${HOME}/.docker/desktop/docker.sock"` (confirm with `docker context inspect` on the active context if yours differs), then rerun `tb`. **3)** Remove stale containers from earlier failed attempts: `docker rm -f` with the name from the error. |
| `ModuleNotFoundError: terminal_bench_codient` | Run from the **`codient/codient`** directory with `export PYTHONPATH="$(pwd)/benchmarks"` (typo `codientnt` or wrong `cd` breaks the path). |
| `TypeError: 'function' object is not subscriptable` when running `tb` | Use Python **3.12 or 3.13**, not 3.14. |
| `docker: command not found` / cannot connect to daemon | Install Docker and ensure your user can run `docker` (e.g. `docker` group, or rootless Docker). |
| `usermod: group 'docker' does not exist` (Snap install) | Create the group, add yourself, fix the socket once the daemon is up: `sudo groupadd -f docker`, `sudo usermod -aG docker "$USER"`, then `sudo chgrp docker /var/run/docker.sock && sudo chmod 660 /var/run/docker.sock`. Open a **new** terminal (or run `newgrp docker`) and test with `docker run --rm hello-world`. Do **not** add `"group": "docker"` to Snap’s `daemon.json` — Snap already passes `--group docker`, and duplicating it prevents `dockerd` from starting. After `snap restart docker` or reboot, the socket may revert to `root:root`; run the `chgrp`/`chmod` line again or automate it with a small systemd oneshot **after** `snap.docker.dockerd.service`. |
| `permission denied` on `unix:///var/run/docker.sock` | Your user is not allowed to use the socket: confirm `groups` includes `docker` and `ls -l /var/run/docker.sock` shows group `docker` with `rw` for group (see previous row for Snap). |
| Agent install fails in the container | Check task logs under `./runs/...`; verify the release has `codient_<version>_linux_amd64.tar.gz` for the requested `release_tag`. |
| API errors inside the container | Confirm `OPENAI_API_KEY` / `CODIENT_API_KEY` on the **host**. For wrong-host / 404-on-chat errors, set **`CODIENT_BASE_URL`** or **`OPENAI_BASE_URL`** (full base URL with `/v1`) so the adapter can write `base_url` into the container `config.json` — codient does not read OpenAI’s URL env vars at runtime, only `config.json`. |
| `docker compose ... up -d` **exit status 1** (e.g. `security-vulhub-minio`) | Multi-service tasks pull several images and bind ports. **Lower `--n-concurrent`**, **`docker pull`** the images from that task’s `docker-compose.yaml`, and retry. Use **`--exclude-task-id`** only if the task is permanently broken on your host. |
| **Test command timed out after 60.0s** | Many tasks set `max_test_timeout_sec: 60` in `task.yaml`. Pass **`--global-test-timeout-sec 600`** (or use [`scripts/run-terminal-bench-codient.sh`](../scripts/run-terminal-bench-codient.sh)) so slow post-agent tests can finish. |
| **No short test summary info** / pytest parse errors | Pytest did not emit a normal summary (tests crashed, hung, or never started). Open the task’s **`panes/post-test.txt`** under the run output tree; fix environment/agent issues for that task; re-run a single **`--task-id`**. |
