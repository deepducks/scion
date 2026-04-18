#!/bin/bash
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#
# Workflow Local Integration Test Script (T1 smoke tests — Phase 1)
# ================================================================
# Tests the local workflow CLI path: scion workflow validate and
# scion workflow run (delegating to quack on the host PATH).
#
# Pre-requisite: quack must be installed on PATH.
# Install: npm install -g @duckflux/runner   (or use the default Scion agent image)
#
# Usage:
#   ./scripts/workflow-local-integration-test.sh [options]
#
# Options:
#   --skip-build     Skip building the scion binary
#   --skip-cleanup   Don't clean up test artifacts after completion
#   --verbose        Show verbose output
#   --help           Show this help message
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEST_DIR="/tmp/scion-workflow-local-test-$$"
SKIP_BUILD=false
SKIP_CLEANUP=false
VERBOSE=false
SCION=""

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-build)
            SKIP_BUILD=true
            shift
            ;;
        --skip-cleanup)
            SKIP_CLEANUP=true
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        --help)
            head -36 "$0" | tail -31
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# ============================================================================
# Logging
# ============================================================================

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $1"
}

log_section() {
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

# ============================================================================
# Test assertion helpers
# ============================================================================

assert_exit_code() {
    local description="$1"
    local expected_exit="$2"
    shift 2
    TESTS_RUN=$((TESTS_RUN + 1))
    local actual_exit=0
    "$@" > "$TEST_DIR/last_stdout" 2> "$TEST_DIR/last_stderr" || actual_exit=$?
    if [[ "$actual_exit" -eq "$expected_exit" ]]; then
        log_success "$description (exit $actual_exit)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        log_error "$description"
        log_error "  expected exit $expected_exit, got $actual_exit"
        log_error "  stdout: $(cat "$TEST_DIR/last_stdout")"
        log_error "  stderr: $(cat "$TEST_DIR/last_stderr")"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

assert_success() {
    local description="$1"
    shift
    TESTS_RUN=$((TESTS_RUN + 1))
    if "$@" > "$TEST_DIR/last_stdout" 2> "$TEST_DIR/last_stderr"; then
        log_success "$description"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        log_error "$description"
        log_error "  stdout: $(cat "$TEST_DIR/last_stdout")"
        log_error "  stderr: $(cat "$TEST_DIR/last_stderr")"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

assert_failure() {
    local description="$1"
    shift
    TESTS_RUN=$((TESTS_RUN + 1))
    if "$@" > "$TEST_DIR/last_stdout" 2> "$TEST_DIR/last_stderr"; then
        log_error "$description (expected failure but got success)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    else
        log_success "$description"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    fi
}

assert_stdout_contains() {
    local description="$1"
    local expected="$2"
    shift 2
    TESTS_RUN=$((TESTS_RUN + 1))
    local output
    output=$("$@" 2>/dev/null) || true
    if echo "$output" | grep -qF "$expected"; then
        log_success "$description"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        log_error "$description"
        log_error "  expected to contain: $expected"
        log_error "  actual output: $output"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

assert_stderr_contains() {
    local description="$1"
    local expected="$2"
    shift 2
    TESTS_RUN=$((TESTS_RUN + 1))
    local stderr_out
    stderr_out=$("$@" 2>&1 >/dev/null) || true
    if echo "$stderr_out" | grep -qF "$expected"; then
        log_success "$description"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        # Also check combined output (some tools write errors to stdout)
        local combined
        combined=$("$@" 2>&1) || true
        if echo "$combined" | grep -qF "$expected"; then
            log_success "$description"
            TESTS_PASSED=$((TESTS_PASSED + 1))
            return 0
        fi
        log_error "$description"
        log_error "  expected stderr/output to contain: $expected"
        log_error "  actual stderr: $stderr_out"
        log_error "  actual combined: $combined"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

# ============================================================================
# Setup and teardown
# ============================================================================

cleanup() {
    if [[ "$SKIP_CLEANUP" == "false" ]]; then
        rm -rf "$TEST_DIR"
    else
        log_info "Test artifacts preserved in: $TEST_DIR"
    fi
}

trap cleanup EXIT

check_prerequisites() {
    log_section "Checking Prerequisites"

    if ! command -v quack >/dev/null 2>&1; then
        log_warning "quack not found on PATH — SKIPPING local workflow tests"
        log_warning "Install with: npm install -g @duckflux/runner"
        log_warning "Or use the Scion agent image which has quack pre-installed."
        exit 0
    fi
    log_success "quack found at: $(command -v quack)"

    for cmd in go; do
        if ! command -v "$cmd" &> /dev/null; then
            log_error "Required command '$cmd' not found"
            exit 1
        fi
    done
    log_success "Required tools available (go)"
}

build_scion() {
    if [[ "$SKIP_BUILD" == "true" ]]; then
        log_info "Skipping build (--skip-build)"
        SCION="$TEST_DIR/scion"
        return
    fi

    log_section "Building Scion Binary"

    mkdir -p "$TEST_DIR"

    log_info "Building scion from $PROJECT_ROOT..."
    if go build -buildvcs=false -o "$TEST_DIR/scion" "$PROJECT_ROOT/cmd/scion" 2>&1; then
        log_success "Build successful: $TEST_DIR/scion"
    else
        log_error "Build failed"
        exit 1
    fi
    SCION="$TEST_DIR/scion"
}

create_test_fixtures() {
    log_section "Creating Test Fixtures"
    mkdir -p "$TEST_DIR/fixtures"

    # Fixture: valid workflow — single exec step
    cat > "$TEST_DIR/fixtures/hello.duck.yaml" << 'YAML'
flow:
  - type: exec
    run: echo "hello T1"
YAML
    log_success "Created hello.duck.yaml"

    # Fixture: invalid workflow — missing 'flow', has invalid top-level key
    cat > "$TEST_DIR/fixtures/bad.duck.yaml" << 'YAML'
version: 99
badkey: this-is-not-valid
YAML
    log_success "Created bad.duck.yaml (deliberately invalid)"

    # Fixture: workflow with a required input (string passthrough via stdin)
    # Note: exec participants receive the `input` value on stdin when input: is
    # a single string expression. cat echoes that to stdout.
    cat > "$TEST_DIR/fixtures/with-input.duck.yaml" << 'YAML'
inputs:
  name:
    required: true
participants:
  greet:
    type: exec
    run: cat
    input: workflow.inputs.name
flow:
  - greet
YAML
    log_success "Created with-input.duck.yaml"

    # Fixture: workflow with a required input that we will intentionally not supply
    cat > "$TEST_DIR/fixtures/requires-input.duck.yaml" << 'YAML'
inputs:
  name:
    required: true
flow:
  - type: exec
    run: echo hello
YAML
    log_success "Created requires-input.duck.yaml"
}

# ============================================================================
# Phase 1: scion workflow validate
# ============================================================================

test_validate() {
    log_section "Phase 1: scion workflow validate"

    # 1.1 Validate a valid workflow: exit 0, stdout contains "valid"
    TESTS_RUN=$((TESTS_RUN + 1))
    local out
    out=$($SCION workflow validate "$TEST_DIR/fixtures/hello.duck.yaml" 2>&1)
    local exit_code=$?
    if [[ $exit_code -eq 0 ]] && echo "$out" | grep -qi "valid"; then
        log_success "1.1  validate valid workflow: exit 0 and output contains 'valid'"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "1.1  validate valid workflow: exit 0 and output contains 'valid'"
        log_error "  exit code: $exit_code"
        log_error "  output: $out"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # 1.2 Validate an invalid workflow: exit non-zero, stderr/output has useful message
    TESTS_RUN=$((TESTS_RUN + 1))
    local combined_out bad_exit=0
    combined_out=$($SCION workflow validate "$TEST_DIR/fixtures/bad.duck.yaml" 2>&1) || bad_exit=$?
    if [[ $bad_exit -ne 0 ]] && ( echo "$combined_out" | grep -qi "validation\|invalid\|required\|schema\|error" ); then
        log_success "1.2  validate invalid workflow: exit $bad_exit and output has useful message"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "1.2  validate invalid workflow: exit non-zero with useful message"
        log_error "  exit code: $bad_exit"
        log_error "  output: $combined_out"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    log_info "Phase 1 complete"
}

# ============================================================================
# Phase 2: scion workflow run (local, no inputs)
# ============================================================================

test_run_simple() {
    log_section "Phase 2: scion workflow run — simple exec"

    # 2.1 Run trivial workflow: exit 0, stdout contains "hello T1"
    TESTS_RUN=$((TESTS_RUN + 1))
    local out
    out=$($SCION workflow run "$TEST_DIR/fixtures/hello.duck.yaml" 2>/dev/null)
    local exit_code=$?
    if [[ $exit_code -eq 0 ]] && echo "$out" | grep -qF "hello T1"; then
        log_success "2.1  run trivial workflow: exit 0 and stdout contains 'hello T1'"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "2.1  run trivial workflow: exit 0 and stdout contains 'hello T1'"
        log_error "  exit code: $exit_code"
        log_error "  stdout: $out"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    log_info "Phase 2 complete"
}

# ============================================================================
# Phase 3: scion workflow run with --input
# ============================================================================

test_run_with_input() {
    log_section "Phase 3: scion workflow run — with --input"

    # 3.1 Run with --input name=gustavo: stdout contains "gustavo"
    # The with-input.duck.yaml fixture uses a string passthrough (input: workflow.inputs.name)
    # which sends the input value to the exec participant via stdin; cat echoes it.
    TESTS_RUN=$((TESTS_RUN + 1))
    local out
    out=$($SCION workflow run "$TEST_DIR/fixtures/with-input.duck.yaml" --input name=gustavo 2>/dev/null)
    local exit_code=$?
    if [[ $exit_code -eq 0 ]] && echo "$out" | grep -qF "gustavo"; then
        log_success "3.1  run with --input name=gustavo: exit 0 and stdout contains 'gustavo'"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "3.1  run with --input name=gustavo: exit 0 and stdout contains 'gustavo'"
        log_error "  exit code: $exit_code"
        log_error "  stdout: $out"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    log_info "Phase 3 complete"
}

# ============================================================================
# Phase 4: scion workflow run — missing required input
# ============================================================================

test_run_missing_input() {
    log_section "Phase 4: scion workflow run — missing required input"

    # 4.1 Run without required input: exit non-zero (quack returns 1 for validation failure)
    # quack returns exit 1 for "input validation failed" (schema/validation error, not workflow failure).
    # We assert exit != 0 and document the actual code in the log.
    TESTS_RUN=$((TESTS_RUN + 1))
    local combined exit_code=0
    combined=$($SCION workflow run "$TEST_DIR/fixtures/requires-input.duck.yaml" 2>&1) || exit_code=$?
    if [[ $exit_code -ne 0 ]]; then
        log_success "4.1  run with missing required input: exit $exit_code (non-zero = correct)"
        log_info    "     quack exits $exit_code for missing required input (validation error = 1)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "4.1  run with missing required input: expected non-zero exit, got 0"
        log_error "  output: $combined"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # 4.2 The error output must mention the missing input or validation failure
    TESTS_RUN=$((TESTS_RUN + 1))
    if echo "$combined" | grep -qi "missing\|required\|validation\|input"; then
        log_success "4.2  error output mentions missing/required/validation/input"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "4.2  error output must mention missing/required/validation/input"
        log_error "  output: $combined"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    log_info "Phase 4 complete"
}

# ============================================================================
# Main Test Runner
# ============================================================================

run_all_tests() {
    log_section "Scion Workflow Local Integration Test Suite (T1)"
    log_info "Test directory: $TEST_DIR"
    log_info "Project root: $PROJECT_ROOT"

    mkdir -p "$TEST_DIR"

    check_prerequisites
    build_scion
    create_test_fixtures

    test_validate
    test_run_simple
    test_run_with_input
    test_run_missing_input

    # Summary
    log_section "Test Summary"
    echo -e "  Total:  $TESTS_RUN"
    echo -e "  ${GREEN}Passed: $TESTS_PASSED${NC}"
    if [[ $TESTS_FAILED -gt 0 ]]; then
        echo -e "  ${RED}Failed: $TESTS_FAILED${NC}"
    else
        echo -e "  Failed: 0"
    fi
    echo ""

    if [[ $TESTS_FAILED -eq 0 ]]; then
        log_success "All tests passed!"
        return 0
    else
        log_error "$TESTS_FAILED test(s) failed"
        return 1
    fi
}

# Run the tests
run_all_tests
