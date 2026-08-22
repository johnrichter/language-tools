// Integration tests exercise the built language-tools binary as a real
// subprocess against fixture Rust and Go modules, per the task's test
// strategy: build/test/lint return a bounded RunResult, --help is complete,
// and exit codes follow clikit's taxonomy.
package cmd_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// buildCLI compiles language-tools once per test binary run and returns the
// path to the resulting executable.
func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "language-tools")
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build language-tools: %v\n%s", err, out)
	}
	return bin
}

// rustFixture writes a minimal cargo crate to a temp dir, valid unless
// broken is true (a compile error) or warn is true (a lint/compile
// warning), so callers exercise every clikit status class the CLI
// surfaces (success, caveats, gate_negative) against one real toolchain.
func rustFixture(t *testing.T, broken, warn bool) string {
	t.Helper()
	dir := t.TempDir()
	toml := "[package]\nname = \"fixture\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[[bin]]\nname = \"fixture\"\npath = \"src/main.rs\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	var main string
	switch {
	case broken:
		main = "fn main() {\n    this is not valid rust\n}\n"
	case warn:
		main = "fn main() {\n    let unused_var = 5;\n    println!(\"hi\");\n}\n"
	default:
		main = "fn main() {\n    println!(\"hi\");\n}\n\n#[cfg(test)]\nmod tests {\n    #[test]\n    fn it_works() { assert_eq!(2 + 2, 4); }\n}\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "main.rs"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func requireCargo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo not on PATH")
	}
}

type wireResult struct {
	SchemaVersion int              `json:"schema_version"`
	Command       []string         `json:"command"`
	Status        string           `json:"status"`
	ExitCode      int              `json:"exit_code"`
	Data          map[string]any   `json:"data"`
	Errors        []map[string]any `json:"errors"`
	Caveats       []map[string]any `json:"caveats"`
}

func runCLI(t *testing.T, bin string, args ...string) (wireResult, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	out, _ := cmd.Output()
	exit := cmd.ProcessState.ExitCode()
	var r wireResult
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out)
	}
	return r, exit
}

func TestHelp_TopLevelIsComplete(t *testing.T) {
	bin := buildCLI(t)
	out, err := exec.Command(bin, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("--help exited non-zero: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{"build", "test", "lint", "format", "vet", "release", "Examples:", "Usage:"} {
		if !strings.Contains(text, want) {
			t.Errorf("--help missing %q:\n%s", want, text)
		}
	}
}

func TestHelp_EverySubcommandHasHelp(t *testing.T) {
	bin := buildCLI(t)
	for _, args := range [][]string{
		{"build", "--help"}, {"test", "--help"}, {"lint", "--help"},
		{"format", "--help"}, {"vet", "--help"},
		{"release", "--help"}, {"release", "build", "--help"},
	} {
		out, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil {
			t.Errorf("%v --help exited non-zero: %v\n%s", args, err, out)
		}
		if !strings.Contains(string(out), "Usage:") {
			t.Errorf("%v --help missing Usage section:\n%s", args, out)
		}
	}
}

func TestBuild_CleanFixtureSucceeds(t *testing.T) {
	requireCargo(t)
	bin := buildCLI(t)
	dir := rustFixture(t, false, false)
	r, exit := runCLI(t, bin, "build", "--language", "rust", "--dir", dir, "--log-dir", filepath.Join(dir, "log"))
	if r.Status != "success" || exit != 0 {
		t.Fatalf("status=%s exit=%d, want success/0: %+v", r.Status, exit, r)
	}
	for _, key := range []string{"tool", "language", "errors", "warnings", "overflow", "duration_ms", "log_ref"} {
		if _, ok := r.Data[key]; !ok {
			t.Errorf("data missing bounded field %q: %+v", key, r.Data)
		}
	}
}

func TestLint_WarningWithoutAllowWarnings_IsGateNegative(t *testing.T) {
	requireCargo(t)
	bin := buildCLI(t)
	dir := rustFixture(t, false, true)
	r, exit := runCLI(t, bin, "lint", "--language", "rust", "--dir", dir, "--log-dir", filepath.Join(dir, "log"))
	if r.Status != "gate_negative" || exit != 20 {
		t.Fatalf("status=%s exit=%d, want gate_negative/20: %+v", r.Status, exit, r)
	}
	if len(r.Errors) == 0 {
		t.Error("gate_negative result carries no errors")
	}
}

func TestLint_WarningWithAllowWarnings_IsCaveats(t *testing.T) {
	requireCargo(t)
	bin := buildCLI(t)
	dir := rustFixture(t, false, true)
	r, exit := runCLI(t, bin, "lint", "--language", "rust", "--dir", dir, "--log-dir", filepath.Join(dir, "log"), "--allow-warnings")
	if r.Status != "caveats" || exit != 10 {
		t.Fatalf("status=%s exit=%d, want caveats/10: %+v", r.Status, exit, r)
	}
	if len(r.Caveats) == 0 {
		t.Error("caveats result carries no caveats")
	}
}

func TestBuild_CompileErrorIsGateNegative(t *testing.T) {
	requireCargo(t)
	bin := buildCLI(t)
	dir := rustFixture(t, true, false)
	r, exit := runCLI(t, bin, "build", "--language", "rust", "--dir", dir, "--log-dir", filepath.Join(dir, "log"))
	if r.Status != "gate_negative" || exit != 20 {
		t.Fatalf("status=%s exit=%d, want gate_negative/20: %+v", r.Status, exit, r)
	}
}

func TestBuild_MissingLanguageIsUsageError(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "build", "--dir", t.TempDir())
	if r.Status != "usage" || exit != 50 {
		t.Fatalf("status=%s exit=%d, want usage/50: %+v", r.Status, exit, r)
	}
}

