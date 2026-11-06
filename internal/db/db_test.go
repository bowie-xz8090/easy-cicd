package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB creates an in-memory SQLite database for testing.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := InitDB(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

// newTestTask creates a DeployTask with sensible defaults for testing.
func newTestTask() *DeployTask {
	now := time.Now().UTC().Truncate(time.Second)
	return &DeployTask{
		ID:           uuid.New().String(),
		ProjectOwner: "test-org",
		ProjectName:  "test-project",
		Branch:       "main",
		Environment:  "dev",
		Status:       "pending",
		Logs:         "",
		ErrorMessage: "",
		CreatedAt:    now,
		UpdatedAt:    now,
		FinishedAt:   nil,
	}
}

func TestInitDB(t *testing.T) {
	db := setupTestDB(t)
	assert.NotNil(t, db)

	// Verify the table exists by running a simple query.
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM deploy_tasks").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCreateTask(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()

	err := CreateTask(db, task)
	require.NoError(t, err)

	// Verify the task was inserted.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM deploy_tasks WHERE id = ?", task.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestCreateTask_DuplicateID(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()

	err := CreateTask(db, task)
	require.NoError(t, err)

	// Inserting the same ID again should fail.
	err = CreateTask(db, task)
	assert.Error(t, err)
}

func TestGetTaskByID(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()

	err := CreateTask(db, task)
	require.NoError(t, err)

	got, err := GetTaskByID(db, task.ID)
	require.NoError(t, err)

	assert.Equal(t, task.ID, got.ID)
	assert.Equal(t, task.ProjectOwner, got.ProjectOwner)
	assert.Equal(t, task.ProjectName, got.ProjectName)
	assert.Equal(t, task.Branch, got.Branch)
	assert.Equal(t, task.Environment, got.Environment)
	assert.Equal(t, task.Status, got.Status)
	assert.Equal(t, task.Logs, got.Logs)
	assert.Equal(t, task.ErrorMessage, got.ErrorMessage)
	assert.Equal(t, task.CreatedAt.UTC(), got.CreatedAt.UTC())
	assert.Equal(t, task.UpdatedAt.UTC(), got.UpdatedAt.UTC())
	assert.Nil(t, got.FinishedAt)
}

func TestGetTaskByID_NotFound(t *testing.T) {
	db := setupTestDB(t)

	got, err := GetTaskByID(db, "nonexistent-id")
	assert.Error(t, err)
	assert.Nil(t, got)
	assert.Contains(t, err.Error(), "task not found")
}

func TestGetTaskByID_WithFinishedAt(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()
	finishedAt := time.Now().UTC().Truncate(time.Second)
	task.FinishedAt = &finishedAt

	err := CreateTask(db, task)
	require.NoError(t, err)

	got, err := GetTaskByID(db, task.ID)
	require.NoError(t, err)
	require.NotNil(t, got.FinishedAt)
	assert.Equal(t, finishedAt.UTC(), got.FinishedAt.UTC())
}

func TestUpdateTaskStatus(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()

	err := CreateTask(db, task)
	require.NoError(t, err)

	// Update to a non-terminal status.
	err = UpdateTaskStatus(db, task.ID, "building")
	require.NoError(t, err)

	got, err := GetTaskByID(db, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "building", got.Status)
	assert.Nil(t, got.FinishedAt)
}

func TestUpdateTaskStatus_Success_SetsFinishedAt(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()

	err := CreateTask(db, task)
	require.NoError(t, err)

	err = UpdateTaskStatus(db, task.ID, "success")
	require.NoError(t, err)

	got, err := GetTaskByID(db, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "success", got.Status)
	require.NotNil(t, got.FinishedAt)
}

func TestUpdateTaskStatus_Failed_SetsFinishedAt(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()

	err := CreateTask(db, task)
	require.NoError(t, err)

	err = UpdateTaskStatus(db, task.ID, "failed")
	require.NoError(t, err)

	got, err := GetTaskByID(db, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", got.Status)
	require.NotNil(t, got.FinishedAt)
}

func TestUpdateTaskStatus_NotFound(t *testing.T) {
	db := setupTestDB(t)

	err := UpdateTaskStatus(db, "nonexistent-id", "building")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestUpdateTaskStatus_PreservesTerminalTimeAndStatus(t *testing.T) {
	for _, terminal := range []string{"success", "failed"} {
		t.Run(terminal, func(t *testing.T) {
			database := setupTestDB(t)
			task := newTestTask()
			require.NoError(t, CreateTask(database, task))
			finished := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
			_, err := database.Exec("UPDATE deploy_tasks SET status = ?, finished_at = ? WHERE id = ?", terminal, finished.Format(time.RFC3339), task.ID)
			require.NoError(t, err)
			for _, lateStatus := range []string{"failed", "building", "success"} {
				require.NoError(t, UpdateTaskStatus(database, task.ID, lateStatus))
				got, err := GetTaskByID(database, task.ID)
				require.NoError(t, err)
				require.NotNil(t, got.FinishedAt)
				assert.Equal(t, finished, *got.FinishedAt)
				assert.Equal(t, terminal, got.Status)
			}
		})
	}
}

func TestUpdateTaskLogs(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()

	err := CreateTask(db, task)
	require.NoError(t, err)

	logs := "[2024-01-01T00:00:00Z] [cloning] Cloning repository..."
	err = UpdateTaskLogs(db, task.ID, logs)
	require.NoError(t, err)

	got, err := GetTaskByID(db, task.ID)
	require.NoError(t, err)
	assert.Equal(t, logs, got.Logs)
}

func TestUpdateTaskLogs_NotFound(t *testing.T) {
	db := setupTestDB(t)

	err := UpdateTaskLogs(db, "nonexistent-id", "some logs")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestListRecords_Empty(t *testing.T) {
	db := setupTestDB(t)

	records, total, err := ListRecords(db, RecordFilter{})
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, records)
}

func TestListRecords_AllRecords(t *testing.T) {
	db := setupTestDB(t)

	// Insert 3 tasks.
	for i := 0; i < 3; i++ {
		task := newTestTask()
		task.CreatedAt = task.CreatedAt.Add(time.Duration(i) * time.Second)
		task.UpdatedAt = task.CreatedAt
		err := CreateTask(db, task)
		require.NoError(t, err)
	}

	records, total, err := ListRecords(db, RecordFilter{})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, records, 3)
}

func TestListRecords_FilterByProject(t *testing.T) {
	db := setupTestDB(t)

	// Insert tasks with different project names.
	task1 := newTestTask()
	task1.ProjectName = "project-a"
	require.NoError(t, CreateTask(db, task1))

	task2 := newTestTask()
	task2.ProjectName = "project-b"
	require.NoError(t, CreateTask(db, task2))

	task3 := newTestTask()
	task3.ProjectName = "project-a"
	require.NoError(t, CreateTask(db, task3))

	records, total, err := ListRecords(db, RecordFilter{Project: "project-a"})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, records, 2)
	for _, r := range records {
		assert.Equal(t, "project-a", r.ProjectName)
	}
}

func TestListRecords_FilterByEnvironment(t *testing.T) {
	db := setupTestDB(t)

	task1 := newTestTask()
	task1.Environment = "dev"
	require.NoError(t, CreateTask(db, task1))

	task2 := newTestTask()
	task2.Environment = "sit"
	require.NoError(t, CreateTask(db, task2))

	task3 := newTestTask()
	task3.Environment = "dev"
	require.NoError(t, CreateTask(db, task3))

	records, total, err := ListRecords(db, RecordFilter{Environment: "sit"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, records, 1)
	assert.Equal(t, "sit", records[0].Environment)
}

func TestListRecords_FilterByProjectAndEnvironment(t *testing.T) {
	db := setupTestDB(t)

	task1 := newTestTask()
	task1.ProjectName = "proj-x"
	task1.Environment = "dev"
	require.NoError(t, CreateTask(db, task1))

	task2 := newTestTask()
	task2.ProjectName = "proj-x"
	task2.Environment = "prod"
	require.NoError(t, CreateTask(db, task2))

	task3 := newTestTask()
	task3.ProjectName = "proj-y"
	task3.Environment = "dev"
	require.NoError(t, CreateTask(db, task3))

	records, total, err := ListRecords(db, RecordFilter{Project: "proj-x", Environment: "dev"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, records, 1)
	assert.Equal(t, "proj-x", records[0].ProjectName)
	assert.Equal(t, "dev", records[0].Environment)
}

func TestListRecords_Pagination(t *testing.T) {
	db := setupTestDB(t)

	// Insert 5 tasks with staggered creation times.
	for i := 0; i < 5; i++ {
		task := newTestTask()
		task.CreatedAt = task.CreatedAt.Add(time.Duration(i) * time.Second)
		task.UpdatedAt = task.CreatedAt
		require.NoError(t, CreateTask(db, task))
	}

	// Page 1, size 2.
	records, total, err := ListRecords(db, RecordFilter{Page: 1, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, records, 2)

	// Page 2, size 2.
	records, total, err = ListRecords(db, RecordFilter{Page: 2, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, records, 2)

	// Page 3, size 2 — only 1 record left.
	records, total, err = ListRecords(db, RecordFilter{Page: 3, PageSize: 2})
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, records, 1)
}

func TestListRecords_OrderByCreatedAtDesc(t *testing.T) {
	db := setupTestDB(t)

	baseTime := time.Now().UTC().Truncate(time.Second)

	task1 := newTestTask()
	task1.ProjectName = "first"
	task1.CreatedAt = baseTime
	task1.UpdatedAt = baseTime
	require.NoError(t, CreateTask(db, task1))

	task2 := newTestTask()
	task2.ProjectName = "second"
	task2.CreatedAt = baseTime.Add(1 * time.Second)
	task2.UpdatedAt = task2.CreatedAt
	require.NoError(t, CreateTask(db, task2))

	task3 := newTestTask()
	task3.ProjectName = "third"
	task3.CreatedAt = baseTime.Add(2 * time.Second)
	task3.UpdatedAt = task3.CreatedAt
	require.NoError(t, CreateTask(db, task3))

	records, _, err := ListRecords(db, RecordFilter{})
	require.NoError(t, err)
	require.Len(t, records, 3)

	// Most recent first.
	assert.Equal(t, "third", records[0].ProjectName)
	assert.Equal(t, "second", records[1].ProjectName)
	assert.Equal(t, "first", records[2].ProjectName)
}

func TestListRecords_DefaultPagination(t *testing.T) {
	db := setupTestDB(t)

	task := newTestTask()
	require.NoError(t, CreateTask(db, task))

	// Zero/negative values should use defaults.
	records, total, err := ListRecords(db, RecordFilter{Page: 0, PageSize: 0})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, records, 1)
}

func TestClearDeployHistory(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()
	task.Status = "success"
	task.Logs = "large deployment log"
	require.NoError(t, CreateTask(db, task))

	deleted, err := ClearDeployHistory(db)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	_, total, err := ListRecords(db, RecordFilter{})
	require.NoError(t, err)
	assert.Zero(t, total)
}

func TestHasActiveTasks(t *testing.T) {
	db := setupTestDB(t)
	task := newTestTask()
	require.NoError(t, CreateTask(db, task))

	hasActive, err := HasActiveTasks(db)
	require.NoError(t, err)
	assert.True(t, hasActive)

	require.NoError(t, UpdateTaskStatus(db, task.ID, "success"))
	hasActive, err = HasActiveTasks(db)
	require.NoError(t, err)
	assert.False(t, hasActive)
}
