package task

import (
	"encoding/json"
	"testing"

	"auto-deploy-platform/internal/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploymentActorIsPersistedAndMasked(t *testing.T) {
	for _, principal := range []identity.Principal{
		{Type: "ip", Value: "192.0.2.10", IP: "192.0.2.10"},
		{Type: "oidc", Value: "18223452780", IP: "198.51.100.2"},
		{},
	} {
		t.Run(principal.Type, func(t *testing.T) {
			manager, cleanup := setupTestDB(t)
			defer cleanup()
			request := validRequest()
			request.Principal = principal
			task, err := manager.CreateTask(request)
			require.NoError(t, err)
			assert.Equal(t, principal.Type, task.OperatorType)
			assert.Equal(t, principal.Value, task.OperatorValue)
			assert.Equal(t, principal.IP, task.OperatorIP)
			for _, filter := range []RecordFilter{{}, {ProjectKeyword: "test"}} {
				records, _, err := manager.ListRecords(filter)
				require.NoError(t, err)
				require.Len(t, records, 1)
				assert.Equal(t, principal.Display(), records[0].Operator)
				assert.Equal(t, principal.Type, records[0].OperatorType)
				body, err := json.Marshal(records)
				require.NoError(t, err)
				if principal.Type == "oidc" {
					assert.NotContains(t, string(body), principal.Value)
					assert.Contains(t, string(body), "182****2780")
				}
			}
		})
	}
}