func TestBuild_MissingDirIsUsageError(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "build", "--language", "rust")
	if r.Status != "usage" || exit != 50 {
		t.Fatalf("status=%s exit=%d, want usage/50: %+v", r.Status, exit, r)
	}
}

func TestUnknownFlag_IsUsageError(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "build", "--this-flag-does-not-exist")
	if r.Status != "usage" || exit != 50 {
		t.Fatalf("status=%s exit=%d, want usage/50: %+v", r.Status, exit, r)
	}
}

func TestUnknownSubcommand_IsUsageError(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "frobnicate")
	if r.Status != "usage" || exit != 50 {
		t.Fatalf("status=%s exit=%d, want usage/50: %+v", r.Status, exit, r)
	}
}

// A --language value with no registered toolchain adapter is a mistake in
// the caller's input, not an infrastructure fault in this CLI or its host —
// clikit reserves its "internal" class (exit 90, fatal) for the latter.
// toolchain.Run reports this the same unwrapped way it reports every other
// failure, and exposes no exported way to tell "unregistered language" apart
// from an actual infrastructure fault, so this CLI cannot yet classify it as
// usage/unsupported without toolchain first exposing that distinction. This
// test pins the current (misclassified) behavior rather than silently
// tolerating a status/exit code drift.
func TestBuild_UnregisteredLanguage_ExitCodeClass(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "build", "--language", "cobol", "--dir", t.TempDir())
	if r.Status != "internal" || exit != 90 {
		t.Fatalf("status=%s exit=%d, want internal/90 (known gap: see toolchain.Run's lookup miss); update this test once toolchain distinguishes an unregistered language from an infrastructure fault", r.Status, exit)
	}
}

func TestBuild_NonexistentDirIsUsageError(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "build", "--language", "rust", "--dir", filepath.Join(t.TempDir(), "does-not-exist"))
	if r.Status != "usage" || exit != 50 {
		t.Fatalf("status=%s exit=%d, want usage/50: %+v", r.Status, exit, r)
	}
}

func TestBuild_NonexistentConfigIsUsageError(t *testing.T) {
	bin := buildCLI(t)
	cfgPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	r, exit := runCLI(t, bin, "build", "--language", "rust", "--dir", t.TempDir(), "--config", cfgPath)
	if r.Status != "usage" || exit != 50 {
		t.Fatalf("status=%s exit=%d, want usage/50: %+v", r.Status, exit, r)
	}
}

