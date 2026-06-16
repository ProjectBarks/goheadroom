package ccr

import "fmt"

// CcrBackendConfig selects and configures a CCR store backend.
type CcrBackendConfig struct {
	Backend    string
	SqlitePath string
	RedisAddr  string
	KeyPrefix  string
}

// FromConfig creates a CcrStore from configuration.
func FromConfig(cfg CcrBackendConfig) (CcrStore, error) {
	switch cfg.Backend {
	case "inmemory", "":
		return NewInMemoryStore(), nil
	case "sqlite":
		if cfg.SqlitePath == "" {
			return nil, fmt.Errorf("ccr sqlite backend requires SqlitePath")
		}
		return NewSqliteStore(cfg.SqlitePath, DefaultTTL)
	case "redis":
		if cfg.RedisAddr == "" {
			return nil, fmt.Errorf("ccr redis backend requires RedisAddr")
		}
		prefix := cfg.KeyPrefix
		if prefix == "" {
			prefix = "ccr"
		}
		return NewRedisStore(cfg.RedisAddr, prefix, DefaultTTL)
	default:
		return nil, fmt.Errorf("unknown ccr backend %q", cfg.Backend)
	}
}
