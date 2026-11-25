package app

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"time"
)

type handoffHours struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

func (h handoffHours) valid() bool {
	for _, s := range []string{h.Start, h.End} {
		t, e := time.Parse("15:04", s)
		if e != nil || t.Format("15:04") != s {
			return false
		}
	}
	return h.Start != h.End
}
func (h handoffHours) open(t time.Time) bool {
	if !h.Enabled {
		return true
	}
	m := t.In(chinaLocation).Format("15:04")
	if h.Start < h.End {
		return m >= h.Start && m < h.End
	}
	return m >= h.Start || m < h.End
}

type hoursClosed string

func (e hoursClosed) Error() string { return string(e) }
func (h handoffHours) notice() hoursClosed {
	return hoursClosed(fmt.Sprintf("目前不在人工服务时间，人工客服每天北京时间 %s 至 %s 在线。您可以在服务时间内再联系人工，也可以继续向我提问。", h.Start, h.End))
}
func (a *App) handoffHoursAPI(c *gin.Context) {
	var h handoffHours
	query := "SELECT handoff_hours FROM bot_services WHERE bot_id=$1 AND id=$2"
	args := []any{c.Param("id"), c.Param("item")}
	if c.Request.Method == "PUT" {
		var in struct {
			Enabled    *bool
			Start, End string
		}
		if c.ShouldBindJSON(&in) != nil || in.Enabled == nil {
			fail(c, 400, "invalid_hours", "请填写 enabled、start、end")
			return
		}
		h = handoffHours{*in.Enabled, in.Start, in.End}
		if !h.valid() {
			fail(c, 400, "invalid_hours", "起止时间须为不同的 HH:mm")
			return
		}
		query = "UPDATE bot_services SET handoff_hours=$3 WHERE bot_id=$1 AND id=$2 RETURNING handoff_hours"
		args = append(args, h)
	}
	err := a.db.QueryRow(c, query, args...).Scan(&h)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "客服不存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "读写服务时间失败")
		return
	}
	ok(c, h)
}
