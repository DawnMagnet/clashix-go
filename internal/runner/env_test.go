package runner

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestProxyEnv(t *testing.T) {
	got := ProxyEnv(7890)
	want := []string{
		"http_proxy=http://127.0.0.1:7890",
		"HTTP_PROXY=http://127.0.0.1:7890",
		"https_proxy=http://127.0.0.1:7890",
		"HTTPS_PROXY=http://127.0.0.1:7890",
		"all_proxy=socks5h://127.0.0.1:7890",
		"ALL_PROXY=socks5h://127.0.0.1:7890",
		"no_proxy=" + NoProxy,
		"NO_PROXY=" + NoProxy,
	}
	if len(got) != len(want) {
		t.Fatalf("ProxyEnv length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ProxyEnv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMergeEnvReplacesExisting(t *testing.T) {
	tests := []struct {
		name      string
		base      []string
		overrides []string
		want      []string
	}{
		{
			name:      "empty base",
			overrides: []string{"A=1", "a=2"},
			want:      []string{"A=1", "a=2"},
		},
		{
			name:      "lower case replaced",
			base:      []string{"PATH=/bin", "http_proxy=old", "keep=yes"},
			overrides: []string{"http_proxy=new"},
			want:      []string{"PATH=/bin", "keep=yes", "http_proxy=new"},
		},
		{
			name:      "upper case replaced by lower override",
			base:      []string{"HTTP_PROXY=old", "https_proxy=old"},
			overrides: []string{"http_proxy=new", "https_proxy=new"},
			want:      []string{"http_proxy=new", "https_proxy=new"},
		},
		{
			name:      "duplicate base entries all removed",
			base:      []string{"all_proxy=a", "ALL_PROXY=b", "no_proxy=c", "NO_PROXY=d"},
			overrides: []string{"ALL_PROXY=new", "NO_PROXY=new"},
			want:      []string{"ALL_PROXY=new", "NO_PROXY=new"},
		},
		{
			name:      "malformed entries preserved",
			base:      []string{"MALFORMED", "A=1"},
			overrides: []string{"B=2"},
			want:      []string{"MALFORMED", "A=1", "B=2"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MergeEnv(test.base, test.overrides)
			if len(got) != len(test.want) {
				t.Fatalf("MergeEnv = %#v, want %#v", got, test.want)
			}
			for i := range test.want {
				if got[i] != test.want[i] {
					t.Errorf("MergeEnv[%d] = %q, want %q", i, got[i], test.want[i])
				}
			}
		})
	}
}

func TestBuildEnvUsesProxyForPort(t *testing.T) {
	got := BuildEnv([]string{"PATH=/bin", "http_proxy=stale"}, 1234)
	seen := make(map[string]string, len(got))
	for _, entry := range got {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			t.Errorf("duplicate key %q", key)
		}
		seen[key] = value
	}
	for _, key := range []string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY", "all_proxy", "ALL_PROXY", "no_proxy", "NO_PROXY"} {
		if _, exists := seen[key]; !exists {
			t.Errorf("missing proxy variable %q", key)
		}
	}
	for _, key := range []string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY"} {
		if seen[key] != "http://127.0.0.1:1234" {
			t.Errorf("%s = %q", key, seen[key])
		}
	}
	for _, key := range []string{"all_proxy", "ALL_PROXY"} {
		if seen[key] != "socks5h://127.0.0.1:1234" {
			t.Errorf("%s = %q", key, seen[key])
		}
	}
	for _, key := range []string{"no_proxy", "NO_PROXY"} {
		if seen[key] != NoProxy {
			t.Errorf("%s = %q", key, seen[key])
		}
	}
	if seen["PATH"] != "/bin" {
		t.Errorf("unrelated environment was lost: %#v", got)
	}
}

func TestFreePorts(t *testing.T) {
	ports, err := FreePorts(3)
	if err != nil {
		t.Fatalf("FreePorts: %v", err)
	}
	if len(ports) != 3 {
		t.Fatalf("FreePorts returned %d ports, want 3", len(ports))
	}
	seen := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if port <= 0 || port > 65535 {
			t.Errorf("invalid port %d", port)
		}
		if _, duplicate := seen[port]; duplicate {
			t.Errorf("duplicate port %d", port)
		}
		seen[port] = struct{}{}
		listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			t.Fatalf("returned port %d is not bindable: %v", port, err)
		}
		_ = listener.Close()
	}
}

func TestFreePortsZero(t *testing.T) {
	ports, err := FreePorts(0)
	if err != nil {
		t.Fatalf("FreePorts(0): %v", err)
	}
	if len(ports) != 0 {
		t.Fatalf("FreePorts(0) = %v, want empty", ports)
	}
	if _, err := FreePorts(-1); err == nil {
		t.Fatal("FreePorts(-1) succeeded, want error")
	}
}
