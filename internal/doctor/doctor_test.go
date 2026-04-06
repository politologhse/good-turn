package doctor

import (
	"context"
	"testing"
	"time"

	"github.com/politologhse/good-turn/internal/profile"
)

func TestCheckProfileValid(t *testing.T) {
	gt := profile.Generate("1.2.3.4:56000", "pass", "hy2")
	r := checkProfile(Config{ProfileString: gt})
	if r.Status != Pass {
		t.Errorf("valid profile: %s %s", r.Status, r.Detail)
	}
}

func TestCheckProfileInvalid(t *testing.T) {
	r := checkProfile(Config{ProfileString: "gt://garbage"})
	if r.Status != Fail {
		t.Errorf("invalid profile: %s", r.Status)
	}
}

func TestCheckProfileEmpty(t *testing.T) {
	r := checkProfile(Config{})
	if r.Status != Warn {
		t.Errorf("empty profile: %s", r.Status)
	}
}

func TestCheckProfileMissingPassword(t *testing.T) {
	gt := profile.Generate("1.2.3.4:56000", "", "hy2")
	r := checkProfile(Config{ProfileString: gt})
	if r.Status != Fail {
		t.Errorf("no password: %s %s", r.Status, r.Detail)
	}
}

func TestCheckNoDTLS(t *testing.T) {
	r := checkNoDTLS(Config{NoDTLS: true})
	if r.Status != Warn {
		t.Errorf("no-dtls should warn: %s", r.Status)
	}
	r = checkNoDTLS(Config{NoDTLS: false})
	if r.Status != Pass {
		t.Errorf("dtls should pass: %s", r.Status)
	}
}

func TestCheckSocksPortAvailable(t *testing.T) {
	// Use a random high port that's likely free
	r := checkSocksPort(Config{SocksPort: 19876})
	if r.Status != Pass {
		t.Errorf("free port: %s %s", r.Status, r.Detail)
	}
}

func TestRunReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gt := profile.Generate("127.0.0.1:59999", "pass", "hy2")
	report := Run(ctx, Config{
		ProfileString: gt,
		SocksPort:     19877,
	})

	if report.Platform == "" {
		t.Error("platform should be set")
	}
	if len(report.Checks) < 5 {
		t.Errorf("expected at least 5 checks, got %d", len(report.Checks))
	}

	// Profile should pass
	for _, c := range report.Checks {
		if c.Name == "Profile" && c.Status != Pass {
			t.Errorf("profile check: %s %s", c.Status, c.Detail)
		}
	}
}
