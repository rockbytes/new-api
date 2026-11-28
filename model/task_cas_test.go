package model

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&Task{},
		&User{},
		&UserModelRateLimit{},
		&UserSession{},
		&AuthFlow{},
		&ExternalIdentityClaim{},
		&Token{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&Log{},
		&Channel{},
		&QuotaData{},
		&Ability{},
		&TopUp{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&UserOAuthBinding{},
		&PerfMetric{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
		&UserAccessToken{},
		&Organization{},
		&OrganizationMembership{},
		&OrganizationMemberQuota{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		DB.Exec("DELETE FROM tasks")
		DB.Exec("DELETE FROM auth_flows")
		DB.Exec("DELETE FROM external_identity_claims")
		DB.Exec("DELETE FROM user_sessions")
		DB.Exec("DELETE FROM passkey_credentials")
		DB.Exec("DELETE FROM two_fa_backup_codes")
		DB.Exec("DELETE FROM two_fas")
		DB.Exec("DELETE FROM tokens")
		DB.Exec("DELETE FROM user_access_tokens")
		DB.Exec("DELETE FROM user_oauth_bindings")
		DB.Exec("DELETE FROM user_model_rate_limits")
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM channels")
		DB.Exec("DELETE FROM quota_data")
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM top_ups")
		DB.Exec("DELETE FROM subscription_orders")
		DB.Exec("DELETE FROM subscription_plans")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM perf_metrics")
		DB.Exec("DELETE FROM system_instances")
		DB.Exec("DELETE FROM system_task_locks")
		DB.Exec("DELETE FROM system_tasks")
	})
}

func insertTask(t *testing.T, task *Task) {
	t.Helper()
	task.CreatedAt = time.Now().Unix()
	task.UpdatedAt = time.Now().Unix()
	require.NoError(t, DB.Create(task).Error)
}

func TestGetTaskForProtocolObservationScopesOwnerAndPlatform(t *testing.T) {
	truncateTables(t)
	task := &Task{
		TaskID:   "task_protocol_scope",
		UserId:   7,
		Platform: "plugin-a",
		Status:   TaskStatusInProgress,
	}
	insertTask(t, task)

	got, exists, err := GetTaskForProtocolObservation(context.Background(), 7, "plugin-a", task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	assert.Equal(t, task.ID, got.ID)

	for _, query := range []struct {
		userID   int
		platform string
	}{
		{userID: 8, platform: "plugin-a"},
		{userID: 7, platform: "plugin-b"},
	} {
		got, exists, err = GetTaskForProtocolObservation(context.Background(), query.userID, constant.TaskPlatform(query.platform), task.TaskID)
		require.NoError(t, err)
		assert.False(t, exists)
		assert.Nil(t, got)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = GetTaskForProtocolObservation(cancelled, 7, "plugin-a", task.TaskID)
	require.ErrorIs(t, err, context.Canceled)
}

// ---------------------------------------------------------------------------
// Snapshot / Equal — pure logic tests (no DB)
// ---------------------------------------------------------------------------

func TestSnapshotEqual_Same(t *testing.T) {
	s := taskSnapshot{
		Status:     TaskStatusInProgress,
		Progress:   "50%",
		StartTime:  1000,
		FinishTime: 0,
		FailReason: "",
		ResultURL:  "",
		Data:       json.RawMessage(`{"key":"value"}`),
	}
	assert.True(t, s.Equal(s))
}

func TestSnapshotEqual_DifferentStatus(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusSuccess, Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentProgress(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Progress: "30%", Data: json.RawMessage(`{}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Progress: "60%", Data: json.RawMessage(`{}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_DifferentData(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":1}`)}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage(`{"a":2}`)}
	assert.False(t, a.Equal(b))
}

func TestSnapshotEqual_NilVsEmpty(t *testing.T) {
	a := taskSnapshot{Status: TaskStatusInProgress, Data: nil}
	b := taskSnapshot{Status: TaskStatusInProgress, Data: json.RawMessage{}}
	// bytes.Equal(nil, []byte{}) == true
	assert.True(t, a.Equal(b))
}

func TestSnapshotEqual_PluginStateAndPollFailures(t *testing.T) {
	base := taskSnapshot{
		Status:       TaskStatusInProgress,
		PluginState:  json.RawMessage(`{"req_key":"a"}`),
		PollFailures: 2,
	}
	assert.True(t, base.Equal(taskSnapshot{
		Status:       TaskStatusInProgress,
		PluginState:  json.RawMessage(`{"req_key":"a"}`),
		PollFailures: 2,
	}))
	assert.False(t, base.Equal(taskSnapshot{
		Status:       TaskStatusInProgress,
		PluginState:  json.RawMessage(`{"req_key":"b"}`),
		PollFailures: 2,
	}))
	assert.False(t, base.Equal(taskSnapshot{
		Status:       TaskStatusInProgress,
		PluginState:  json.RawMessage(`{"req_key":"a"}`),
		PollFailures: 3,
	}))
}

func TestSnapshot_Roundtrip(t *testing.T) {
	task := &Task{
		Status:     TaskStatusInProgress,
		Progress:   "42%",
		StartTime:  1234,
		FinishTime: 5678,
		FailReason: "timeout",
		PrivateData: TaskPrivateData{
			ResultURL:    "https://example.com/result.mp4",
			PluginState:  json.RawMessage(`{"req_key":"keep"}`),
			PollFailures: 3,
		},
		Data: json.RawMessage(`{"model":"test-model"}`),
	}
	snap := task.Snapshot()
	assert.Equal(t, task.Status, snap.Status)
	assert.Equal(t, task.Progress, snap.Progress)
	assert.Equal(t, task.StartTime, snap.StartTime)
	assert.Equal(t, task.FinishTime, snap.FinishTime)
	assert.Equal(t, task.FailReason, snap.FailReason)
	assert.Equal(t, task.PrivateData.ResultURL, snap.ResultURL)
	assert.JSONEq(t, string(task.Data), string(snap.Data))
	assert.Equal(t, task.PrivateData.PluginState, snap.PluginState)
	assert.Equal(t, task.PrivateData.PollFailures, snap.PollFailures)
}

// ---------------------------------------------------------------------------
// UpdateWithStatus CAS — DB integration tests
// ---------------------------------------------------------------------------

func TestUpdateWithStatus_Win(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID:   "task_cas_win",
		Status:   TaskStatusInProgress,
		Progress: "50%",
		Data:     json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	task.Progress = "100%"
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	assert.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

func TestUpdateWithStatus_Lose(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_lose",
		Status: TaskStatusFailure,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	task.Status = TaskStatusSuccess
	won, err := task.UpdateWithStatus(TaskStatusInProgress) // wrong fromStatus
	require.NoError(t, err)
	assert.False(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, reloaded.Status) // unchanged
}

func TestUpdateWithStatus_ConcurrentWinner(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_race",
		Status: TaskStatusInProgress,
		Quota:  1000,
		Data:   json.RawMessage(`{}`),
	}
	insertTask(t, task)

	const goroutines = 5
	wins := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			t := &Task{}
			*t = Task{
				ID:       task.ID,
				TaskID:   task.TaskID,
				Status:   TaskStatusSuccess,
				Progress: "100%",
				Quota:    task.Quota,
				Data:     json.RawMessage(`{}`),
			}
			t.CreatedAt = task.CreatedAt
			t.UpdatedAt = time.Now().Unix()
			won, err := t.UpdateWithStatus(TaskStatusInProgress)
			if err == nil {
				wins[idx] = won
			}
		}(i)
	}
	wg.Wait()

	winCount := 0
	for _, w := range wins {
		if w {
			winCount++
		}
	}
	assert.Equal(t, 1, winCount, "exactly one goroutine should win the CAS")
}

func TestFailedTaskRefundAcrossDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, layout := range []string{"fresh", "upgrade"} {
				t.Run(layout, func(t *testing.T) {
					db := openSubscriptionRefundTestDB(t, dialect)
					previous, previousType := DB, common.MainDatabaseType()
					DB = db
					common.SetMainDatabaseType(common.DatabaseType(dialect))
					t.Cleanup(func() { DB = previous; common.SetMainDatabaseType(previousType) })
					verifyFailedTaskRefund(t, db, layout == "upgrade")
				})
			}
		})
	}
}

