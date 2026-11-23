package app

import "context"

// Timing is diagnostic: a missing observation must never block the reply.
func (a *App) startStage(ctx context.Context, mid, label string) func(string, error) {
	if mid == "" {
		return func(string, error) {}
	}
	var id int64
	err := a.db.QueryRow(ctx, "INSERT INTO message_stages(message_id,label) VALUES($1,$2) RETURNING id", mid, label).Scan(&id)
	if err != nil {
		a.log.Warn("start message timing", "stage", label, "error", err)
		return func(string, error) {}
	}
	return func(detail string, cause error) {
		status := "success"
		if cause != nil {
			status = "failed"
			detail = cause.Error()
		}
		if _, err := a.db.Exec(ctx, "UPDATE message_stages SET finished_at=clock_timestamp(),status=$2,detail=$3 WHERE id=$1", id, status, detail); err != nil {
			a.log.Warn("finish message timing", "stage", label, "error", err)
		}
	}
}
