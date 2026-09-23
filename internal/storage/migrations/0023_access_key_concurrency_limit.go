package migrations

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const ID0023 = "0023_access_key_concurrency_limit"

const (
	concurrencyTable0023      = "access_keys"
	concurrencyColumn0023     = "max_concurrent"
	concurrencyConstraint0023 = "chk_access_key_max_concurrent"
)

// Up0023 用独立且原子的列 DDL 保持 MySQL 中断后的安全恢复。
func Up0023(db *gorm.DB) error {
	if err := ValidateRecoverable0023(db); err != nil {
		return err
	}
	if !db.Migrator().HasColumn(concurrencyTable0023, concurrencyColumn0023) {
		statement := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s BIGINT NOT NULL DEFAULT 0 CONSTRAINT %s CHECK (%s >= 0)",
			quoteConcurrencyIdentifier0023(db, concurrencyTable0023),
			quoteConcurrencyIdentifier0023(db, concurrencyColumn0023),
			quoteConcurrencyIdentifier0023(db, concurrencyConstraint0023),
			quoteConcurrencyIdentifier0023(db, concurrencyColumn0023),
		)
		if err := db.Exec(statement).Error; err != nil {
			return fmt.Errorf("add %s.%s: %w", concurrencyTable0023, concurrencyColumn0023, err)
		}
	}
	return Validate0023(db)
}

func ValidateRecoverable0023(db *gorm.DB) error {
	if !db.Migrator().HasTable(concurrencyTable0023) {
		return fmt.Errorf("table %q is missing", concurrencyTable0023)
	}
	if !db.Migrator().HasColumn(concurrencyTable0023, concurrencyColumn0023) {
		return nil
	}
	return validateConcurrencyColumn0023(db)
}

func Validate0023(db *gorm.DB) error {
	if !db.Migrator().HasColumn(concurrencyTable0023, concurrencyColumn0023) {
		return fmt.Errorf("%s.%s is missing", concurrencyTable0023, concurrencyColumn0023)
	}
	return validateConcurrencyColumn0023(db)
}

func validateConcurrencyColumn0023(db *gorm.DB) error {
	if err := validateConcurrencyType0023(db); err != nil {
		return err
	}
	if !db.Migrator().HasConstraint(concurrencyTable0023, concurrencyConstraint0023) {
		return fmt.Errorf("%s constraint is missing", concurrencyConstraint0023)
	}
	return validateConcurrencyValues0023(db)
}

func validateConcurrencyType0023(db *gorm.DB) error {
	columns, err := db.Migrator().ColumnTypes(concurrencyTable0023)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", concurrencyColumn0023, err)
	}
	for _, column := range columns {
		if !strings.EqualFold(column.Name(), concurrencyColumn0023) {
			continue
		}
		if !strings.Contains(strings.ToLower(column.DatabaseTypeName()), "int") {
			return fmt.Errorf("%s is not integer", concurrencyColumn0023)
		}
		if nullable, known := column.Nullable(); !known || nullable {
			return fmt.Errorf("%s is nullable", concurrencyColumn0023)
		}
		defaultValue, known := column.DefaultValue()
		if strings.EqualFold(db.Dialector.Name(), "sqlite") {
			// SQLite 驱动解析列尾约束时可能把 CHECK 也并入默认值，读取数据库元数据。
			if err := db.Raw("SELECT dflt_value FROM pragma_table_info(?) WHERE name = ?", concurrencyTable0023, concurrencyColumn0023).Scan(&defaultValue).Error; err != nil {
				return fmt.Errorf("inspect %s default: %w", concurrencyColumn0023, err)
			}
			known = defaultValue != ""
		}
		defaultValue = strings.Trim(strings.Split(defaultValue, "::")[0], "()' \"")
		value, parseErr := strconv.ParseInt(defaultValue, 10, 64)
		if !known || parseErr != nil || value != 0 {
			return fmt.Errorf("%s has invalid default %q (known %t)", concurrencyColumn0023, defaultValue, known)
		}
		return nil
	}
	return fmt.Errorf("%s is missing", concurrencyColumn0023)
}

func validateConcurrencyValues0023(db *gorm.DB) error {
	var invalid int64
	if err := db.Table(concurrencyTable0023).
		Where(concurrencyColumn0023 + " IS NULL OR " + concurrencyColumn0023 + " < 0").
		Count(&invalid).Error; err != nil {
		return fmt.Errorf("validate %s values: %w", concurrencyColumn0023, err)
	}
	if invalid != 0 {
		return fmt.Errorf("%s contains invalid values", concurrencyColumn0023)
	}
	return nil
}

func quoteConcurrencyIdentifier0023(db *gorm.DB, value string) string {
	if strings.EqualFold(db.Dialector.Name(), "mysql") {
		return "`" + value + "`"
	}
	return `"` + value + `"`
}
