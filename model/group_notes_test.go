package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestGroupNotesDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct {
		name, env string
		open      func(string) gorm.Dialector
	}{
		{"sqlite", "", func(string) gorm.Dialector { return sqlite.Open(":memory:") }},
		{"mysql", "TEST_MYSQL_DSN", mysql.Open},
		{"postgres", "TEST_POSTGRES_DSN", func(dsn string) gorm.Dialector {
			return postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
		}},
	} {
		t.Run(dialect.name, func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dialect.env != "" && dsn == "" {
				t.Skipf("set %s to run this database", dialect.env)
			}
			db, err := gorm.Open(dialect.open(dsn), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "group_notes_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
			sqlDB.SetMaxOpenConns(1)
			require.False(t, db.Migrator().HasTable(&Option{}), "use an isolated database without leftover group_notes_test_options")
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(&Option{})) })
			require.NoError(t, db.AutoMigrate(&Option{}))
			query := "SELECT version()"
			if dialect.name == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			previousDB, previousOptions := DB, common.OptionMap
			previousPolicy := requestPolicySnapshot.Load()
			pluginSnapshot := jsplugin.DefaultRegistry.Snapshot()
			pluginEnabled := jsplugin.DefaultRegistry.Enabled()
			DB = db
			t.Cleanup(func() {
				DB, common.OptionMap = previousDB, previousOptions
				requestPolicySnapshot.Store(previousPolicy)
				jsplugin.DefaultRegistry.SetEnabled(pluginEnabled)
				jsplugin.DefaultRegistry.SetDisabledFactoryKeys(pluginSnapshot.DisabledFactory)
			})
			InitOptionMap()
			assert.Equal(t, "{}", common.OptionMap[GroupNotesOptionKey])
			options, err := AllOption()
			require.NoError(t, err)
			assert.Empty(t, options, "using the default must not insert an options row")

			const canonical = `{"Group":"<备用>","group":"second"}`
			const input = ` {"Group":"  <备用>　","group":"second","Other":" \t "} `
			require.NoError(t, UpdateOption(GroupNotesOptionKey, input))
			options, err = AllOption()
			require.NoError(t, err)
			require.Len(t, options, 1)
			assert.Equal(t, GroupNotesOptionKey, options[0].Key)
			assert.JSONEq(t, canonical, options[0].Value)
			assert.Equal(t, options[0].Value, common.OptionMap[GroupNotesOptionKey])
			InitOptionMap()
			assert.JSONEq(t, canonical, common.OptionMap[GroupNotesOptionKey])

			// Exercise the upper boundary on each engine, not just a small JSON value.
			const maxBytes = 2 * 1024 * 1024
			atLimit := `{"` + strings.Repeat("g", maxBytes-len(`{"":"n"}`)) + `":"n"}`
			values := map[string]string{GroupNotesOptionKey: atLimit}
			require.NoError(t, UpdateOptionsBulk(values))
			assert.True(t, atLimit == requireOptionValue(t, db, GroupNotesOptionKey), "maximum length JSON must round-trip without truncation")
			assert.True(t, atLimit == common.OptionMap[GroupNotesOptionKey])

			require.NoError(t, UpdateOption(GroupNotesOptionKey, `{"Group":"　 "}`))
			InitOptionMap()
			assert.Equal(t, "{}", common.OptionMap[GroupNotesOptionKey])
			options, err = AllOption()
			require.NoError(t, err)
			assert.Equal(t, []*Option{{Key: GroupNotesOptionKey, Value: "{}"}}, options)
		})
	}
}

func TestGroupNotesRejectsInvalidWritesAndKeepsLastPersistedValue(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	previousOptions := common.OptionMap
	previousPolicy := requestPolicySnapshot.Load()
	previousPasskeySettings, previousServerAddress := *system_setting.GetPasskeySettings(), system_setting.ServerAddress
	common.OptionMap = make(map[string]string)
	*system_setting.GetPasskeySettings() = system_setting.PasskeySettings{}
	system_setting.ServerAddress = ""
	t.Cleanup(func() {
		*system_setting.GetPasskeySettings(), system_setting.ServerAddress = previousPasskeySettings, previousServerAddress
		common.OptionMap = previousOptions
		requestPolicySnapshot.Store(previousPolicy)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	const key = GroupNotesOptionKey
	const failureText = "private rejected text"
	const invalid = `{"Group":null}`
	const maxBytes = 2 * 1024 * 1024
	var before = `{"Group":"` + strings.Repeat("😀", 200) + `"}`
	require.NoError(t, UpdateOption(key, before))
	for _, tc := range []struct{ name, input string }{
		{"invalid JSON", `{"Group":"` + failureText},
		{"trailing JSON", `{ } { }`},
		{"array", `[]`},
		{"null object", `null`},
		{"null note", invalid},
		{"number note", `{"Group":1}`},
		{"nested note", `{"Group":{}}`},
		{"blank name", `{"":"n"}`},
		{"untrimmed name", `{"Group　":"n"}`},
		{"invalid UTF-8", "{\"Group\":\"\xff\"}"},
		{"too many code points", `{"Group":"` + strings.Repeat("😀", 201) + `"}`},
		{"submitted value too large", `{} ` + strings.Repeat(" ", maxBytes-2)},
		{"normalized value too large", `{"` + strings.Repeat("<", maxBytes/6) + `":"n"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := UpdateOption(key, tc.input)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), failureText)
			assert.Equal(t, before, common.OptionMap[key])
		})
	}
	for _, otherKey := range []string{"Notice", "passkey.origins"} {
		err := UpdateOptionsBulk(map[string]string{key: invalid, otherKey: "must not persist"})
		require.ErrorIs(t, err, errGroupNotesObject)
		requireOptionMissing(t, db, otherKey)
	}
	assert.Equal(t, before, requireOptionValue(t, db, key))

	// Fail inside a real database statement even though the submitted data is
	// valid. The driver error deliberately contains private text.
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_group_notes BEFORE INSERT ON options
WHEN NEW.key = 'GroupNotes' BEGIN SELECT RAISE(ABORT, 'private rejected text'); END`).Error)
	err := UpdateOption(key, `{"Group":"private rejected text"}`)
	require.ErrorIs(t, err, errGroupNotesSave)
	assert.NotContains(t, err.Error(), failureText)
	assert.Equal(t, before, common.OptionMap[key])
	assert.Equal(t, before, requireOptionValue(t, db, key))

	// A mixed bulk write takes the existing passkey transaction path. It
	// still stores the canonical value without changing the input map.
	const mixedInput = `{"Other":"  latest  "}`
	batch := map[string]string{key: mixedInput, "passkey.origins": ""}
	require.NoError(t, db.Exec(`DROP TRIGGER reject_group_notes`).Error)
	require.NoError(t, UpdateOptionsBulk(batch))
	assert.Equal(t, mixedInput, batch[key])
	assert.Equal(t, `{"Other":"latest"}`, requireOptionValue(t, db, key))
	assert.Equal(t, `{"Other":"latest"}`, common.OptionMap[key])

	// A value from another instance becomes visible on sync; an invalid
	// database row must not displace the last valid in-memory value.
	require.NoError(t, db.Model(&Option{Key: key}).Update("value", `{"Other":"  remote  "}`).Error)
	loadOptionsFromDatabase()
	assert.Equal(t, `{"Other":"remote"}`, common.OptionMap[key])
	require.NoError(t, db.Model(&Option{Key: key}).Update("value", invalid).Error)
	loadOptionsFromDatabase()
	assert.Equal(t, `{"Other":"remote"}`, common.OptionMap[key])
}
