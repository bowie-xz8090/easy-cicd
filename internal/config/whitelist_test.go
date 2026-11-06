package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWhitelistMigrationAndPrecedence(t *testing.T) {
	for _, test := range []struct {
		name, access string
		want         []string
	}{
		{"legacy", "  ip_whitelist: [192.0.2.1]\n", []string{"192.0.2.1"}},
		{"mixed", "  whitelist: [192.0.2.1, '18223452780', ' ::ffff:192.0.2.1 ', '18223452780']\n", []string{"192.0.2.1", "18223452780"}},
		{"new wins", "  ip_whitelist: [192.0.2.1]\n  whitelist: ['18223452780']\n", []string{"18223452780"}},
		{"empty wins", "  ip_whitelist: [192.0.2.1]\n  whitelist: []\n", []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeTestConfig(t, validYAML+"\naccess:\n"+test.access)
			manager, err := NewManager(path)
			require.NoError(t, err)
			assert.Equal(t, test.want, manager.Get().Access.Whitelist)
			require.NoError(t, manager.Update(manager.Get()))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(data), "whitelist:")
			assert.NotContains(t, string(data), "ip_whitelist:")
			reloaded, err := Load(path)
			require.NoError(t, err)
			assert.Equal(t, test.want, reloaded.Access.Whitelist)
		})
	}
}

func TestWhitelistRejectsInvalidEntriesWithoutChangingDisk(t *testing.T) {
	manager, err := NewManager(writeTestConfig(t, validYAML))
	require.NoError(t, err)
	before, err := os.ReadFile(manager.GetFilePath())
	require.NoError(t, err)
	for _, entry := range []string{"", "1822345278", "182234527800", "+8618223452780", "abcdefghijk", "192.0.2.0/24", "192.0.2.1-10", "999.1.1.1"} {
		t.Run(entry, func(t *testing.T) {
			require.Error(t, manager.UpdateWhitelist([]string{entry}))
			after, err := os.ReadFile(manager.GetFilePath())
			require.NoError(t, err)
			assert.Equal(t, before, after)
			assert.Empty(t, manager.Get().Access.Whitelist)
		})
	}
}
