package service

import "testing"

// TestParseRunning checks running, stopped, and unknown states on every supported platform.
// TestParseRunning 检查所有支持平台的运行、停止和未知状态。
func TestParseRunning(t *testing.T) {
	// cases bind actual status formats to the expected running state.
	// cases 将真实状态格式绑定到预期的运行状态。
	cases := []struct {
		name    string
		goos    string
		status  string
		running bool
		fail    bool
	}{
		{"windows-running", "windows", "SERVICE_NAME: VulcanAgentService\n        STATE              : 4  RUNNING\n", true, false},
		{"windows-stopped", "windows", "        STATE              : 1  STOPPED\n", false, false},
		{"windows-pending", "windows", "        STATE              : 2  START_PENDING\n", false, true},
		{"linux-running", "linux", "manager: systemd\nactive: active\nenabled: enabled", true, false},
		{"linux-stopped", "linux", "manager: systemd\nactive: inactive\nenabled: disabled", false, false},
		{"linux-unknown", "linux", "manager: systemd\nactive: unknown", false, true},
		{"mac-running", "darwin", "state = running\n", true, false},
		{"mac-waiting", "darwin", "state = waiting\n", false, false},
		{"mac-not-loaded", "darwin", "manager: launchd\nservice: VulcanAgentService\nloaded: false\nstatus: not-loaded", false, false},
		{"mac-unknown", "darwin", "state = something-else", false, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			running, err := ParseRunning(testCase.goos, testCase.status)
			if (err != nil) != testCase.fail || running != testCase.running {
				t.Fatalf("running=%t error=%v", running, err)
			}
		})
	}
}
