package database

import (
	"gorm.io/gorm"
)

// AppendJobLog appends a log entry to a job
func AppendJobLog(db *gorm.DB, jobID uint64, level, message string, itemID *uint64) error {
	log := JobLog{
		JobID:     jobID,
		JobItemID: itemID,
		Level:     level,
		Message:   message,
	}
	return db.Create(&log).Error
}
