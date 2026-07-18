#!/usr/bin/env bash

set -euo pipefail

readonly SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
readonly REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." >/dev/null 2>&1 && pwd)"
readonly DEFAULT_OUTPUT_DIR="${TMPDIR:-/tmp}/robotgo-x11-backend-evidence"
readonly OUTPUT_SENTINEL='.robotgo-x11-evidence-output'
readonly OUTPUT_SENTINEL_VALUE='robotgo-x11-evidence-v1'
readonly NATIVE_BEHAVIOR_PATTERN='^Test(X11BackendBehavioralParity|X11XTestVersionAtLeast|NativeX11.*)$'
readonly PUREGO_BEHAVIOR_PATTERN='^Test(X11BackendBehavioralParity|X11XTestVersionAtLeast|PureGoX11.*)$'
readonly BENCHMARK_PATTERN='^Benchmark(X11InputRuntime|CaptureImgRuntime)$'
readonly NATIVE_IMPLEMENTATION='native-cgo'
readonly PUREGO_IMPLEMENTATION='pure-go'
readonly -a SHARED_BEHAVIOR_TESTS=(
	'TestX11BackendBehavioralParity'
	'TestX11XTestVersionAtLeast'
)
readonly -a NATIVE_ONLY_BEHAVIOR_TESTS=(
	'TestNativeX11UnicodeFailsBeforeMutation'
	'TestNativeX11UnmappedKeyFailsBeforeModifierInput'
	'TestNativeX11TextKeymapFailureIsAtomic'
	'TestNativeX11TextUsesActiveGermanKeymap'
	'TestNativeX11TextRejectsMidStreamKeymapChange'
	'TestNativeX11KeyPressKeepsResolvedKeycodesAcrossKeymapChange'
	'TestNativeX11ForeignHeldKeysNeverReleased'
	'TestNativeX11TextHonorsActiveXKBState'
	'TestNativeX11KeyToggleOwnershipSurvivesKeymapChange'
	'TestNativeX11KeyToggleSharesOnlyOwnedModifiers'
	'TestNativeX11ToggleOwnershipFollowsDisplayLifecycle'
	'TestNativeX11StatefulInputDoesNotSwitchToPortalOnUp'
	'TestNativeX11ExplicitInvalidDisplayNeverFallsBack'
	'TestNativeX11PixelColorOutOfBoundsReturnsError'
	'TestNativeX11DisplayLifecycleConcurrent'
	'TestNativeX11EnvironmentToggleLifecycleConcurrent'
	'TestNativeX11ConfiguredTargetAppliesToWindowAndScale'
	'TestNativeX11ClientCoordinatesThroughReparenting'
	'TestNativeX11DestroyedWindowDoesNotAbort'
)
readonly -a PUREGO_ONLY_BEHAVIOR_TESTS=(
	'TestPureGoX11RejectsImplicitDependencyPortalFallback'
	'TestPureGoX11WindowCapabilitySelection'
	'TestPureGoX11WindowCapabilityRejectsWaylandConflict'
	'TestPureGoX11MultiLayoutConfiguration'
	'TestPureGoX11Capabilities'
	'TestPureGoX11PointerInput'
	'TestPureGoX11KeyboardInput'
	'TestPureGoX11TextReachesDelayedXKBClient'
	'TestPureGoX11CrashRestoresScratchAndOwnedInput'
	'TestPureGoX11ExplicitShiftReachesXKBClient'
	'TestPureGoX11ScratchReservationSkipsPressedEmptyKeycode'
	'TestPureGoX11ScratchCleanupCanRetryAfterForeignRelease'
	'TestPureGoX11RejectsNonModifierBeforeScratchMutation'
	'TestPureGoX11ScratchCleanupCanRetryAfterModifierRestore'
	'TestPureGoX11TextCapacityFailsBeforeInput'
	'TestPureGoX11RejectsScratchReplacementBeforeTextTap'
	'TestPureGoX11ClosePreservesForeignScratchReplacement'
	'TestPureGoX11ClosePreservesForeignModifierScratchReplacement'
	'TestPureGoX11PreservesForeignInputState'
	'TestPureGoX11EventDrainDoesNotStall'
	'TestPureGoX11CloseMainDisplayReconnects'
	'TestPureGoX11SessionSwitchReleasesOwnedInput'
	'TestPureGoX11WindowIntrospectionAndControl'
)
readonly -a EXPECTED_BENCHMARK_NAMES=(
	'BenchmarkCaptureImgRuntime'
	'BenchmarkX11InputRuntime/Location'
	'BenchmarkX11InputRuntime/MoveAbsolute'
	'BenchmarkX11InputRuntime/MoveRelative'
	'BenchmarkX11InputRuntime/ButtonTogglePair'
	'BenchmarkX11InputRuntime/ClickLeft'
	'BenchmarkX11InputRuntime/ScrollVertical1'
	'BenchmarkX11InputRuntime/KeyTogglePairEnter'
	'BenchmarkX11InputRuntime/KeyPressEnter'
	'BenchmarkX11InputRuntime/TypeASCII8'
)
readonly EXPECTED_BENCHMARKS="${#EXPECTED_BENCHMARK_NAMES[@]}"
readonly -a EVIDENCE_ARTIFACTS=(
	behavior-native.txt
	behavior-purego.txt
	metadata.txt
	native.txt
	purego.txt
	run-status.txt
	summary.md.tmp
	summary-table.md
	summary.md
)

