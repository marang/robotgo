//go:build linux

package portalrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostedIdentityIsExact(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	for _, cell := range []string{"remote-desktop", "screencast"} {
		if err := validateHostedIdentity(commit, cell); err != nil {
			t.Fatalf("validateHostedIdentity(%q): %v", cell, err)
		}
	}
	for _, invalid := range []struct {
		commit string
		cell   string
	}{
		{commit: strings.Repeat("A", 40), cell: "remote-desktop"},
		{commit: strings.Repeat("a", 39), cell: "remote-desktop"},
		{commit: strings.Repeat("g", 40), cell: "remote-desktop"},
		{commit: commit, cell: "other"},
	} {
		if err := validateHostedIdentity(
			invalid.commit,
			invalid.cell,
		); err == nil {
			t.Fatalf("invalid hosted identity was accepted: %+v", invalid)
		}
	}
}

func TestHostedRepositoryMustBeExactAndClean(t *testing.T) {
	t.Parallel()
	commit := strings.Repeat("a", 40)
	clean := &scriptedCommandExecutor{
		outputs: []string{commit + "\n", ""},
	}
	if err := validateExactCleanCommit(
		context.Background(),
		clean,
		"/repository",
		commit,
	); err != nil {
		t.Fatalf("validateExactCleanCommit: %v", err)
	}
	if got := clean.calls; len(got) != 2 ||
		got[0][0] != "git" ||
		got[1][0] != "git" ||
		!strings.Contains(strings.Join(got[1], " "), "--untracked-files=all") {
		t.Fatalf("repository validation calls = %v", got)
	}

	for _, outputs := range [][]string{
		{strings.Repeat("b", 40) + "\n"},
		{commit + "\n", "?? private.png\x00"},
	} {
		executor := &scriptedCommandExecutor{outputs: outputs}
		if err := validateExactCleanCommit(
			context.Background(),
			executor,
			"/repository",
			commit,
		); err == nil {
			t.Fatalf("unsafe hosted repository was accepted: %q", outputs)
		}
	}
}

func TestHostedPortalCommandsAreCredentialFreeAndCellSpecific(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		cell    string
		env     string
		pattern string
	}{
		{
			cell:    "remote-desktop",
			env:     "ROBOTGO_REMOTE_DESKTOP_E2E=1",
			pattern: "^TestRemoteDesktopPortalRuntime$",
		},
		{
			cell:    "screencast",
			env:     "ROBOTGO_SCREENCAST_E2E=1",
			pattern: "^TestPipeWireCapturePersistentSessionIntegration$",
		},
	} {
		command := hostedPortalTestCommand(
			portalLaneGNOME,
			test.cell,
			"/run/user/1100/robotgo-portal-consent-"+test.cell+".ready",
		)
		for _, required := range []string{
			"runuser -u robotgo",
			test.env,
			test.pattern,
			"ROBOTGO_PORTAL_CONSENT_READY_FILE=/run/user/1100/",
			"HTTP_PROXY=http://10.0.2.2:3128",
		} {
			if !strings.Contains(command, required) {
				t.Errorf("%s command omits %q", test.cell, required)
			}
		}
		for _, forbidden := range []string{
			"ACTIONS_RUNTIME_TOKEN",
			"GITHUB_TOKEN",
			"gh auth",
			"config.sh",
			"runner-register",
		} {
			if strings.Contains(command, forbidden) {
				t.Errorf("%s command contains %q", test.cell, forbidden)
			}
		}
	}
}