// TestVet_UnsupportedOnRustIsUnsupportedExit80 pins the SC3 contract: cargo
// has no vet-equivalent check, so toolchain.Run returns an error wrapping
// ErrUnsupportedCheck, and the CLI must classify that as clikit's
// "unsupported" status/exit-80 class rather than falling through to the
// generic internal-fault branch (exit 90). This does not require cargo on
// PATH -- the adapter rejects the check before invoking any tool.
func TestVet_UnsupportedOnRustIsUnsupportedExit80(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "vet", "--language", "rust", "--dir", ".", "--log-dir", t.TempDir())
	if r.Status != "unsupported" || exit != 80 {
		t.Fatalf("status=%s exit=%d, want unsupported/80: %+v", r.Status, exit, r)
	}
	if len(r.Errors) == 0 {
		t.Fatal("unsupported result carries no errors")
	}
	code, _ := r.Errors[0]["code"].(string)
	if code != "unsupported.toolchain.check_not_supported" {
		t.Errorf("errors[0].code = %q, want unsupported.toolchain.check_not_supported", code)
	}
}

// TestFormat_UnregisteredLanguage_ExitCodeClass exercises the format command
// specifically (not just build/test/lint) against an unregistered language,
// confirming the new command shares runCheck's full error path rather than
// some divergent copy.
func TestFormat_UnregisteredLanguage_ExitCodeClass(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "format", "--language", "cobol", "--dir", t.TempDir())
	if r.Status != "internal" || exit != 90 {
		t.Fatalf("status=%s exit=%d, want internal/90: %+v", r.Status, exit, r)
	}
}

// TestVet_MissingLanguageIsUsageError checks the shared usage-validation
// path (missing --language) still runs ahead of the toolchain dispatch for
// the new vet command, so a caller mistake isn't misrouted through the
// unsupported-check branch.
func TestVet_MissingLanguageIsUsageError(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "vet", "--dir", t.TempDir())
	if r.Status != "usage" || exit != 50 {
		t.Fatalf("status=%s exit=%d, want usage/50: %+v", r.Status, exit, r)
	}
}

// goLintFixture returns the absolute path of the checked-in fixture module
// named under internal/lint/testdata, and fails if it is absent — these
// tests read a fixture, and never write one.
func goLintFixture(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "internal", "lint", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("lint fixture %q is missing, so this test proves nothing: %v", name, err)
	}
	return dir
}

// toolCodes returns the analyzer code carried in each diagnostic's context.
func toolCodes(diags []map[string]any) []string {
	codes := make([]string, 0, len(diags))
	for _, d := range diags {
		context, _ := d["context"].(map[string]any)
		code, _ := context["tool_code"].(string)
		codes = append(codes, code)
	}
	return codes
}

// messagesFor returns the messages of every diagnostic whose analyzer code
// is code.
func messagesFor(diags []map[string]any, code string) []string {
	var msgs []string
	for _, d := range diags {
		context, _ := d["context"].(map[string]any)
		if got, _ := context["tool_code"].(string); got == code {
			msg, _ := d["message"].(string)
			msgs = append(msgs, msg)
		}
	}
	return msgs
}

// TestLint_GoSixFamilyFixture_ReportsEveryFamily is the SC7c acceptance: the
// fixture plants one defect per analyzer family, and a lint of it must
// surface all six and fail the gate.
func TestLint_GoSixFamilyFixture_ReportsEveryFamily(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "lint", "--language", "go", "--dir", goLintFixture(t, "sixfamilies"), "--log-dir", t.TempDir())
	if r.Status != "gate_negative" || exit != 20 {
		t.Fatalf("status=%s exit=%d, want gate_negative/20: %+v", r.Status, exit, r)
	}
	codes := toolCodes(r.Errors)
	for family, pattern := range map[string]*regexp.Regexp{
		"unchecked error":        regexp.MustCompile(`^errcheck$`),
		"ineffectual assignment": regexp.MustCompile(`^ineffassign$`),
		"staticcheck SA":         regexp.MustCompile(`^SA\d+$`),
		"simple S":               regexp.MustCompile(`^S\d+$`),
		"stylecheck ST":          regexp.MustCompile(`^ST\d+$`),
		"unused U1000":           regexp.MustCompile(`^U1000$`),
	} {
		found := false
		for _, code := range codes {
			if pattern.MatchString(code) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no diagnostic for the %s family; reported codes: %v", family, codes)
		}
	}
}