usage() {
	printf 'Usage: %s [OUTPUT_DIR]\n' "${0##*/}"
	printf '\n'
	printf 'Builds native-CGO and Pure-Go test binaries, validates their shared X11\n'
	printf 'behavior in one isolated Xvfb, and records balanced benchmark samples.\n'
	printf 'OUTPUT_DIR is exclusively owned by this script. An existing non-empty\n'
	printf 'directory without its evidence sentinel is rejected without modification.\n'
	printf '\n'
	printf 'Environment controls:\n'
	printf '  ROBOTGO_X11_EVIDENCE_COUNT       balanced order cycles (default: 5)\n'
	printf '  ROBOTGO_X11_EVIDENCE_BALANCED    run both orders per cycle, 0 or 1 (default: 1)\n'
	printf '  ROBOTGO_X11_EVIDENCE_BENCHTIME   Go benchmark duration/count (default: 500ms)\n'
	printf '  ROBOTGO_X11_EVIDENCE_BEHAVIOR_CPU GOMAXPROCS for behavior tests (default: 4)\n'
	printf '  ROBOTGO_X11_EVIDENCE_CPU         GOMAXPROCS and -test.cpu (default: 1)\n'
	printf '  ROBOTGO_X11_EVIDENCE_TIMEOUT     per-process timeout (default: 10m)\n'
	printf '  ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY allow non-decision smoke runs, 0 or 1 (default: 0)\n'
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf 'required command not found: %s\n' "$1" >&2
		exit 1
	fi
}

require_positive_integer() {
	local name="$1"
	local value="$2"
	if [[ ! "${value}" =~ ^[1-9][0-9]*$ ]]; then
		printf '%s must be a positive integer, got %q\n' "${name}" "${value}" >&2
		exit 2
	fi
}

source_status() {
	git -C "${REPO_ROOT}" status --short --untracked-files=all
}

source_fingerprint() {
	{
		git -C "${REPO_ROOT}" rev-parse HEAD
		git -C "${REPO_ROOT}" diff --no-ext-diff --binary HEAD --
		while IFS= read -r -d '' path; do
			printf 'untracked:%s\0' "${path}"
			git -C "${REPO_ROOT}" hash-object --no-filters -- "${path}"
		done < <(git -C "${REPO_ROOT}" ls-files --others --exclude-standard -z | LC_ALL=C sort -z)
	} | git hash-object --stdin
}

ignored_build_inputs() {
	git -C "${REPO_ROOT}" ls-files --others --ignored --exclude-standard -- \
		'go.work' 'go.work.sum' 'vendor/**' \
		'*.go' '*.c' '*.cc' '*.cpp' '*.h' '*.m' '*.mm' '*.s' '*.S' \
		'*.syso' '*.a' '*.so' '*.pc'
}

verify_source_state() {
	local stage="$1"
	local current_commit current_fingerprint
	current_commit="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
	if [[ "${current_commit}" != "${ROBOTGO_X11_EVIDENCE_SOURCE_COMMIT}" ]]; then
		printf '%s: git commit changed from %s to %s\n' \
			"${stage}" "${ROBOTGO_X11_EVIDENCE_SOURCE_COMMIT}" "${current_commit}" >&2
		return 1
	fi
	current_fingerprint="$(source_fingerprint)"
	if [[ "${current_fingerprint}" != "${ROBOTGO_X11_EVIDENCE_SOURCE_FINGERPRINT}" ]]; then
		printf '%s: source fingerprint changed from %s to %s\n' \
			"${stage}" "${ROBOTGO_X11_EVIDENCE_SOURCE_FINGERPRINT}" "${current_fingerprint}" >&2
		return 1
	fi
	if [[ "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" == "0" ]]; then
		local current_status
		current_status="$(source_status)"
		if [[ -n "${current_status}" ]]; then
			printf '%s: repository is no longer clean:\n%s\n' "${stage}" "${current_status}" >&2
			return 1
		fi
	fi
}

reset_x11_keymap() {
	setxkbmap -layout us,de
}