func verifyFailedTaskRefund(t *testing.T, db *gorm.DB, upgrade bool) {
	t.Helper()
	versionSQL := "SELECT version()"
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		versionSQL = "SELECT sqlite_version()"
	}
	var version string
	require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
	t.Logf("database version: %s", version)
	ctx := context.Background()

	require.NoError(t, db.AutoMigrate(&User{}, &Channel{}, &Token{}, &UserSubscription{}, &Organization{}, &OrganizationMembership{},
		&OrganizationQuotaAccount{}, &OrganizationMemberQuota{}, &OrganizationQuotaLedgerEntry{}, &Task{}))
	if upgrade {
		// Recreate the pre-refund task schema while preserving all existing indexes.
		require.NoError(t, db.Migrator().DropIndex(&Task{}, "idx_tasks_refund_pending_at"))
		require.NoError(t, db.Migrator().DropColumn(&Task{}, "refund_pending_at"))
	}
	historical := Task{TaskID: "old-failed", Quota: 30, Status: TaskStatusFailure, Progress: "100%"}
	require.NoError(t, db.Omit("RefundPendingAt").Create(&historical).Error)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&Task{}))
	}
	require.True(t, db.Migrator().HasIndex(&Task{}, "idx_tasks_task_id"))
	assert.False(t, HasUnfinishedSyncTasks())
	pending, err := GetPendingTaskRefunds(common.GetTimestamp(), 100)
	require.NoError(t, err)
	assert.Empty(t, pending)

	const quota = 100
	for _, payer := range []string{"wallet", "subscription", "organization"} {
		t.Run(payer, func(t *testing.T) {
			user := User{Username: payer, AffCode: payer, Quota: 500, UsedQuota: quota, RequestCount: 1}
			require.NoError(t, db.Create(&user).Error)
			token := Token{UserId: user.Id, Key: payer, RemainQuota: 400, UsedQuota: quota}
			require.NoError(t, db.Create(&token).Error)
			ch := Channel{Name: payer, Key: "test", UsedQuota: quota}
			require.NoError(t, db.Create(&ch).Error)
			task := Task{TaskID: "new-failure-" + payer, Quota: quota, Status: TaskStatusInProgress, Progress: "50%", UserId: user.Id, ChannelId: ch.Id,
				PrivateData: TaskPrivateData{BillingSource: payer, TokenId: token.Id}}
			wantWallet := user.Quota
			switch payer {
			case "wallet":
				wantWallet += quota
			case "subscription":
				const epoch int64 = 7
				sub := UserSubscription{UserId: user.Id, AmountUsed: quota + 10, AmountTotal: 1000, UsageResetEpoch: epoch}
				require.NoError(t, db.Create(&sub).Error)
				task.PrivateData.SubscriptionId = sub.Id
				task.PrivateData.SubscriptionPeriodStart = common.GetPointer(epoch)
			case "organization":
				org := Organization{Name: payer, Status: OrganizationStatusActive}
				require.NoError(t, db.Create(&org).Error)
				require.NoError(t, db.Create(&OrganizationMembership{OrganizationId: org.Id, UserId: user.Id,
					Role: OrganizationRoleMember, Status: OrganizationMembershipActive}).Error)
				task.BillingOrganizationId = org.Id
				task.PrivateData.BillingRequestId = task.TaskID
				_, err := GrantOrganizationQuota(org.Id, 1000, user.Id, "test", "task-refund", 1)
				require.NoError(t, err)
				require.NoError(t, SetOrganizationMemberAllocation(org.Id, user.Id, 1000, 2))
				require.NoError(t, ConsumeOrganizationQuota(org.Id, user.Id, task.TaskID, 1, quota-20, 3))
			}
			require.NoError(t, db.Create(&task).Error)
			task.Status, task.Progress, task.FailReason = TaskStatusFailure, "100%", "test failure"
			won, err := task.UpdateWithStatus(TaskStatusInProgress)
			require.NoError(t, err)
			require.True(t, won)
			pending, err := GetPendingTaskRefunds(common.GetTimestamp(), 100)
			require.NoError(t, err)
			require.Len(t, pending, 1)
			require.Equal(t, task.ID, pending[0].ID)

			// Failure after the payer write rolls it back together with the claim.
			const callback = "test:task_refund_fail_token"
			writeErr := errors.New("test token update failure")
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "tokens" {
					tx.AddError(writeErr)
				}
			}))
			_, err = pending[0].ApplyFailedTaskRefund(ctx)
			assert.ErrorIs(t, err, writeErr)
			require.NoError(t, db.Callback().Update().Remove(callback))
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 500, user.Quota)
			assert.Equal(t, quota, user.UsedQuota)
			assert.True(t, HasUnfinishedSyncTasks())

			// The retry timestamp doubles as the claim revision: a stale worker
			// cannot bypass backoff after another worker defers a broken refund.
			if payer == "wallet" {
				stale := pending[0]
				require.NoError(t, stale.DeferTaskRefund(common.GetTimestamp()+60))
				refunded, err := stale.ApplyFailedTaskRefund(ctx)
				require.NoError(t, err)
				assert.Zero(t, refunded)
				assert.False(t, HasUnfinishedSyncTasks())
				require.NoError(t, db.Model(&Task{}).Where("id = ?", task.ID).UpdateColumn("refund_pending_at", common.GetTimestamp()).Error)
				pending, err = GetPendingTaskRefunds(common.GetTimestamp(), 100)
				require.NoError(t, err)
				require.Len(t, pending, 1)
			}

			stale := *pending[0]
			type refundResult struct {
				quota int
				err   error
			}
			results := make(chan refundResult, 2)
			start := make(chan struct{})
			for range 2 {
				attempt := stale
				go func() {
					<-start
					quota, err := attempt.ApplyFailedTaskRefund(ctx)
					results <- refundResult{quota, err}
				}()
			}
			close(start)
			first, second := <-results, <-results
			require.NoError(t, first.err)
			require.NoError(t, second.err)
			assert.ElementsMatch(t, []int{0, quota}, []int{first.quota, second.quota})
			refunded, err := stale.ApplyFailedTaskRefund(ctx)
			require.NoError(t, err)
			assert.Zero(t, refunded)
			stale.Status = TaskStatusSuccess
			won, err = stale.UpdateWithStatus(TaskStatusFailure)
			require.NoError(t, err)
			assert.False(t, won, "a late success must not replace a terminal failure")
			var stored Task
			require.NoError(t, db.First(&stored, task.ID).Error)
			assert.EqualValues(t, TaskStatusFailure, stored.Status)
			assert.Zero(t, stored.Quota)
			assert.False(t, HasUnfinishedSyncTasks())
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, wantWallet, user.Quota)
			assert.Zero(t, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			require.NoError(t, db.First(&token, token.Id).Error)
			assert.Equal(t, 500, token.RemainQuota)
			assert.Zero(t, token.UsedQuota)
			require.NoError(t, db.First(&ch, ch.Id).Error)
			assert.Zero(t, ch.UsedQuota)
			if payer == "subscription" {
				assert.EqualValues(t, 10, getSubscriptionResetSub(t, task.PrivateData.SubscriptionId).AmountUsed)
			}
			if payer == "organization" {
				reconciliation := reconciliationOf(t, task.BillingOrganizationId)
				assert.Zero(t, reconciliation.ConsumedQuota)
				assert.True(t, reconciliation.Balanced)
			}
		})
	}
	var preserved Task
	require.NoError(t, db.First(&preserved, historical.ID).Error)
	assert.EqualValues(t, TaskStatusFailure, preserved.Status)
	assert.Equal(t, historical.Quota, preserved.Quota)
}

func TestUpdateWithStatus_PersistsPluginStateAndPollFailures(t *testing.T) {
	truncateTables(t)

	task := &Task{
		TaskID: "task_cas_plugin_state",
		Status: TaskStatusInProgress,
		Data:   json.RawMessage(`{}`),
		PrivateData: TaskPrivateData{
			PluginState:  json.RawMessage(`{"req_key":"old"}`),
			PollFailures: 1,
		},
	}
	insertTask(t, task)

	task.PrivateData.PluginState = json.RawMessage(`{"req_key":"new"}`)
	task.PrivateData.PollFailures = 4
	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.NoError(t, err)
	require.True(t, won)

	var reloaded Task
	require.NoError(t, DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, TaskStatusInProgress, reloaded.Status)
	assert.JSONEq(t, `{"req_key":"new"}`, string(reloaded.PrivateData.PluginState))
	assert.Equal(t, 4, reloaded.PrivateData.PollFailures)
}
