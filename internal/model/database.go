package model

import (
	"fmt"
	"time"
)

// DatabasePoolConfig bounds PostgreSQL connections per application process.
type DatabasePoolConfig struct {
	MaxOpen         int `yaml:"max_open"`
	MaxIdle         int `yaml:"max_idle"`
	LifetimeSeconds int `yaml:"lifetime_seconds"`
	IdleSeconds     int `yaml:"idle_seconds"`
}

func (c DatabasePoolConfig) WithDefaults() (DatabasePoolConfig, error) {
	if c.MaxOpen < 0 || c.MaxIdle < 0 || c.LifetimeSeconds < 0 || c.IdleSeconds < 0 {
		return c, fmt.Errorf("database pool values must not be negative")
	}
	if c.MaxOpen == 0 {
		c.MaxOpen = 20
	}
	if c.MaxIdle == 0 {
		c.MaxIdle = min(5, c.MaxOpen)
	}
	if c.LifetimeSeconds == 0 {
		c.LifetimeSeconds = 1800
	}
	if c.IdleSeconds == 0 {
		c.IdleSeconds = 300
	}
	if c.MaxIdle > c.MaxOpen {
		return c, fmt.Errorf("database max_idle must not exceed max_open")
	}
	if int64(c.LifetimeSeconds) > int64((1<<63-1)/time.Second) || int64(c.IdleSeconds) > int64((1<<63-1)/time.Second) {
		return c, fmt.Errorf("database connection duration is too large")
	}
	return c, nil
}

func (c *Config) ValidateDatabasePolicy() error {
	if _, err := c.DatabasePool.WithDefaults(); err != nil {
		return err
	}
	if c.AuditRetentionDays < 0 || c.AuditRetentionDays > 36500 {
		return fmt.Errorf("audit_retention_days must be between 0 (keep all) and 36500")
	}
	return nil
}
