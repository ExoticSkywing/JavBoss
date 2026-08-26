package models

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

var ErrInvalidJavCode = errors.New("JAV code has no canonical letters or digits")

// NormalizeJavCode returns the durable identity key for a JAV work. Display
// separators and case are deliberately excluded so variants such as IPX-001,
// ipx_001, and IPX001 resolve to the same row. JavDB's FC2-PPV alias is also
// folded into the canonical FC2 identity.
func NormalizeJavCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var normalized strings.Builder
	normalized.Grow(len(value))
	for _, char := range value {
		if (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			normalized.WriteRune(char)
		}
	}
	result := normalized.String()
	if strings.HasPrefix(result, "FC2PPV") {
		result = "FC2" + strings.TrimPrefix(result, "FC2PPV")
	}
	return result
}

// BeforeCreate keeps direct GORM inserts, including fixtures and scanner
// writes, aligned with the database-level canonical uniqueness invariant.
func (item *Jav) BeforeCreate(_ *gorm.DB) error {
	if item == nil {
		return ErrInvalidJavCode
	}
	item.NormalizedCode = NormalizeJavCode(item.Code)
	if item.NormalizedCode == "" {
		return ErrInvalidJavCode
	}
	return nil
}
