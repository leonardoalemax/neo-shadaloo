package config

import "context"

// ConfigRepository defines key-value config persistence.
type ConfigRepository interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}
