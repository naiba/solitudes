package model

import "testing"

func TestDatabasePolicyConfiguration(t *testing.T) {
	defaults, err := (DatabasePoolConfig{}).WithDefaults()
	if err != nil || defaults.MaxOpen != 20 || defaults.MaxIdle != 5 || defaults.LifetimeSeconds != 1800 || defaults.IdleSeconds != 300 {
		t.Fatalf("defaults: %+v, %v", defaults, err)
	}
	for _, c := range []DatabasePoolConfig{{MaxOpen: -1}, {MaxIdle: -1}, {LifetimeSeconds: -1}, {IdleSeconds: -1}, {MaxOpen: 2, MaxIdle: 3}, {LifetimeSeconds: int(1<<63 - 1)}} {
		if _, err := c.WithDefaults(); err == nil {
			t.Errorf("accepted invalid pool %+v", c)
		}
	}
	one, err := (DatabasePoolConfig{MaxOpen: 1}).WithDefaults()
	if err != nil || one.MaxIdle != 1 {
		t.Fatalf("single connection pool: %+v %v", one, err)
	}
	for _, days := range []int{-1, 36501} {
		if err := (&Config{AuditRetentionDays: days}).ValidateDatabasePolicy(); err == nil {
			t.Errorf("accepted retention %d", days)
		}
	}
	for _, days := range []int{0, 1, 90, 36500} {
		if err := (&Config{AuditRetentionDays: days}).ValidateDatabasePolicy(); err != nil {
			t.Errorf("retention %d: %v", days, err)
		}
	}
}