record_metadata() {
	local output_dir="$1"
	{
		printf 'timestamp_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		printf 'git_commit=%s\n' "${ROBOTGO_X11_EVIDENCE_SOURCE_COMMIT}"
		printf 'source_fingerprint=%s\n' "${ROBOTGO_X11_EVIDENCE_SOURCE_FINGERPRINT}"
		printf 'allow_dirty=%s\n' "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}"
		if [[ "${ROBOTGO_X11_EVIDENCE_SNAPSHOT_VERIFIED:-0}" == "1" ]]; then
			printf 'source_mode=detached-clean-worktree\n'
		else
			printf 'source_mode=dirty-development-smoke\n'
		fi
		printf 'git_status_begin\n'
		printf '%s\n' "${ROBOTGO_X11_EVIDENCE_SOURCE_STATUS}"
		printf 'git_status_end\n'
		printf 'go_version=%s\n' "$(go version)"
		printf 'go_env_begin\n'
		go env -json GOOS GOARCH GOVERSION GOTOOLCHAIN GOWORK GOFLAGS \
			GOEXPERIMENT GOAMD64 GO386 GOARM GOARM64 GOMIPS GOMIPS64 \
			GOPPC64 GORISCV64 CC CXX AR PKG_CONFIG CGO_CFLAGS CGO_CPPFLAGS \
			CGO_CXXFLAGS CGO_LDFLAGS
		printf 'go_env_end\n'
		printf 'runtime_env_begin\n'
		printf 'GOGC=%s\n' "${GOGC-}"
		printf 'GOMEMLIMIT=%s\n' "${GOMEMLIMIT-}"
		printf 'GODEBUG=%s\n' "${GODEBUG-}"
		printf 'runtime_env_end\n'
		printf 'pkg_config_env_begin\n'
		printf 'PKG_CONFIG_PATH=%s\n' "${PKG_CONFIG_PATH-}"
		printf 'PKG_CONFIG_LIBDIR=%s\n' "${PKG_CONFIG_LIBDIR-}"
		printf 'PKG_CONFIG_SYSROOT_DIR=%s\n' "${PKG_CONFIG_SYSROOT_DIR-}"
		printf 'LD_LIBRARY_PATH=%s\n' "${LD_LIBRARY_PATH-}"
		printf 'pkg_config_env_end\n'
		printf 'x11_pkg_config_begin\n'
		printf 'x11_version=%s\n' "$(pkg-config --modversion x11)"
		printf 'xtst_version=%s\n' "$(pkg-config --modversion xtst)"
		pkg-config --cflags --libs x11 xtst
		printf 'x11_pkg_config_end\n'
		printf 'native_binary_build_info_begin\n'
		go version -m "${ROBOTGO_X11_EVIDENCE_NATIVE_BIN}"
		printf 'native_binary_build_info_end\n'
		printf 'purego_binary_build_info_begin\n'
		go version -m "${ROBOTGO_X11_EVIDENCE_PUREGO_BIN}"
		printf 'purego_binary_build_info_end\n'
		if command -v ldd >/dev/null 2>&1; then
			printf 'native_binary_dependencies_begin\n'
			if ! ldd "${ROBOTGO_X11_EVIDENCE_NATIVE_BIN}" 2>&1; then
				printf 'native_binary_dependencies=unavailable-or-static\n'
			fi
			printf 'native_binary_dependencies_end\n'
		fi
		printf 'cc_version=%s\n' "$("$(go env CC)" --version 2>/dev/null | head -n 1 || printf unknown)"
		printf 'pkg_config_version=%s\n' "$(pkg-config --version 2>/dev/null || printf unknown)"
		printf 'benchmark_cycles=%s\n' "${ROBOTGO_X11_EVIDENCE_COUNT}"
		printf 'benchmark_balanced=%s\n' "${ROBOTGO_X11_EVIDENCE_BALANCED}"
		printf 'benchmark_benchtime=%s\n' "${ROBOTGO_X11_EVIDENCE_BENCHTIME}"
		printf 'behavior_cpu=%s\n' "${ROBOTGO_X11_EVIDENCE_BEHAVIOR_CPU}"
		printf 'benchmark_cpu=%s\n' "${ROBOTGO_X11_EVIDENCE_CPU}"
		printf 'uname=%s\n' "$(uname -srm)"
		if command -v lscpu >/dev/null 2>&1; then
			lscpu
		fi
		printf 'xvfb_path=%s\n' "$(command -v Xvfb)"
		if command -v dpkg-query >/dev/null 2>&1 && dpkg-query -W xvfb >/dev/null 2>&1; then
			dpkg-query -W -f='xvfb_package=${Package} ${Version}\n' xvfb
		elif command -v rpm >/dev/null 2>&1; then
			rpm -q xorg-x11-server-Xvfb 2>/dev/null || printf 'xvfb_package=unknown\n'
		elif command -v pacman >/dev/null 2>&1; then
			pacman -Q xorg-server-xvfb 2>/dev/null || printf 'xvfb_package=unknown\n'
		else
			printf 'xvfb_package=unknown\n'
		fi
		printf 'xtest_requirement=2.2 (enforced by behavioral harness)\n'
		printf 'x11_keymap_begin\n'
		setxkbmap -query
		printf 'x11_keymap_end\n'
	} | sed 's/[[:space:]]*$//' >"${output_dir}/metadata.txt"
}

run_behavior() {
	local implementation="$1"
	local binary="$2"
	local output_file="$3"
	local pattern
	pattern="$(behavior_pattern "${implementation}")"
	reset_x11_keymap
	printf 'Validating %s behavior\n' "${implementation}"
	env \
		GOMAXPROCS="${ROBOTGO_X11_EVIDENCE_BEHAVIOR_CPU}" \
		ROBOTGO_EXPECT_X11_IMPLEMENTATION="${implementation}" \
		ROBOTGO_REQUIRE_X11_INTEGRATION=1 \
		"${binary}" \
		-test.run "${pattern}" \
		-test.count 1 \
		-test.timeout "${ROBOTGO_X11_EVIDENCE_TIMEOUT}" \
		-test.v 2>&1 | tee "${output_file}"
	verify_behavior_results "${implementation}" "${output_file}"
}