func TestHostedPortalCommandsSelectDesktopLane(t *testing.T) {
	t.Parallel()
	tests := []struct {
		lane      string
		current   string
		session   string
		forbidden string
	}{
		{
			lane:      portalLaneGNOME,
			current:   "XDG_CURRENT_DESKTOP=GNOME",
			session:   "XDG_SESSION_DESKTOP=gnome",
			forbidden: "XDG_CURRENT_DESKTOP=KDE",
		},
		{
			lane:      portalLaneKDE,
			current:   "XDG_CURRENT_DESKTOP=KDE",
			session:   "XDG_SESSION_DESKTOP=plasmawayland",
			forbidden: "XDG_CURRENT_DESKTOP=GNOME",
		},
	}
	for _, test := range tests {
		command := hostedPortalTestCommand(
			test.lane,
			"remote-desktop",
			"/run/user/1100/robotgo-portal-consent-remote-desktop.ready",
		)
		for _, required := range []string{test.current, test.session} {
			if !strings.Contains(command, required) {
				t.Errorf("%s command omits %q", test.lane, required)
			}
		}
		if strings.Contains(command, test.forbidden) {
			t.Errorf("%s command contains %q", test.lane, test.forbidden)
		}
	}
	if command := hostedPortalTestCommand(
		"other",
		"remote-desktop",
		"/run/user/1100/marker",
	); command != "" {
		t.Fatalf("unsupported lane command = %q", command)
	}
}

func TestHostedSourceArchiveMovesOutOfRootBeforeExtraction(t *testing.T) {
	t.Parallel()
	executor := &scriptedCommandExecutor{}
	if err := transferSourceArchive(
		context.Background(),
		executor,
		[]string{"-p", "22222"},
		"/private/source.tar",
		&strings.Builder{},
	); err != nil {
		t.Fatalf("transferSourceArchive: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("source transfer calls = %v", executor.calls)
	}
	extract := executor.calls[1][len(executor.calls[1])-1]
	for _, required := range []string{
		"test ! -e /home/robotgo/robotgo",
		"mv /root/robotgo-source.tar /home/robotgo/robotgo-source.tar",
		"chown robotgo:robotgo /home/robotgo/robotgo-source.tar",
		"runuser -u robotgo -- tar",
		"rm -f /home/robotgo/robotgo-source.tar",
	} {
		if !strings.Contains(extract, required) {
			t.Errorf("source extraction omits %q", required)
		}
	}
	if strings.Contains(
		extract,
		"runuser -u robotgo -- tar --no-same-owner --no-same-permissions -xf /root/",
	) {
		t.Fatal("unprivileged archive extraction still reads through /root")
	}
}

func TestHostedEgressBoundaryRequiresEnabledActiveDropPolicy(t *testing.T) {
	t.Parallel()
	executor := &scriptedCommandExecutor{}
	if err := enforceHostedEgressBoundary(
		context.Background(),
		executor,
		[]string{"-p", "22222"},
		&strings.Builder{},
	); err != nil {
		t.Fatalf("enforceHostedEgressBoundary: %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("egress boundary calls = %v", executor.calls)
	}
	command := executor.calls[0][len(executor.calls[0])-1]
	for _, required := range []string{
		"systemctl is-enabled --quiet robotgo-runner-egress.service",
		"systemctl start robotgo-runner-egress.service",
		"systemctl is-active --quiet robotgo-runner-egress.service",
		"nft --json list chain inet robotgo_runner output",
		`$chain.policy == "drop"`,
	} {
		if !strings.Contains(command, required) {
			t.Errorf("hosted egress enforcement omits %q", required)
		}
	}

	failing := &scriptedCommandExecutor{errors: []error{errors.New("disabled")}}
	if err := enforceHostedEgressBoundary(
		context.Background(),
		failing,
		[]string{"-p", "22222"},
		&strings.Builder{},
	); err == nil {
		t.Fatal("disabled hosted egress boundary was accepted")
	}
}

func TestPortalDialogWaitRejectsEarlyTestExit(t *testing.T) {
	t.Parallel()
	done := make(chan struct{})
	close(done)
	start := time.Now()
	if err := waitForPortalDialog(context.Background(), done); err == nil {
		t.Fatal("early hosted portal test exit was accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("early hosted portal test exit was not detected promptly")
	}
}

func TestPortalFailureStageParserReturnsOnlyAllowlistedMarker(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "portal-test.log")
	if err := os.WriteFile(
		path,
		[]byte(
			"private diagnostic ignored\n"+
				"ROBOTGO_PORTAL_STAGE=open\n"+
				"token=must-not-leak\n"+
				"ROBOTGO_PORTAL_STAGE=capture-2\n",
		),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if got := readPortalFailureStage(path); got != "capture-2" {
		t.Fatalf("failure stage = %q", got)
	}
}
