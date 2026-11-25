package app

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"ai-bot/backend/internal/knowledge"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxArchive = 1 << 30
const maxManifest = 32 << 20
const maxDocument = 10 << 20

type portableDoc struct {
	Name, ContentType, ImageDescription string
	Unindexed                           bool
	Size                                int64
}
type archiveRow map[string]json.RawMessage
type knowledgeArchive struct {
	Version                           int
	Name, Description, EmbeddingModel string
	Documents                         []portableDoc
	Records                           map[string][]archiveRow
}

// Column names are fixed by the archive version. Local account, conversation,
// job and model identifiers are never used to establish destination relations.
type archiveTable struct{ table, columns, parentColumn, parentTable string }

var archiveTables = []archiveTable{
	{"knowledge_entries", "question,aliases,answer,source,status,version,source_refs,hit_count,helpful_count,unhelpful_count,last_hit_at,created_at,updated_at", "", ""},
	{"knowledge_candidates", "question,aliases,answer,source,evidence,confidence,status,review_note,reviewed_at,created_at,updated_at", "entry_id", "knowledge_entries"},
	{"knowledge_gaps", "question,occurrences,status,first_seen_at,last_seen_at", "candidate_id", "knowledge_candidates"},
	{"eval_cases", "question,expected,source,enabled,created_at,updated_at", "entry_id", "knowledge_entries"},
}

func docMember(i int) string { return fmt.Sprintf("files/%d", i) }
func (a *App) exportKnowledge(c *gin.Context) {
	id := c.Param("id")
	if uuid.Validate(id) != nil {
		fail(c, 400, "invalid_request", "知识库编号无效")
		return
	}
	tmp, e := os.CreateTemp("", "kb-*.zip")
	if e != nil {
		fail(c, 500, "export_failed", "创建临时文件失败")
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	tx, e := a.db.BeginTx(c, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		fail(c, 500, "export_failed", "读取知识库失败")
		return
	}
	defer tx.Rollback(c)
	m := knowledgeArchive{Version: 1, Records: map[string][]archiveRow{}}
	e = tx.QueryRow(c, "SELECT name,description,embedding_model FROM knowledge_bases WHERE id=$1", id).Scan(&m.Name, &m.Description, &m.EmbeddingModel)
	if errors.Is(e, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "知识库不存在")
		return
	}
	z := zip.NewWriter(tmp)
	if e == nil {
		e = a.writeArchive(c, tx, z, id, &m)
	}
	if e == nil {
		e = z.Close()
	}
	if e != nil {
		archiveFailure(c, e, "export_failed")
		return
	}
	if e = tx.Commit(c); e != nil {
		fail(c, 500, "export_failed", "读取知识库失败")
		return
	}
	info, e := tmp.Stat()
	if e != nil {
		fail(c, 500, "export_failed", "读取导出文件失败")
		return
	}
	if info.Size() > maxArchive {
		fail(c, 413, "archive_too_large", "文件包超过 1 GiB")
		return
	}
	c.FileAttachment(tmp.Name(), "knowledge-"+id+".zip")
}

var tooLarge = errors.New("archive too large")

