package model

import (
	"maps"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

const (
	GroupNotesOptionKey = "GroupNotes"
	groupNotesMaxBytes  = 2 * 1024 * 1024
	groupNoteMaxRunes   = 200
)

// Hold through the database read or write and publication to OptionMap, so a
// local sync cannot publish an old database snapshot after a successful write.
var groupNotesOptionMutex sync.Mutex

var errGroupNotesObject = common.NewMessage("Group notes must be a JSON object mapping group names to strings")
var errGroupNotesSize = common.NewMessage("Group notes JSON must be at most 2 MiB")
var errGroupNotesSave = common.NewMessage("Could not save group notes")

type groupNote string

func (note *groupNote) UnmarshalJSON(data []byte) error {
	var text *string
	if err := common.Unmarshal(data, &text); err != nil || text == nil {
		return errGroupNotesObject
	}
	trimmed := strings.TrimSpace(*text)
	if utf8.RuneCountInString(trimmed) > groupNoteMaxRunes {
		return common.NewMessage("Each group note must be at most 200 characters")
	}
	*note = groupNote(trimmed)
	return nil
}

func normalizeGroupNotes(raw string) (string, error) {
	if len(raw) > groupNotesMaxBytes {
		return "", errGroupNotesSize
	}
	var notes map[string]groupNote
	if !utf8.ValidString(raw) {
		return "", errGroupNotesObject
	}
	if err := common.UnmarshalJsonStr(raw, &notes); err != nil {
		// JSON decoder diagnostics can include input text; expose only our
		// validation messages from decoding individual notes.
		if message, ok := err.(*common.Message); ok {
			return "", message
		}
		return "", errGroupNotesObject
	}
	if notes == nil {
		return "", errGroupNotesObject
	}
	for name, note := range notes {
		if name == "" || strings.TrimSpace(name) != name {
			return "", common.NewMessage("Group names in group notes must not be empty or have leading or trailing whitespace")
		}
		if note == "" {
			delete(notes, name)
		}
	}
	encoded, err := common.Marshal(notes)
	if err != nil {
		return "", errGroupNotesObject
	}
	if len(encoded) > groupNotesMaxBytes {
		return "", errGroupNotesSize
	}
	return string(encoded), nil
}

// normalizeGroupNotesWrite keeps the caller's batch unchanged, including when
// a transaction fails and the caller retries it.
func normalizeGroupNotesWrite(values map[string]string) (map[string]string, error) {
	raw, ok := values[GroupNotesOptionKey]
	if !ok {
		return values, nil
	}
	value, err := normalizeGroupNotes(raw)
	if err != nil {
		return nil, err
	}
	values = maps.Clone(values)
	values[GroupNotesOptionKey] = value
	return values, nil
}

func publishGroupNotes(value string) {
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	common.OptionMap[GroupNotesOptionKey] = value
}

func saveGroupNotesOption(tx *gorm.DB, value string) error {
	// Even debug SQL logs and driver diagnostics must not include note text.
	return tx.Session(&gorm.Session{Logger: logger.Discard}).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&Option{Key: GroupNotesOptionKey, Value: value}).Error
}