expected_behavior_tests() {
	local implementation="$1"
	printf '%s\n' "${SHARED_BEHAVIOR_TESTS[@]}"
	if [[ "${implementation}" == "${NATIVE_IMPLEMENTATION}" ]]; then
		printf '%s\n' "${NATIVE_ONLY_BEHAVIOR_TESTS[@]}"
	else
		printf '%s\n' "${PUREGO_ONLY_BEHAVIOR_TESTS[@]}"
	fi
}

decision_grade_for_run() {
	if [[ "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" != "0" || \
		"${ROBOTGO_X11_EVIDENCE_SNAPSHOT_VERIFIED:-0}" != "1" || \
		"${ROBOTGO_X11_EVIDENCE_BALANCED}" != "1" || \
		"${ROBOTGO_X11_EVIDENCE_COUNT}" -lt 5 ]]; then
		printf '0\n'
		return
	fi

	# Be deliberately conservative: only duration-based samples of at least
	# 500 ms qualify. Other valid Go duration spellings remain useful smoke
	# configurations but are not labeled decision-grade by this harness.
	local benchtime="${ROBOTGO_X11_EVIDENCE_BENCHTIME}"
	if [[ "${benchtime}" =~ ^([0-9]+)ms$ ]] && ((10#${BASH_REMATCH[1]} >= 500)); then
		printf '1\n'
	elif [[ "${benchtime}" =~ ^([0-9]+)(s|m|h)$ ]] && ((10#${BASH_REMATCH[1]} >= 1)); then
		printf '1\n'
	else
		printf '0\n'
	fi
}

behavior_pattern() {
	local implementation="$1"
	if [[ "${implementation}" == "${NATIVE_IMPLEMENTATION}" ]]; then
		printf '%s\n' "${NATIVE_BEHAVIOR_PATTERN}"
	else
		printf '%s\n' "${PUREGO_BEHAVIOR_PATTERN}"
	fi
}

verify_behavior_manifest() {
	local implementation="$1"
	local binary="$2"
	local listed actual expected pattern
	pattern="$(behavior_pattern "${implementation}")"
	if ! listed="$("${binary}" -test.list "${pattern}")"; then
		printf 'failed to list %s behavior tests\n' "${implementation}" >&2
		exit 1
	fi
	actual="$(printf '%s\n' "${listed}" | awk '/^Test/ { print }' | LC_ALL=C sort)"
	expected="$(expected_behavior_tests "${implementation}" | LC_ALL=C sort)"
	if [[ "${actual}" != "${expected}" ]]; then
		printf '%s behavior test manifest mismatch\n' "${implementation}" >&2
		printf 'expected:\n%s\n' "${expected}" >&2
		printf 'actual:\n%s\n' "${actual:-<empty>}" >&2
		exit 1
	fi
}

verify_behavior_results() {
	local implementation="$1"
	local output_file="$2"
	local test_name
	while IFS= read -r test_name; do
		if ! grep -Eq "^--- PASS: ${test_name} \\(.*\\)$" "${output_file}"; then
			printf '%s behavior test did not pass: %s\n' "${implementation}" "${test_name}" >&2
			exit 1
		fi
	done < <(expected_behavior_tests "${implementation}")
}

run_benchmark_sample() {
	local implementation="$1"
	local binary="$2"
	local output_file="$3"
	local sample="$4"
	local order="$5"
	reset_x11_keymap
	printf 'Benchmarking %s (sample %s, order %s)\n' "${implementation}" "${sample}" "${order}"
	printf '# implementation=%s sample=%s order=%s\n' "${implementation}" "${sample}" "${order}" >>"${output_file}"
	env \
		GOMAXPROCS="${ROBOTGO_X11_EVIDENCE_CPU}" \
		ROBOTGO_CAPTURE_BENCHMARK=1 \
		ROBOTGO_EXPECT_X11_IMPLEMENTATION="${implementation}" \
		ROBOTGO_REQUIRE_X11_INTEGRATION=1 \
		ROBOTGO_X11_INPUT_BENCHMARK=1 \
		"${binary}" \
		-test.run '^$' \
		-test.bench "${BENCHMARK_PATTERN}" \
		-test.benchtime "${ROBOTGO_X11_EVIDENCE_BENCHTIME}" \
		-test.count 1 \
		-test.cpu "${ROBOTGO_X11_EVIDENCE_CPU}" \
		-test.timeout "${ROBOTGO_X11_EVIDENCE_TIMEOUT}" 2>&1 | tee -a "${output_file}"
}

run_order() {
	local first="$1"
	local second="$2"
	local sample="$3"
	local order="$4"
	if [[ "${first}" == "${NATIVE_IMPLEMENTATION}" ]]; then
		run_benchmark_sample "${NATIVE_IMPLEMENTATION}" "${ROBOTGO_X11_EVIDENCE_NATIVE_BIN}" "${ROBOTGO_X11_EVIDENCE_OUTPUT}/native.txt" "${sample}" "${order}.1"
	else
		run_benchmark_sample "${PUREGO_IMPLEMENTATION}" "${ROBOTGO_X11_EVIDENCE_PUREGO_BIN}" "${ROBOTGO_X11_EVIDENCE_OUTPUT}/purego.txt" "${sample}" "${order}.1"
	fi
	if [[ "${second}" == "${NATIVE_IMPLEMENTATION}" ]]; then
		run_benchmark_sample "${NATIVE_IMPLEMENTATION}" "${ROBOTGO_X11_EVIDENCE_NATIVE_BIN}" "${ROBOTGO_X11_EVIDENCE_OUTPUT}/native.txt" "${sample}" "${order}.2"
	else
		run_benchmark_sample "${PUREGO_IMPLEMENTATION}" "${ROBOTGO_X11_EVIDENCE_PUREGO_BIN}" "${ROBOTGO_X11_EVIDENCE_OUTPUT}/purego.txt" "${sample}" "${order}.2"
	fi
}

run_inside_xvfb() {
	if [[ -z "${DISPLAY:-}" ]]; then
		printf 'internal Xvfb run has no DISPLAY\n' >&2
		exit 1
	fi
	if [[ ! -x "${ROBOTGO_X11_EVIDENCE_NATIVE_BIN:-}" || ! -x "${ROBOTGO_X11_EVIDENCE_PUREGO_BIN:-}" ]]; then
		printf 'internal Xvfb run is missing test binaries\n' >&2
		exit 1
	fi

	export LC_ALL=C
	unset WAYLAND_DISPLAY XDG_SESSION_TYPE
	unset ROBOTGO_DISABLE_PORTAL ROBOTGO_FORCE_PORTAL ROBOTGO_WAYLAND_BACKEND
	cd -- "${REPO_ROOT}"
	reset_x11_keymap
	verify_behavior_manifest "${NATIVE_IMPLEMENTATION}" "${ROBOTGO_X11_EVIDENCE_NATIVE_BIN}"
	verify_behavior_manifest "${PUREGO_IMPLEMENTATION}" "${ROBOTGO_X11_EVIDENCE_PUREGO_BIN}"
	record_metadata "${ROBOTGO_X11_EVIDENCE_OUTPUT}"
	run_behavior "${NATIVE_IMPLEMENTATION}" "${ROBOTGO_X11_EVIDENCE_NATIVE_BIN}" "${ROBOTGO_X11_EVIDENCE_OUTPUT}/behavior-native.txt"
	run_behavior "${PUREGO_IMPLEMENTATION}" "${ROBOTGO_X11_EVIDENCE_PUREGO_BIN}" "${ROBOTGO_X11_EVIDENCE_OUTPUT}/behavior-purego.txt"

	: >"${ROBOTGO_X11_EVIDENCE_OUTPUT}/native.txt"
	: >"${ROBOTGO_X11_EVIDENCE_OUTPUT}/purego.txt"
	local sample
	for ((sample = 1; sample <= ROBOTGO_X11_EVIDENCE_COUNT; sample++)); do
		if [[ "${ROBOTGO_X11_EVIDENCE_BALANCED}" == "1" ]]; then
			run_order "${NATIVE_IMPLEMENTATION}" "${PUREGO_IMPLEMENTATION}" "${sample}" "native-first"
			run_order "${PUREGO_IMPLEMENTATION}" "${NATIVE_IMPLEMENTATION}" "${sample}" "purego-first"
		elif ((sample % 2 == 1)); then
			run_order "${NATIVE_IMPLEMENTATION}" "${PUREGO_IMPLEMENTATION}" "${sample}" "native-first"
		else
			run_order "${PUREGO_IMPLEMENTATION}" "${NATIVE_IMPLEMENTATION}" "${sample}" "purego-first"
		fi
	done

	local observations_per_implementation="${ROBOTGO_X11_EVIDENCE_COUNT}"
	if [[ "${ROBOTGO_X11_EVIDENCE_BALANCED}" == "1" ]]; then
		observations_per_implementation=$((observations_per_implementation * 2))
	fi
	local benchmark_name
	local -a expected_benchmark_flags=()
	for benchmark_name in "${EXPECTED_BENCHMARK_NAMES[@]}"; do
		expected_benchmark_flags+=("-expected-benchmark" "${benchmark_name}")
	done
	go run ./internal/cmd/benchcompare \
		-baseline "${ROBOTGO_X11_EVIDENCE_OUTPUT}/native.txt" \
		-candidate "${ROBOTGO_X11_EVIDENCE_OUTPUT}/purego.txt" \
		-baseline-label "native CGO" \
		-candidate-label "Pure-Go" \
		-expected-benchmarks "${EXPECTED_BENCHMARKS}" \
		"${expected_benchmark_flags[@]}" \
		-expected-samples "${observations_per_implementation}" \
		-out "${ROBOTGO_X11_EVIDENCE_OUTPUT}/summary-table.md"

	local summary_tmp="${ROBOTGO_X11_EVIDENCE_OUTPUT}/summary.md.tmp"
	local decision_grade
	local balanced_mode="disabled"
	decision_grade="$(decision_grade_for_run)"
	if [[ "${ROBOTGO_X11_EVIDENCE_BALANCED}" == "1" ]]; then
		balanced_mode="enabled"
	fi
	{
		printf '# X11 native CGO vs Pure-Go benchmark report\n\n'
		printf '> Report-only evidence: correctness is blocking; timing ratios never fail CI. '
		printf 'A 1x CI smoke validates this measurement path only and is not decision-grade performance data.\n\n'
		if [[ "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" == "1" ]]; then
			printf '> **Non-decision development smoke:** this run used an explicitly allowed dirty worktree. '
			printf 'Do not commit or use these measurements for a backend decision.\n\n'
		elif [[ "${decision_grade}" != "1" ]]; then
			printf '> **Non-decision configuration:** this clean run did not use the canonical minimum '
			printf 'of five balanced two-order cycles with at least 500ms per benchmark.\n\n'
		fi
		printf -- '- Git commit: `%s`\n' "${ROBOTGO_X11_EVIDENCE_SOURCE_COMMIT}"
		printf -- '- Source fingerprint: `%s`\n' "${ROBOTGO_X11_EVIDENCE_SOURCE_FINGERPRINT}"
		printf -- '- Observations per benchmark and implementation: `%s`\n' "${observations_per_implementation}"
		printf -- '- Go benchtime: `%s`\n' "${ROBOTGO_X11_EVIDENCE_BENCHTIME}"
		printf -- '- GOMAXPROCS / `-test.cpu`: `%s`\n' "${ROBOTGO_X11_EVIDENCE_CPU}"
		printf -- '- Benchmark cycles: `%s`\n' "${ROBOTGO_X11_EVIDENCE_COUNT}"
		printf -- '- Balanced two-order mode: `%s`\n\n' "${balanced_mode}"
		cat "${ROBOTGO_X11_EVIDENCE_OUTPUT}/summary-table.md"
	} >"${summary_tmp}"
	mv -- "${summary_tmp}" "${ROBOTGO_X11_EVIDENCE_OUTPUT}/summary.md"
	rm -f -- "${ROBOTGO_X11_EVIDENCE_OUTPUT}/summary-table.md"
	verify_source_state 'after X11 evidence run'
	printf 'status=complete\ngit_commit=%s\nsource_fingerprint=%s\nallow_dirty=%s\nsnapshot_verified=%s\ndecision_grade=%s\n' \
		"${ROBOTGO_X11_EVIDENCE_SOURCE_COMMIT}" \
		"${ROBOTGO_X11_EVIDENCE_SOURCE_FINGERPRINT}" \
		"${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" \
		"${ROBOTGO_X11_EVIDENCE_SNAPSHOT_VERIFIED:-0}" \
		"${decision_grade}" \
		>"${ROBOTGO_X11_EVIDENCE_OUTPUT}/run-status.txt"
	printf 'X11 backend evidence written to %s\n' "${ROBOTGO_X11_EVIDENCE_OUTPUT}"
}

if [[ "${1:-}" == "--inside-xvfb" ]]; then
	shift
	if (($# != 0)); then
		usage >&2
		exit 2
	fi
	run_inside_xvfb
	exit 0
fi

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
	usage
	exit 0
fi
if (($# > 1)); then
	usage >&2
	exit 2
fi

output_dir="${1:-${DEFAULT_OUTPUT_DIR}}"
mkdir -p -- "${output_dir}"
export ROBOTGO_X11_EVIDENCE_OUTPUT="$(cd -- "${output_dir}" >/dev/null 2>&1 && pwd -P)"
lock_owned=0
snapshot_parent=''
snapshot_dir=''
binary_dir=''
lock_dir="${ROBOTGO_X11_EVIDENCE_OUTPUT}.lock"
lock_owner_file="${lock_dir}/owner"
lock_token="${ROBOTGO_X11_EVIDENCE_LOCK_TOKEN:-}"

cleanup_evidence_run() {
	if [[ -n "${snapshot_dir}" ]]; then
		git -C "${REPO_ROOT}" worktree remove --force "${snapshot_dir}" >/dev/null 2>&1 || true
	fi
	if [[ -n "${snapshot_parent}" ]]; then
		rm -rf -- "${snapshot_parent}"
	fi
	if [[ -n "${binary_dir}" ]]; then
		rm -rf -- "${binary_dir}"
	fi
	if [[ "${lock_owned}" == "1" && -f "${lock_owner_file}" ]]; then
		IFS= read -r current_lock_token <"${lock_owner_file}" || current_lock_token=''
		if [[ "${current_lock_token}" == "${lock_token}" ]]; then
			rm -rf -- "${lock_dir}"
		fi
	fi
}

if [[ -n "${lock_token}" ]]; then
	# Only the recursively invoked detached-worktree process may inherit the
	# outer process's lock. Both the canonical source root and per-run owner
	# token must match the live lock directory.
	inherited_snapshot_root="${ROBOTGO_X11_EVIDENCE_SNAPSHOT_ROOT:-}"
	if [[ -z "${inherited_snapshot_root}" ]]; then
		printf 'invalid inherited X11 evidence output lock\n' >&2
		exit 1
	fi
	inherited_snapshot_root="$(cd -- "${inherited_snapshot_root}" >/dev/null 2>&1 && pwd -P)"
	IFS= read -r current_lock_token <"${lock_owner_file}" || current_lock_token=''
	if [[ "${ROBOTGO_X11_EVIDENCE_LOCK_DIR:-}" != "${lock_dir}" || \
		"${REPO_ROOT}" != "${inherited_snapshot_root}" || \
		"${current_lock_token}" != "${lock_token}" ]]; then
		printf 'invalid inherited X11 evidence output lock\n' >&2
		exit 1
	fi
else
	lock_token="${BASHPID}-${RANDOM}-${RANDOM}"
	if ! mkdir -- "${lock_dir}" 2>/dev/null; then
		printf 'evidence output is locked by another or interrupted run: %s\n' \
			"${ROBOTGO_X11_EVIDENCE_OUTPUT}" >&2
		printf 'remove %s only after confirming that no evidence run is active\n' "${lock_dir}" >&2
		exit 1
	fi
	printf '%s\n' "${lock_token}" >"${lock_owner_file}"
	lock_owned=1
	export ROBOTGO_X11_EVIDENCE_LOCK_TOKEN="${lock_token}"
	export ROBOTGO_X11_EVIDENCE_LOCK_DIR="${lock_dir}"
fi
trap cleanup_evidence_run EXIT

sentinel_path="${ROBOTGO_X11_EVIDENCE_OUTPUT}/${OUTPUT_SENTINEL}"
if [[ -e "${sentinel_path}" ]]; then
	if [[ ! -f "${sentinel_path}" || -L "${sentinel_path}" ]]; then
		printf 'evidence output sentinel is not a regular file: %s\n' "${sentinel_path}" >&2
		exit 1
	fi
	IFS= read -r sentinel_value <"${sentinel_path}" || sentinel_value=''
	if [[ "${sentinel_value}" != "${OUTPUT_SENTINEL_VALUE}" ]]; then
		printf 'evidence output sentinel has an unknown value: %s\n' "${sentinel_path}" >&2
		exit 1
	fi
else
	shopt -s nullglob dotglob
	existing_output_entries=("${ROBOTGO_X11_EVIDENCE_OUTPUT}"/*)
	shopt -u nullglob dotglob
	if ((${#existing_output_entries[@]} != 0)); then
		printf 'refusing non-empty output directory not owned by this script: %s\n' \
			"${ROBOTGO_X11_EVIDENCE_OUTPUT}" >&2
		printf 'choose a new or empty directory; no existing files were modified\n' >&2
		exit 1
	fi
	printf '%s\n' "${OUTPUT_SENTINEL_VALUE}" >"${sentinel_path}"
fi
for artifact in "${EVIDENCE_ARTIFACTS[@]}"; do
	rm -f -- "${ROBOTGO_X11_EVIDENCE_OUTPUT}/${artifact}"
done
# Invalidate any stale successful run before configuration, dependency,
# repository, or source preflight can fail. The commit is filled in after Git
# becomes available.
printf 'status=incomplete\ngit_commit=unknown\n' \
	>"${ROBOTGO_X11_EVIDENCE_OUTPUT}/run-status.txt"

export ROBOTGO_X11_EVIDENCE_COUNT="${ROBOTGO_X11_EVIDENCE_COUNT:-5}"
export ROBOTGO_X11_EVIDENCE_BALANCED="${ROBOTGO_X11_EVIDENCE_BALANCED:-1}"
export ROBOTGO_X11_EVIDENCE_BENCHTIME="${ROBOTGO_X11_EVIDENCE_BENCHTIME:-500ms}"
export ROBOTGO_X11_EVIDENCE_BEHAVIOR_CPU="${ROBOTGO_X11_EVIDENCE_BEHAVIOR_CPU:-4}"
export ROBOTGO_X11_EVIDENCE_CPU="${ROBOTGO_X11_EVIDENCE_CPU:-1}"
export ROBOTGO_X11_EVIDENCE_TIMEOUT="${ROBOTGO_X11_EVIDENCE_TIMEOUT:-10m}"
export ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY="${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY:-0}"
require_positive_integer ROBOTGO_X11_EVIDENCE_COUNT "${ROBOTGO_X11_EVIDENCE_COUNT}"
require_positive_integer ROBOTGO_X11_EVIDENCE_BEHAVIOR_CPU "${ROBOTGO_X11_EVIDENCE_BEHAVIOR_CPU}"
require_positive_integer ROBOTGO_X11_EVIDENCE_CPU "${ROBOTGO_X11_EVIDENCE_CPU}"
if [[ "${ROBOTGO_X11_EVIDENCE_BALANCED}" != "0" && "${ROBOTGO_X11_EVIDENCE_BALANCED}" != "1" ]]; then
	printf 'ROBOTGO_X11_EVIDENCE_BALANCED must be 0 or 1, got %q\n' "${ROBOTGO_X11_EVIDENCE_BALANCED}" >&2
	exit 2
fi
if [[ "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" != "0" && "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" != "1" ]]; then
	printf 'ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY must be 0 or 1, got %q\n' "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" >&2
	exit 2
fi

require_command go
require_command git
require_command xvfb-run
require_command Xvfb
require_command setxkbmap
require_command xkbcomp
require_command tee
require_command pkg-config

# A workspace or ambient GOFLAGS can silently select different modules,
# vendored inputs, tags, or compiler behavior. Evidence always uses the module
# at the recorded commit and refuses dependency-file mutation.
export GOWORK=off
export GOFLAGS=-mod=readonly
export PKG_CONFIG=pkg-config
# Runtime knobs materially change timing and allocation behavior. Use one
# explicit baseline rather than inheriting developer- or runner-specific values.
export GOGC=100
export GOMEMLIMIT=off
export GODEBUG=

export ROBOTGO_X11_EVIDENCE_SOURCE_COMMIT="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
export ROBOTGO_X11_EVIDENCE_SOURCE_STATUS="$(source_status)"
export ROBOTGO_X11_EVIDENCE_SOURCE_FINGERPRINT="$(source_fingerprint)"
printf 'status=incomplete\ngit_commit=%s\nsource_fingerprint=%s\nallow_dirty=%s\n' \
	"${ROBOTGO_X11_EVIDENCE_SOURCE_COMMIT}" \
	"${ROBOTGO_X11_EVIDENCE_SOURCE_FINGERPRINT}" \
	"${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" \
	>"${ROBOTGO_X11_EVIDENCE_OUTPUT}/run-status.txt"
if [[ -n "${ROBOTGO_X11_EVIDENCE_SOURCE_STATUS}" && "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" == "0" ]]; then
	printf 'X11 decision evidence requires a clean repository; current status:\n%s\n' \
		"${ROBOTGO_X11_EVIDENCE_SOURCE_STATUS}" >&2
	printf 'Set ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY=1 only for non-decision development smoke runs.\n' >&2
	exit 1
fi
ignored_inputs="$(ignored_build_inputs)"
if [[ -n "${ignored_inputs}" ]]; then
	printf 'X11 evidence rejects ignored build inputs that could change the compiled source:\n%s\n' \
		"${ignored_inputs}" >&2
	exit 1
fi
verify_source_state 'before X11 evidence build'

# Decision evidence is always built from an isolated detached worktree. This
# excludes ignored/untracked package inputs and keeps the measured source fixed
# even if the caller's checkout changes while benchmarks run. Dirty mode is an
# explicitly non-decision smoke and instead uses the fingerprint checks above.
export ROBOTGO_X11_EVIDENCE_SNAPSHOT_VERIFIED=0
snapshot_root="${ROBOTGO_X11_EVIDENCE_SNAPSHOT_ROOT:-}"
if [[ -n "${snapshot_root}" ]]; then
	snapshot_root="$(cd -- "${snapshot_root}" >/dev/null 2>&1 && pwd -P)"
	if [[ "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" != "0" || \
		"${REPO_ROOT}" != "${snapshot_root}" || ! -f "${REPO_ROOT}/.git" || \
		-n "$(git -C "${REPO_ROOT}" symbolic-ref -q HEAD || true)" ]]; then
		printf 'invalid X11 evidence snapshot context for %s\n' "${REPO_ROOT}" >&2
		exit 1
	fi
	export ROBOTGO_X11_EVIDENCE_SNAPSHOT_VERIFIED=1
fi
if [[ "${ROBOTGO_X11_EVIDENCE_ALLOW_DIRTY}" == "0" && \
	"${ROBOTGO_X11_EVIDENCE_SNAPSHOT_VERIFIED}" != "1" ]]; then
	snapshot_parent="$(mktemp -d "${TMPDIR:-/tmp}/robotgo-x11-source.XXXXXX")"
	snapshot_dir="${snapshot_parent}/source"
	git -C "${REPO_ROOT}" worktree add --detach "${snapshot_dir}" \
		"${ROBOTGO_X11_EVIDENCE_SOURCE_COMMIT}" >/dev/null
	ROBOTGO_X11_EVIDENCE_SNAPSHOT_ROOT="${snapshot_dir}" \
		bash "${snapshot_dir}/scripts/benchmark-x11-backends.sh" \
		"${ROBOTGO_X11_EVIDENCE_OUTPUT}"
	exit $?
fi

binary_dir="$(mktemp -d "${TMPDIR:-/tmp}/robotgo-x11-evidence.XXXXXX")"
export ROBOTGO_X11_EVIDENCE_NATIVE_BIN="${binary_dir}/robotgo-native.test"
export ROBOTGO_X11_EVIDENCE_PUREGO_BIN="${binary_dir}/robotgo-purego.test"

cd -- "${REPO_ROOT}"
printf 'Verifying module cache against go.sum\n'
go mod verify
verify_source_state 'after module verification'
printf 'Building native CGO X11 test binary\n'
CGO_ENABLED=1 go test -c -tags x11integration -o "${ROBOTGO_X11_EVIDENCE_NATIVE_BIN}" .
verify_source_state 'after native X11 evidence build'
printf 'Building Pure-Go X11 test binary\n'
CGO_ENABLED=0 go test -c -tags x11integration -o "${ROBOTGO_X11_EVIDENCE_PUREGO_BIN}" .
verify_source_state 'after Pure-Go X11 evidence build'

xvfb-run -a -s '-screen 0 1280x720x24 -nolisten tcp -noreset' \
	"${BASH_SOURCE[0]}" --inside-xvfb
