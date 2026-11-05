package db

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessMigrationPreservesLegacyRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	_, err = legacy.Exec(createTableSQL)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	_, err = legacy.Exec(`INSERT INTO deploy_tasks (id,project_owner,project_name,branch,environment,created_at,updated_at) VALUES ('legacy','org','project','main','dev',?,?)`, now.Format(time.RFC3339), now.Format(time.RFC3339))
	require.NoError(t, err)
	require.NoError(t, legacy.Close())
	for attempt := 0; attempt < 2; attempt++ {
		database, err := InitDB(path)
		require.NoError(t, err)
		record, err := GetTaskByID(database, "legacy")
		require.NoError(t, err)
		assert.Equal(t, "project", record.ProjectName)
		assert.Empty(t, record.OperatorType)
		assert.Empty(t, record.OperatorValue)
		require.NoError(t, database.Close())
	}
}

func TestOperatorPersistenceAndJSONPrivacy(t *testing.T) {
	database := setupTestDB(t)
	task := newTestTask()
	task.OperatorType = "oidc"
	task.OperatorValue = "18223452780"
	task.OperatorIP = "198.51.100.1"
	require.NoError(t, CreateTask(database, task))
	byID, err := GetTaskByID(database, task.ID)
	require.NoError(t, err)
	records, _, err := ListRecords(database, RecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 1)
	all, _, err := ListAllRecords(database, RecordFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	for _, record := range []DeployTask{*byID, records[0], all[0]} {
		assert.Equal(t, task.OperatorType, record.OperatorType)
		assert.Equal(t, task.OperatorValue, record.OperatorValue)
		assert.Equal(t, task.OperatorIP, record.OperatorIP)
		body, err := json.Marshal(record)
		require.NoError(t, err)
		assert.NotContains(t, string(body), task.OperatorValue)
	}
}
