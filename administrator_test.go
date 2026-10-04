package solitudes

import (
	"os"
	"testing"
)

func TestAdministratorIdentityValidation(t *testing.T) {
	for _, tc := range []struct{ email, name string }{{"", "Admin"}, {"not-an-email", "Admin"}, {"Name <admin@example.test>", "Admin"}, {"admin@example.test", ""}, {"admin@example.test", "Admin\nInjected"}} {
		if _, _, err := administratorIdentity(tc.email, tc.name); err == nil {
			t.Fatalf("invalid setup accepted: %q %q", tc.email, tc.name)
		}
	}
	email, name, err := administratorIdentity(" ADMIN@example.test ", " Admin ")
	if err != nil || email != "admin@example.test" || name != "Admin" {
		t.Fatal("identity normalization failed", email, name, err)
	}
}

func TestConfigurationRejectsRemovedAccountAndRuntimeFields(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("data", 0700); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"user:\n  email: admin@example.test\n", "configfilepath: /unexpected/path\n", "unknown_option: true\n"} {
		if err := os.WriteFile("data/conf.yml", []byte(field), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := newConfig(); err == nil {
			t.Fatalf("unrecognized configuration was accepted: %s", field)
		}
	}
}
