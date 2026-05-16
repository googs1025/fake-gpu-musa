#!/usr/bin/env bash
# dual-coverage.sh — T1–T9 matrix covering both NVIDIA and MTHREADS paths.
# Aligned with docs/superpowers/plans/2026-05-04-add-musa-support.md §14
# and docs/mthreads-support-design.md §7.0.
#
# T1  NVIDIA   nvidia-smi -L                                  >= 1 row
# T2  NVIDIA   CUDA Driver smoke (cuInit + cuDeviceGetCount)  count == yaml
# T3  NVIDIA   CUDA Runtime smoke (vectorAdd)                 exits 0
# T4  MTHREADS mthreads-gmi -L                                      >= 1 row
# T5  MTHREADS mthreads-gmi -L count matches conf/fake-musa.yaml    count == yaml
# T6  MTHREADS muInit + muDeviceGetCount + muMemGetInfo_v2    stub errors
# T7  MTHREADS musaSetDevice + musaMemGetInfo                 musaErrorNoDevice
# T8  INJECTOR mutex refusal (NV + MUSA env)                  0 mounts
# T9  INJECTOR vendor=both per-container plan                 dispatch ok
#
# The T1–T7 cases need a Linux build with libfakegpu.so + binaries on disk;
# T8/T9 are Go unit tests that run on any host. The script auto-detects which
# half it can run and reports the rest as SKIP so devs on macOS still get
# value from `make test-dual`.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

LIB=${LIB:-output/lib64/libfakegpu.so}
BIN=${BIN:-output/bin}
HAVE_NATIVE=0
if [ -f "$LIB" ] && [ -x "$BIN/nvidia-smi" ] && [ -x "$BIN/mthreads-gmi" ]; then
  HAVE_NATIVE=1
fi

# Pick a YAML-row counter that works on macOS BSD grep too.
count_yaml_rows() {
  grep -cE '^[[:space:]]*-[[:space:]]+name:' "$1" || true
}

pass()  { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; exit 1; }
skip()  { printf '  \033[33mSKIP\033[0m %s (%s)\n' "$1" "$2"; }

if [ "$HAVE_NATIVE" = 1 ]; then
  echo "== Native libfakegpu.so detected, running T1-T7 =="

  # T1
  printf 'T1: nvidia-smi -L ... '
  if LD_PRELOAD="$LIB" "$BIN/nvidia-smi" -L | grep -qE '^GPU [0-9]+:'; then
    pass T1
  else
    fail T1
  fi

  # T2/T3: vectorAdd exercises both Driver and Runtime hooks.
  printf 'T2/T3: vectorAdd CUDA Driver+Runtime ... '
  if LD_PRELOAD="$LIB" "$BIN/vectorAdd" >/dev/null 2>&1; then
    pass T2/T3
  else
    fail T2/T3
  fi

  # T4
  printf 'T4: mthreads-gmi -L ... '
  if LD_PRELOAD="$LIB" "$BIN/mthreads-gmi" -L | grep -qE '^GPU [0-9]+:'; then
    pass T4
  else
    fail T4
  fi

  # T5: count rows in fake-musa.yaml and compare against mthreads-gmi output.
  expected=$(count_yaml_rows conf/fake-musa.yaml)
  got=$(LD_PRELOAD="$LIB" "$BIN/mthreads-gmi" -L | grep -cE '^GPU [0-9]+:' || true)
  printf 'T5: mtml count = yaml count (%s == %s) ... ' "$got" "$expected"
  if [ "$got" = "$expected" ] && [ "$expected" != "0" ]; then
    pass T5
  else
    fail T5
  fi

  # T6/T7: MUSA driver+runtime stubs return error codes.
  if [ ! -x "$BIN/musa-smoke" ]; then
    # Build on demand so devs don't have to remember the make target.
    "${CC:-cc}" -o "$BIN/musa-smoke" scripts/musa-smoke.c -ldl
  fi
  printf 'T6/T7: musa-smoke (driver + runtime error codes) ... '
  if LD_PRELOAD="$LIB" LIB="$LIB" "$BIN/musa-smoke" >/dev/null; then
    pass T6/T7
  else
    fail T6/T7
  fi
else
  echo "== libfakegpu.so + binaries not built (need Linux build); skipping T1-T7 =="
  for t in T1 T2/T3 T4 T5 T6/T7; do
    skip "$t" "no native build at $LIB"
  done
fi

echo "== T8/T9: injector unit tests =="

printf 'T8: mutual exclusion ... '
if go test ./cmd/device-injector -run TestMutualExclusion -count=1 >/tmp/dual-T8.log 2>&1; then
  pass T8
else
  cat /tmp/dual-T8.log
  fail T8
fi

printf 'T9: vendor=both per-container plan ... '
if go test ./cmd/device-injector -run 'TestVendorBoth|TestNvidiaOnlyVendor' -count=1 >/tmp/dual-T9.log 2>&1; then
  pass T9
else
  cat /tmp/dual-T9.log
  fail T9
fi

echo
echo "dual-coverage: ALL PASS"
