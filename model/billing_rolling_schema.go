package model

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type rollingBillingColumn struct {
	model any
	field string
}

var rollingBillingColumns = []rollingBillingColumn{
	{&User{}, "PersonalKeyUsedQuota"},
	{&Task{}, "RefundPendingAt"},
	{&UserSubscription{}, "UsageResetEpoch"},
	{&SubscriptionPreConsumeRecord{}, "UsageResetEpoch"},
}

// A release built from dev also includes the per-user rate-limit feature.
var rollingUpgradeTables = []any{&PersonalKeySettlement{}, &UserModelRateLimit{}}

// PrepareBillingRollingSchema runs without starting a gateway or touching its
// log store. Require an explicit database target so this command never falls
// back to an unrelated local SQLite file.
func PrepareBillingRollingSchema() error {
	dsn := strings.TrimSpace(os.Getenv("SQL_DSN"))
	if dsn == "" || (strings.HasPrefix(dsn, "local") && os.Getenv("SQLITE_PATH") == "") {
		return errors.New("set the gateway's SQL_DSN (and SQLITE_PATH for SQLite) before preparing the billing schema")
	}
	if path := os.Getenv("SQLITE_PATH"); path != "" {
		common.SQLitePath = path
	}
	db, dialect, err := chooseDB("SQL_DSN", false)
	if err != nil {
		return err
	}
	connection, err := db.DB()
	if err != nil {
		return err
	}
	defer connection.Close()
	return MigrateBillingRollingSchema(db, dialect)
}

// MigrateBillingRollingSchema is additive and can be retried. Do not run the
// full application AutoMigrate while an old gateway is still serving requests.
func MigrateBillingRollingSchema(db *gorm.DB, dialect common.DatabaseType) error {
	migrate := func(tx *gorm.DB) error {
		m := tx.Migrator()
		for _, column := range rollingBillingColumns {
			if !m.HasTable(column.model) {
				return fmt.Errorf("billing schema preparation requires the existing %T table", column.model)
			}
		}
		for _, column := range rollingBillingColumns {
			if !m.HasColumn(column.model, column.field) {
				if err := m.AddColumn(column.model, column.field); err != nil {
					return fmt.Errorf("add billing column %s: %w", column.field, err)
				}
			}
		}
		for _, table := range rollingUpgradeTables {
			if !m.HasTable(table) {
				if err := m.CreateTable(table); err != nil {
					return err
				}
			}
		}
		if !m.HasIndex(&Task{}, "RefundPendingAt") {
			if err := m.CreateIndex(&Task{}, "RefundPendingAt"); err != nil {
				return err
			}
		}
		return CheckBillingRollingSchema(tx)
	}
	switch dialect {
	case common.DatabaseTypePostgreSQL:
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SET LOCAL lock_timeout = '2s'").Error; err != nil {
				return err
			}
			return migrate(tx)
		})
	case common.DatabaseTypeMySQL:
		return db.Connection(func(conn *gorm.DB) error {
			if err := conn.Exec("SET SESSION lock_wait_timeout = 2").Error; err != nil {
				return err
			}
			defer conn.Exec("SET SESSION lock_wait_timeout = DEFAULT")
			return migrate(conn)
		})
	default:
		return db.Transaction(migrate)
	}
}

// CheckBillingRollingSchema catches a slave-first upgrade that skipped schema preparation.
func CheckBillingRollingSchema(db *gorm.DB) error {
	for _, column := range rollingBillingColumns {
		if !db.Migrator().HasColumn(column.model, column.field) {
			return fmt.Errorf("billing column %s is missing; run --migrate-billing-schema before rolling upgrade", column.field)
		}
	}
	for _, table := range rollingUpgradeTables {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("required %T table is missing; run --migrate-billing-schema before rolling upgrade", table)
		}
	}
	if !db.Migrator().HasIndex(&Task{}, "RefundPendingAt") {
		return errors.New("billing table or index is missing; run --migrate-billing-schema before rolling upgrade")
	}
	return nil
}
