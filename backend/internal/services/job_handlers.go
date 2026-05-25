package services

import (
	"context"
	"log/slog"

	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/gorm"
)

type JobHandler interface {
	Execute(ctx context.Context, jobID uint64, data database.Job) error
}

type BaseHandler struct {
	db *gorm.DB
}

func (h *BaseHandler) Log(jobID uint64, level, message string, itemID *uint64) {
	err := database.AppendJobLog(h.db, jobID, level, message, itemID)
	if err != nil {
		if itemID != nil {
			slog.Error("Failed to append log", "job_id", jobID, "item_id", *itemID, "error", err)
		} else {
			slog.Error("Failed to append log", "job_id", jobID, "error", err)
		}
	}
}
