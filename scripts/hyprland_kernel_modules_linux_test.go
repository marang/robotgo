package scripts

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestHyprlandHostedKernelModules(t *testing.T) {
	t.Parallel()

	// Execute the actual workflow's package setup, stopping before any host
	// module/device operation. Shell functions replace every external command;
	// the child receives no inherited startup hooks or desktop environment.
	workflow := readWorkflow(t, "../.github/workflows/hyprland-e2e.yml")
	const step = "      - name: Enable one isolated virtual display\n"
	start := strings.Index(workflow, step)
	if start < 0 {
		t.Fatal("missing hosted vkms setup step")
	}
	setup := workflow[start+len(step):]
	const run = "        run: |\n"
	start = strings.Index(setup, run)
	if start < 0 {
		t.Fatal("missing hosted vkms setup commands")
	}
	setup = setup[start+len(run):]
	end := strings.Index(setup, "          sudo modprobe vkms enable_default_device=1\n")
	if end < 0 {
		t.Fatal("missing mandatory vkms module load")
	}
	setup = setup[:end]
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	const mockCommands = `
uname() {
  [[ "$*" == '-r' ]] || return 99
  printf '%s\n' "$ROBOTGO_TEST_KERNEL"
  return "$ROBOTGO_TEST_UNAME_STATUS"
}
sudo() {
  printf '%s\n' "$*"
  case "$*" in
    'apt-get update') return "$ROBOTGO_TEST_UPDATE_STATUS" ;;
    'apt-get install --no-install-recommends -y linux-modules-extra-'*)
      return "$ROBOTGO_TEST_INSTALL_STATUS" ;;
    *) return 99 ;;
  esac
}
`
	for _, test := range []struct {
		name          string
		kernel        string
		unameStatus   string
		updateStatus  string
		installStatus string
		wantOutput    string
		wantFailure   bool
	}{
		{
			name: "old azure runner installs extras", kernel: "6.17.0-1022-azure",
			wantOutput: "apt-get update\napt-get install --no-install-recommends -y linux-modules-extra-6.17.0-1022-azure\nready\n",
		},
		{
			name: "older generic runner installs matching extras", kernel: "6.8.0-88-generic",
			wantOutput: "apt-get update\napt-get install --no-install-recommends -y linux-modules-extra-6.8.0-88-generic\nready\n",
		},
		{
			name: "new azure runner uses merged modules", kernel: "7.0.0-1017-azure",
			installStatus: "100", wantOutput: "ready\n",
		},
		{
			name: "future major uses merged modules", kernel: "10.2.0-azure",
			installStatus: "100", wantOutput: "ready\n",
		},
		{
			name: "release candidate uses merged modules", kernel: "7.1.0-rc1-azure",
			installStatus: "100", wantOutput: "ready\n",
		},
		{
			name: "unknown release is blocking", kernel: "unknown",
			wantOutput: "unsupported hosted kernel release\n", wantFailure: true,
		},
		{
			name:       "empty release is blocking",
			wantOutput: "unsupported hosted kernel release\n", wantFailure: true,
		},
		{
			name: "old runner update error is blocking", kernel: "6.17.0-1022-azure",
			updateStatus: "17", wantOutput: "apt-get update\n", wantFailure: true,
		},
		{
			name: "old runner missing extras is blocking", kernel: "6.17.0-1022-azure",
			installStatus: "100", wantFailure: true,
			wantOutput: "apt-get update\napt-get install --no-install-recommends -y linux-modules-extra-6.17.0-1022-azure\n",
		},
		{
			name: "kernel lookup failure is blocking", kernel: "7.0.0-1017-azure",
			unameStatus: "19", wantFailure: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-c",
				mockCommands+setup+"\nprintf 'ready\\n'\n")
			command.WaitDelay = time.Second
			status := func(value string) string {
				if value == "" {
					return "0"
				}
				return value
			}
			command.Env = []string{
				"PATH=/nonexistent",
				"ROBOTGO_TEST_KERNEL=" + test.kernel,
				"ROBOTGO_TEST_UNAME_STATUS=" + status(test.unameStatus),
				"ROBOTGO_TEST_UPDATE_STATUS=" + status(test.updateStatus),
				"ROBOTGO_TEST_INSTALL_STATUS=" + status(test.installStatus),
			}
			output, err := command.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal(ctx.Err())
			}
			if (err != nil) != test.wantFailure {
				t.Fatalf("setup error = %v, want failure %v; output: %s", err, test.wantFailure, output)
			}
			if string(output) != test.wantOutput {
				t.Fatalf("setup output = %q, want %q", output, test.wantOutput)
			}
		})
	}
}