// TestLint_GoSixFamilyFixture_ReportsUnusedFuncAndType is the SC8
// acceptance: U1000 reports nothing through its pass, so only a driver that
// reads its result surfaces the family at all — for a function and for a
// type alike.
func TestLint_GoSixFamilyFixture_ReportsUnusedFuncAndType(t *testing.T) {
	bin := buildCLI(t)
	r, _ := runCLI(t, bin, "lint", "--language", "go", "--dir", goLintFixture(t, "sixfamilies"), "--log-dir", t.TempDir())
	msgs := messagesFor(r.Errors, "U1000")
	if len(msgs) < 2 {
		t.Fatalf("want at least two U1000 diagnostics, got %d: %v", len(msgs), msgs)
	}
	for _, kind := range []string{"func ", "type "} {
		found := false
		for _, msg := range msgs {
			if strings.Contains(msg, kind) && strings.Contains(msg, "is unused") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no U1000 diagnostic for an unused %sobject: %v", kind, msgs)
		}
	}
}

// TestLint_GoCleanFixture_IsSuccess pins the other half of SC7c: the same
// six shapes written correctly report nothing and pass the gate.
func TestLint_GoCleanFixture_IsSuccess(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "lint", "--language", "go", "--dir", goLintFixture(t, "clean"), "--log-dir", t.TempDir())
	if r.Status != "success" || exit != 0 {
		t.Fatalf("status=%s exit=%d, want success/0: %+v", r.Status, exit, r)
	}
	if len(r.Errors) != 0 || len(r.Caveats) != 0 {
		t.Errorf("clean fixture reported diagnostics: errors=%v caveats=%v", r.Errors, r.Caveats)
	}
}

