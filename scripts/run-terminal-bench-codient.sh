#!/usr/bin/env bash
# Run Terminal-Bench (terminal-bench-core 0.1.1) with the codient adapter and
# defaults that reduce common failures: longer test/agent timeouts, lower
# concurrency for heavy multi-service compose tasks, and a longer codient -timeout.
#
# Usage (from anywhere):
#   ./scripts/run-terminal-bench-codient.sh
#   ./scripts/run-terminal-bench-codient.sh --task-id hello-world
#   TB_BIN=~/path/to/tb TBENCH_N_CONCURRENT=1 ./scripts/run-terminal-bench-codient.sh
#   TBENCH_SUPERVISOR_EVERY_TURN=1  → disable intent heuristic in container (more LLM calls; can fix mis-routing)
#   TBENCH_NO_BENCH_HINTS=1         → omit default -system benchmark hint
#
# Requires: OPENAI_API_KEY or CODIENT_API_KEY; Docker; Python tb CLI (see docs/terminal-bench.md).

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -n "${TB_BIN:-}" ]]; then
  :
elif [[ -x "${ROOT}/../tbench-venv/bin/tb" ]]; then
  TB_BIN="${ROOT}/../tbench-venv/bin/tb"
elif [[ -x "${ROOT}/.venv-tbench/bin/tb" ]]; then
  TB_BIN="${ROOT}/.venv-tbench/bin/tb"
else
  TB_BIN="tb"
fi

for v in $(env | awk -F= '/^T_BENCH_/ {print $1}' | sort -u); do
  if [[ -n "${v}" ]]; then
    unset "${v}"
  fi
done 2>/dev/null || true

export PYTHONPATH="${ROOT}/benchmarks"

if [[ -z "${DOCKER_HOST:-}" ]] && [[ -S "${HOME}/.docker/desktop/docker.sock" ]]; then
  export DOCKER_HOST="unix://${HOME}/.docker/desktop/docker.sock"
fi

MODEL="${TBENCH_MODEL:-openai/gpt-5.3-codex}"
SAFE_MODEL="${MODEL//\//-}"
OUT="${TBENCH_OUTPUT:-runs/tb-codient-${SAFE_MODEL}-$(date +%Y-%m-%d_%H%M)}"
NCON="${TBENCH_N_CONCURRENT:-2}"
GTEST="${TBENCH_GLOBAL_TEST_TIMEOUT_SEC:-600}"
GAGENT="${TBENCH_GLOBAL_AGENT_TIMEOUT_SEC:-720}"
CTO="${TBENCH_CODIENT_TIMEOUT:-120m}"

EXTRA=()
if [[ "${TBENCH_SUPERVISOR_EVERY_TURN:-0}" == "1" ]]; then
  EXTRA+=(--agent-kwarg disable_intent_heuristic=true)
fi
if [[ "${TBENCH_NO_BENCH_HINTS:-0}" == "1" ]]; then
  EXTRA+=(--agent-kwarg enable_bench_hints=false)
fi

exec "${TB_BIN}" run \
  --dataset terminal-bench-core==0.1.1 \
  --agent-import-path terminal_bench_codient.agent:CodientTBAgent \
  --model "${MODEL}" \
  --output-path "${OUT}" \
  --n-concurrent "${NCON}" \
  --global-test-timeout-sec "${GTEST}" \
  --global-agent-timeout-sec "${GAGENT}" \
  --agent-kwarg "codient_timeout=${CTO}" \
  "${EXTRA[@]}" \
  --no-upload-results \
  "$@"
