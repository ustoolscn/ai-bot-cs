package app

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestDecisionHistoryVisibilityAndWindow(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	db := newIntegrationSchema(t, ctx, url)
	a := &App{db: db}
	var bot string
	j := inboxJob{ContextLimit: 20}
	if err := db.QueryRow(ctx, `INSERT INTO bots(name,app_id,app_secret_enc) VALUES('bot','app','secret') RETURNING id::text`).Scan(&bot); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,moderation_cutoff) VALUES('qq',$1,'g','group','2026-01-01') RETURNING id::text`, bot).Scan(&j.ConversationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO blocked_users(bot_id,conversation_id,platform_user_id) VALUES($1,NULL,'global'),($1,$2,'local')`, bot, j.ConversationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO conversation_members(conversation_id,platform_user_id,is_staff) VALUES($1,'staff',true)`, j.ConversationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,sender_id,event_type,status,context_excluded,content,event_at,created_at)
		SELECT 'qq',$1,$2,direction,who,kind,status,hidden,body,'2026-01-01'::date + n * interval '1 second','2026-01-01'::date + seq * interval '1 second'
		FROM (VALUES
		(0,0,'inbound','alice','','received',false,'before cutoff'),
		(1,1,'inbound','alice','','received',false,'业务问题'),
		(2,2,'inbound','bob','','received',false,'群聊'),
		(3,3,'outbound','','AI_RESPONSE','sent',false,repeat('答',400)),
		(4,4,'inbound','staff','','received',false,'人工建议'),
		(5,5,'outbound','','HUMAN_REPLY','sent',false,'请重试'),
		(6,6,'inbound','alice','','received',true,'excluded'),
		(7,7,'outbound','','HUMAN_REPLY','retracted',false,'retracted'),
		(8,8,'inbound','global','','received',false,'global blocked'),
		(9,9,'inbound','local','','received',false,'local blocked'),
		(10,10,'outbound','','PROCESSING_ACK','sent',false,'ack'),
		(11,11,'outbound','','AI_RESPONSE','pending',false,'pending'),
		(12,12,'inbound','alice','','received',false,'还是不行'),
		(12,13,'inbound','alice','','received',false,'later')
		) AS msgs(n,seq,direction,who,kind,status,hidden,body)`, bot, j.ConversationID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT id::text FROM messages WHERE content='还是不行'`).Scan(&j.MessageID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE messages SET platform_message_id='question' WHERE content='业务问题'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE messages SET reply_to_message_id='question' WHERE event_type='AI_RESPONSE' AND status='sent'`); err != nil {
		t.Fatal(err)
	}
	got, err := a.decisionHistory(ctx, j, "alice")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"业务问题", "群聊", strings.Repeat("答", 300) + "…", "人工建议", "请重试"}
	roles := []string{"群成员", "群成员", "机器人", "人工客服", "人工客服"}
	if len(got) != len(want) {
		t.Fatalf("unexpected history: %+v", got)
	}
	for i, turn := range got {
		if turn.Content != want[i] || turn.Role != roles[i] || turn.Answered != (i == 0) || turn.SameSender != (i == 0) {
			t.Fatalf("turn %d: %+v", i, turn)
		}
	}
	for _, limit := range []int{0, 1} {
		j.ContextLimit = limit
		recent, err := a.decisionHistory(ctx, j, "alice")
		if err != nil || len(recent) != len(want) {
			t.Fatalf("routing context at limit %d: %+v, %v", limit, recent, err)
		}
	}
	j.ContextLimit = 2
	got, err = a.decisionHistory(ctx, j, "alice")
	if err != nil || len(got) != 1 || got[0].Content != "请重试" {
		t.Fatalf("limited history: %+v, %v", got, err)
	}
}

func TestSocialOnlyRequiresWholeMessage(t *testing.T) {
	for _, q := range []string{"你好！", "您好", "谢谢你", " Hello! "} {
		if !socialOnly(q) {
			t.Errorf("unnecessary retrieval for %q", q)
		}
	}
	for _, q := range []string{"你好，怎么退款？", "还是不行", "hi, what is the price?", "你好\n用户引用的原文：支付失败"} {
		if socialOnly(q) {
			t.Errorf("business context lost for %q", q)
		}
	}
}

func TestDirectNameCall(t *testing.T) {
	if directNameCall("小花你在吗", []decisionTurn{{Name: "小花", Role: "群成员"}}, "小花") {
		t.Error("ambiguous call to a same-named group member must go to the judge")
	}
	for _, text := range []string{"小花你在吗？", "小花，帮我看下", "小花 你好！", "客服小Q，在不在？"} {
		if !directNameCall(text, nil, "小花", "客服小q") {
			t.Errorf("missed direct call %q", text)
		}
	}
	for _, text := range []string{"你在吗", "小花猫，你在吗", "我问过小花你在吗", "小花今天在线吗", "小花，你在吗是他问的", "“小花，帮我看下”", "谁能转告小花？"} {
		if directNameCall(text, nil, "", "小花") {
			t.Errorf("unexpected forced reply to %q", text)
		}
	}
}
