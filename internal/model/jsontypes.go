package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// jsonDBType picks a JSON-capable column type per dialect.
func jsonDBType(db *gorm.DB) string {
	switch db.Dialector.Name() {
	case "mysql":
		return "json"
	case "postgres":
		return "jsonb"
	default:
		return "text"
	}
}

func scanJSON(value any, dst any) error {
	var raw []byte
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("unsupported JSON column value %T", value)
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// StringMap stores map[string]string as JSON (resource tags).
type StringMap map[string]string

func (m StringMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(map[string]string(m))
	return string(b), err
}

func (m *StringMap) Scan(value any) error {
	out := map[string]string{}
	if err := scanJSON(value, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

func (StringMap) GormDBDataType(db *gorm.DB, _ *schema.Field) string { return jsonDBType(db) }

// JSONObject stores an arbitrary JSON object (provider specific details).
type JSONObject map[string]any

func (m JSONObject) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(map[string]any(m))
	return string(b), err
}

func (m *JSONObject) Scan(value any) error {
	out := map[string]any{}
	if err := scanJSON(value, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

func (JSONObject) GormDBDataType(db *gorm.DB, _ *schema.Field) string { return jsonDBType(db) }

// StringList stores []string as JSON.
type StringList []string

func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(l))
	return string(b), err
}

func (l *StringList) Scan(value any) error {
	out := []string{}
	if err := scanJSON(value, &out); err != nil {
		return err
	}
	*l = out
	return nil
}

func (StringList) GormDBDataType(db *gorm.DB, _ *schema.Field) string { return jsonDBType(db) }

// TaskError describes one failed collection task of a sync job.
type TaskError struct {
	Region  string `json:"region"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// TaskErrors stores []TaskError as JSON.
type TaskErrors []TaskError

func (l TaskErrors) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]TaskError(l))
	return string(b), err
}

func (l *TaskErrors) Scan(value any) error {
	out := []TaskError{}
	if err := scanJSON(value, &out); err != nil {
		return err
	}
	*l = out
	return nil
}

func (TaskErrors) GormDBDataType(db *gorm.DB, _ *schema.Field) string { return jsonDBType(db) }

// IntMap stores map[string]int as JSON (per-type counts).
type IntMap map[string]int

func (m IntMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(map[string]int(m))
	return string(b), err
}

func (m *IntMap) Scan(value any) error {
	out := map[string]int{}
	if err := scanJSON(value, &out); err != nil {
		return err
	}
	*m = out
	return nil
}

func (IntMap) GormDBDataType(db *gorm.DB, _ *schema.Field) string { return jsonDBType(db) }