func archiveFailure(c *gin.Context, e error, code string) {
	if errors.Is(e, tooLarge) {
		fail(c, 413, "archive_too_large", "超过文件包容量限制")
		return
	}
	if code == "export_failed" {
		fail(c, 500, code, "导出失败，请检查原始文件是否完整")
		return
	}
	fail(c, 400, code, "知识库文件包无效或内容不完整")
}
func (a *App) writeArchive(c *gin.Context, tx pgx.Tx, z *zip.Writer, id string, m *knowledgeArchive) error {
	rows, e := tx.Query(c, "SELECT name,content_type,image_description,status='unindexed',size_bytes,storage_key FROM knowledge_documents WHERE knowledge_base_id=$1 ORDER BY id FOR SHARE", id)
	if e != nil {
		return e
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		var d portableDoc
		var key string
		if e = rows.Scan(&d.Name, &d.ContentType, &d.ImageDescription, &d.Unindexed, &d.Size, &key); e != nil {
			return e
		}
		total += d.Size
		if total > maxArchive || d.Size > maxDocument {
			return tooLarge
		}
		b, e := a.files.Read(c, key)
		if e != nil {
			return e
		}
		if int64(len(b)) != d.Size {
			return io.ErrUnexpectedEOF
		}
		w, e := z.CreateHeader(&zip.FileHeader{Name: docMember(len(m.Documents)), Method: zip.Store})
		if e != nil {
			return e
		}
		if _, e = w.Write(b); e != nil {
			return e
		}
		m.Documents = append(m.Documents, d)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	metaBytes := 0
	for _, t := range archiveTables {
		cols := "id," + t.columns
		if t.parentColumn != "" {
			cols += "," + t.parentColumn
		}
		filter := "knowledge_base_id=$1"
		if t.table == "eval_cases" {
			filter = "entry_id IN (SELECT id FROM knowledge_entries WHERE knowledge_base_id=$1)"
		}
		rows, e := tx.Query(c, "SELECT to_jsonb(r) FROM (SELECT "+cols+" FROM "+t.table+" WHERE "+filter+" ORDER BY id) r", id)
		if e != nil {
			return e
		}
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				rows.Close()
				return e
			}
			metaBytes += len(b)
			if metaBytes > maxManifest {
				rows.Close()
				return tooLarge
			}
			var r archiveRow
			if e = json.Unmarshal(b, &r); e != nil {
				rows.Close()
				return e
			}
			m.Records[t.table] = append(m.Records[t.table], r)
		}
		if e = rows.Err(); e != nil {
			return e
		}
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	if int64(len(b))+total > maxArchive || len(b) > maxManifest {
		return tooLarge
	}
	w, e := z.Create("manifest.json")
	if e != nil {
		return e
	}
	_, e = w.Write(b)
	return e
}
func readMember(f *zip.File, max int64) ([]byte, error) {
	if f == nil {
		return nil, io.ErrUnexpectedEOF
	}
	if f.UncompressedSize64 > uint64(max) {
		return nil, tooLarge
	}
	r, e := f.Open()
	if e != nil {
		return nil, e
	}
	defer r.Close()
	b, e := io.ReadAll(io.LimitReader(r, max+1))
	if int64(len(b)) > max {
		return nil, tooLarge
	}
	return b, e
}
func (a *App) importKnowledge(c *gin.Context) {
	deadline := time.Now().Add(30 * time.Minute)
	controller := http.NewResponseController(c.Writer)
	_ = controller.SetReadDeadline(deadline)
	_ = controller.SetWriteDeadline(deadline)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxArchive+(1<<20))
	if e := c.Request.ParseMultipartForm(8 << 20); e != nil {
		var limit *http.MaxBytesError
		if errors.As(e, &limit) {
			archiveFailure(c, tooLarge, "")
			return
		}
		fail(c, 400, "invalid_archive", "请选择文件包")
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	fh, e := c.FormFile("file")
	if e != nil {
		fail(c, 400, "invalid_archive", "缺少文件包")
		return
	}
	if fh.Size > maxArchive {
		archiveFailure(c, tooLarge, "")
		return
	}
	f, e := fh.Open()
	if e != nil {
		fail(c, 400, "invalid_archive", "读取文件包失败")
		return
	}
	defer f.Close()
	z, e := zip.NewReader(f, fh.Size)
	if e != nil {
		archiveFailure(c, e, "invalid_archive")
		return
	}
	members := map[string]*zip.File{}
	var size uint64
	for _, v := range z.File {
		if members[v.Name] != nil {
			archiveFailure(c, errors.New("duplicate"), "invalid_archive")
			return
		}
		members[v.Name] = v
		if v.UncompressedSize64 > maxArchive-size {
			archiveFailure(c, tooLarge, "")
			return
		}
		size += v.UncompressedSize64
	}
	b, e := readMember(members["manifest.json"], maxManifest)
	var m knowledgeArchive
	if e != nil {
		archiveFailure(c, e, "invalid_archive")
		return
	}
	if json.Unmarshal(b, &m) != nil || m.Version != 1 || len(m.Documents)+1 != len(members) || strings.TrimSpace(m.Name) == "" {
		fail(c, 400, "invalid_archive", "文件包内容或版本无效")
		return
	}
	pid := c.PostForm("embeddingProfileId")
	if uuid.Validate(pid) != nil {
		fail(c, 400, "invalid_embedding_profile", "请选择本机 Embedding 配置")
		return
	}
	tx, e := a.db.Begin(c)
	if e != nil {
		fail(c, 500, "database_error", "无法开始导入")
		return
	}
	defer tx.Rollback(c)
	var model, provider, id string
	e = tx.QueryRow(c, "SELECT model,provider FROM model_profiles WHERE id=$1 AND kind='embedding' AND enabled FOR SHARE", pid).Scan(&model, &provider)
	if e != nil {
		fail(c, 400, "invalid_embedding_profile", "需要启用的 Embedding 配置")
		return
	}
	name := strings.TrimSpace(c.DefaultPostForm("name", m.Name))
	if name == "" {
		fail(c, 400, "invalid_request", "库名不能为空")
		return
	}
	e = tx.QueryRow(c, "INSERT INTO knowledge_bases(name,description,embedding_profile_id,embedding_model) VALUES($1,$2,$3,$4) RETURNING id::text", name, m.Description, pid, model).Scan(&id)
	if e != nil {
		fail(c, 500, "database_error", "创建知识库失败")
		return
	}
	var keys []string
	defer func() {
		for _, k := range keys {
			_ = a.files.Delete(c, k)
		}
	}()
	for i, d := range m.Documents {
		if !knowledge.Supported(d.Name) {
			fail(c, 400, "invalid_archive", "不支持的文档类型")
			return
		}
		if knowledge.IsImage(d.Name) && !d.Unindexed && (provider != "google" || model != "gemini-embedding-2") {
			fail(c, 400, "image_model_required", knowledge.ImageModelRequired)
			return
		}
		data, e := readMember(members[docMember(i)], maxDocument)
		if e != nil {
			archiveFailure(c, e, "invalid_archive")
			return
		}
		if int64(len(data)) != d.Size {
			fail(c, 400, "invalid_archive", "文件长度不符")
			return
		}
		key, e := a.files.Save(c, d.Name, data)
		if e != nil {
			fail(c, 500, "storage_error", "保存原始文件失败")
			return
		}
		keys = append(keys, key)
		status := "pending"
		if d.Unindexed {
			status = "unindexed"
		}
		_, e = tx.Exec(c, "INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes,image_description,status) VALUES($1,$2,$3,$4,$5,$6,$7)", id, d.Name, key, d.ContentType, d.Size, d.ImageDescription, status)
		if e != nil {
			fail(c, 500, "database_error", "保存文档记录失败")
			return
		}
	}
	if e = restoreRecords(c, tx, id, &m); e != nil {
		archiveFailure(c, e, "invalid_archive")
		return
	}
	_, e = tx.Exec(c, "INSERT INTO agent_jobs(kind,payload) SELECT 'index_entry',jsonb_build_object('entryId',id) FROM knowledge_entries WHERE knowledge_base_id=$1 AND status='published'", id)
	if e != nil {
		fail(c, 500, "database_error", "安排索引失败")
		return
	}
	if e = tx.Commit(c); e != nil {
		// A lost commit acknowledgement may mean that the new library is live.
		var serverError *pgconn.PgError
		if !errors.Is(e, pgx.ErrTxCommitRollback) && !errors.As(e, &serverError) && !pgconn.SafeToRetry(e) {
			keys = nil
		}
		fail(c, 500, "database_error", "提交结果未确认，请刷新知识库列表")
		return
	}
	keys = nil
	created(c, gin.H{"id": id, "name": name, "documentCount": len(m.Documents), "entryCount": len(m.Records["knowledge_entries"])})
}
func restoreRecords(c *gin.Context, tx pgx.Tx, kb string, m *knowledgeArchive) error {
	ids := map[string]map[string]string{}
	for _, t := range archiveTables {
		ids[t.table] = map[string]string{}
		cols := t.columns
		if t.table != "eval_cases" {
			cols += ",knowledge_base_id"
		}
		if t.parentColumn != "" {
			cols += "," + t.parentColumn
		}
		stmt := "INSERT INTO " + t.table + "(" + cols + ") SELECT " + cols + " FROM jsonb_populate_record(NULL::" + t.table + ",$1::jsonb) RETURNING id::text"
		for _, r := range m.Records[t.table] {
			var old string
			if json.Unmarshal(r["id"], &old) != nil || uuid.Validate(old) != nil || ids[t.table][old] != "" {
				return errors.New("invalid record id")
			}
			r["knowledge_base_id"], _ = json.Marshal(kb)
			if t.parentColumn != "" {
				var parent string
				_ = json.Unmarshal(r[t.parentColumn], &parent)
				local := ids[t.parentTable][parent]
				if local != "" {
					r[t.parentColumn], _ = json.Marshal(local)
				} else {
					r[t.parentColumn] = json.RawMessage("null")
				}
			}
			b, e := json.Marshal(r)
			if e != nil {
				return e
			}
			var local string
			if e = tx.QueryRow(c, stmt, string(b)).Scan(&local); e != nil {
				return e
			}
			ids[t.table][old] = local
		}
	}
	return nil
}