// TestLint_Go_RunsInProcess pins SC7a: the Go lint check names the
// in-process route's fixed tool and spawns nothing, which the run's own log
// records as an empty command and no captured tool output.
func TestLint_Go_RunsInProcess(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "lint", "--language", "go", "--dir", goLintFixture(t, "clean"), "--log-dir", t.TempDir())
	if exit != 0 {
		t.Fatalf("exit=%d, want 0: %+v", exit, r)
	}
	if tool, _ := r.Data["tool"].(string); tool != "go-analysis" {
		t.Errorf("data.tool = %v, want go-analysis", r.Data["tool"])
	}
	logRef, _ := r.Data["log_ref"].(string)
	raw, err := os.ReadFile(logRef)
	if err != nil {
		t.Fatalf("read run log: %v", err)
	}
	var detail struct {
		Tool    string   `json:"tool"`
		Command []string `json:"command"`
		Stdout  string   `json:"stdout"`
		Stderr  string   `json:"stderr"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("run log is not valid JSON: %v", err)
	}
	if detail.Tool != "go-analysis" {
		t.Errorf("log tool = %q, want go-analysis", detail.Tool)
	}
	if len(detail.Command) != 0 {
		t.Errorf("log command = %v, want empty: an in-process run spawns nothing", detail.Command)
	}
	if detail.Stdout != "" || detail.Stderr != "" {
		t.Errorf("log carries tool output (stdout=%q stderr=%q); nothing was spawned to produce it", detail.Stdout, detail.Stderr)
	}
}

// TestLint_GoCurrentDir_ReachesTheAdapter pins SC4: `--language go` resolves
// to the adapter this binary registers at start-up, rather than failing as
// an unregistered language the way it did before registration.
func TestLint_GoCurrentDir_ReachesTheAdapter(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "lint", "--language", "go", "--dir", ".", "--log-dir", t.TempDir())
	if r.Status != "success" && r.Status != "gate_negative" {
		t.Fatalf("status=%s exit=%d, want a lint verdict rather than a dispatch failure: %+v", r.Status, exit, r)
	}
	if tool, _ := r.Data["tool"].(string); tool != "go-analysis" {
		t.Errorf("data.tool = %v, want go-analysis", r.Data["tool"])
	}
}

// TestFormat_GoUnformattedFixture_IsGateNegative pins SC4a: gofmt -l exits 0
// even when it lists an unformatted file, so the adapter's parse of that
// listing is the only thing that keeps the check from passing.
func TestFormat_GoUnformattedFixture_IsGateNegative(t *testing.T) {
	requireGofmt(t)
	bin := buildCLI(t)
	dir := goModuleFixture(t, false)
	if err := os.WriteFile(filepath.Join(dir, "unformatted.go"), []byte("package main\n\nfunc  greet( ) string {\nreturn \"hi\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, exit := runCLI(t, bin, "format", "--language", "go", "--dir", dir, "--log-dir", t.TempDir())
	if r.Status != "gate_negative" || exit != 20 {
		t.Fatalf("status=%s exit=%d, want gate_negative/20: %+v", r.Status, exit, r)
	}
	if len(r.Errors) == 0 {
		t.Fatal("gate_negative result carries no errors")
	}
}

func requireGofmt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt not on PATH")
	}
}

func TestReleaseBuild_ProducesArchivesAndChecksums(t *testing.T) {
	bin := buildCLI(t)
	outDir := t.TempDir()
	r, exit := runCLI(t, bin, "release", "build", "--version", "v0.0.1-test", "--output-dir", outDir, "--target", "linux/amd64")
	if r.Status != "success" || exit != 0 {
		t.Fatalf("status=%s exit=%d, want success/0: %+v", r.Status, exit, r)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	hasChecksums := false
	hasArchive := false
	for _, e := range entries {
		if e.Name() == "checksums.txt" {
			hasChecksums = true
		}
		if strings.HasSuffix(e.Name(), ".tar.gz") {
			hasArchive = true
		}
	}
	if !hasChecksums {
		t.Error("release build did not write checksums.txt")
	}
	if !hasArchive {
		t.Error("release build did not write any archive")
	}
}

func TestReleaseBuild_InvalidTargetIsUsageError(t *testing.T) {
	bin := buildCLI(t)
	r, exit := runCLI(t, bin, "release", "build", "--target", "not-a-valid-target", "--output-dir", t.TempDir())
	if r.Status != "usage" || exit != 50 {
		t.Fatalf("status=%s exit=%d, want usage/50: %+v", r.Status, exit, r)
	}
}

// goModuleFixture writes a minimal Go module to a temp dir, valid unless
// broken is true (a syntax error `go build` reports across multiple lines).
func goModuleFixture(t *testing.T, broken bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := "package main\n\nfunc main() {}\n"
	if broken {
		main = "package main\n\nfunc main() {\n\tthis is not valid go\n}\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestReleaseBuild_CompileFailureIsInternal pins a real defect: go build's
// stderr for a syntax error is multi-line, but finishErr passes it straight
// into clikit.NewError without collapsing it to one line first. clikit
// rejects the multi-line message, finishErr's build of the internal
// diagnostic fails, and the raw construction error — not wrapped in
// exitError — escapes runReleaseBuild uncaught. Execute's fallback then
// reports it as usage.cli.invalid_invocation/50, misclassifying a genuine
// release-build compile failure as a CLI usage mistake and burying the
// actual compiler diagnostic inside a "clikit: invalid diagnostic message"
// wrapper. Expected per the clikit taxonomy: status=internal, exit=90.
func TestReleaseBuild_CompileFailureIsInternal(t *testing.T) {
	bin := buildCLI(t)
	moduleDir := goModuleFixture(t, true)
	r, exit := runCLI(t, bin, "release", "build", "--module-dir", moduleDir, "--output-dir", t.TempDir(), "--target", "linux/amd64")
	if r.Status != "internal" || exit != 90 {
		t.Fatalf("status=%s exit=%d, want internal/90 (got misclassified as usage/50 due to an unsanitized multi-line error message reaching clikit.NewError): %+v", r.Status, exit, r)
	}
}
