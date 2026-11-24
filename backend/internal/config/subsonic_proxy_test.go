package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSubsonicTrustedProxyCIDRs(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "empty disables proxy trust"},
		{name: "single IPv4 CIDR", value: "127.0.0.1/32"},
		{name: "comma separated IPv4 and IPv6 CIDRs", value: "10.0.0.0/8, fc00::/7"},
		{name: "malformed CIDR", value: "127.0.0.1", wantErr: true},
		{name: "invalid address", value: "not-a-cidr", wantErr: true},
		{name: "empty item", value: "10.0.0.0/8,,192.168.0.0/16", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSubsonicTrustedProxyCIDRs(tt.value)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLoadSubsonicTrustedProxyCIDRsFromEnvironment(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CONFIG_ENV", "test-no-config")
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_URL", "sqlite://config-test.db")
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	t.Setenv("SUBSONIC_ENABLED", "false")
	t.Setenv("SUBSONIC_TRUSTED_PROXY_CIDRS", "127.0.0.1/32,10.0.0.0/8")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1/32,10.0.0.0/8", cfg.SubsonicTrustedProxyCIDRs)

	t.Setenv("SUBSONIC_TRUSTED_PROXY_CIDRS", "127.0.0.1")
	_, err = Load()
	require.ErrorContains(t, err, "SUBSONIC_TRUSTED_PROXY_CIDRS")
}
