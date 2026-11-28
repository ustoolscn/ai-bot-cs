package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type knowledgeMapBoundItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type knowledgeMapDocument struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Status     string    `json:"status"`
	SizeBytes  int64     `json:"sizeBytes"`
	ChunkCount int64     `json:"chunkCount"`
	ImageCount int64     `json:"imageCount"`
	LastError  string    `json:"lastError"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Preview    string    `json:"preview"`
}

type knowledgeMapEntry struct {
	ID             string    `json:"id"`
	Question       string    `json:"question"`
	Answer         string    `json:"answer"`
	Aliases        []string  `json:"aliases"`
	Status         string    `json:"status"`
	IndexStatus    string    `json:"indexStatus"`
	Source         string    `json:"source"`
	HitCount       int64     `json:"hitCount"`
	HelpfulCount   int64     `json:"helpfulCount"`
	UnhelpfulCount int64     `json:"unhelpfulCount"`
	MaterialCount  int64     `json:"materialCount"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type knowledgeMapCandidate struct {
	ID         string    `json:"id"`
	Question   string    `json:"question"`
	Answer     string    `json:"answer"`
	Confidence float64   `json:"confidence"`
	Source     string    `json:"source"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

type knowledgeMapGap struct {
	ID          string    `json:"id"`
	Question    string    `json:"question"`
	Occurrences int64     `json:"occurrences"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

type knowledgeMapCluster struct {
	ID         int      `json:"id"`
	Label      string   `json:"label"`
	Keywords   []string `json:"keywords"`
	Count      int      `json:"count"`
	X          float64  `json:"x"`
	Y          float64  `json:"y"`
	Z          float64  `json:"z"`
	Radius     float64  `json:"radius"`
	ColorIndex int      `json:"colorIndex"`
}

type knowledgeMapPoint struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"` // document | entry | gap
	SourceID       string  `json:"sourceId"`
	Title          string  `json:"title"`
	Snippet        string  `json:"snippet"`
	ChunkIndex     int     `json:"chunkIndex"`
	HasImage       bool    `json:"hasImage"`
	ImageUrl       string  `json:"imageUrl,omitempty"`
	HitCount       int64   `json:"hitCount"`
	HelpfulCount   int64   `json:"helpfulCount"`
	UnhelpfulCount int64   `json:"unhelpfulCount"`
	ClusterID      int     `json:"clusterId"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	Z              float64 `json:"z"`
}

type knowledgeDocumentChunk struct {
	ID         string `json:"id"`
	ChunkIndex int    `json:"chunkIndex"`
	Content    string `json:"content"`
	HasImage   bool   `json:"hasImage"`
	ImageUrl   string `json:"imageUrl,omitempty"`
}

type mapRawVectorItem struct {
	Point  knowledgeMapPoint
	Vector []float64
	Text   string
}

func (a *App) getKBMap(c *gin.Context) {
	kbID := c.Param("id")
	if uuid.Validate(kbID) != nil {
		fail(c, 400, "invalid_id", "知识库编号无效")
		return
	}

	var name, desc, profileID, embeddingModel string
	var docCount, readyDocCount, entryCount, readyEntryCount, chunkCount, unreadNegative int64
	var boundAgentsRaw, boundConvsRaw json.RawMessage
	var isDefaultLearning bool

	err := a.db.QueryRow(c, `
		SELECT k.name, k.description, COALESCE(k.embedding_profile_id::text,''), k.embedding_model,
			COALESCE((SELECT json_agg(json_build_object('id',s.id::text,'name',s.name) ORDER BY s.name,s.id)
				FROM bot_services s LEFT JOIN system_settings g ON g.key='agent_settings'
				WHERE COALESCE(s.settings->'knowledgeBaseIds',g.value->'knowledgeBaseIds') ? k.id::text
				   OR COALESCE(s.settings->>'learningKnowledgeBaseId',g.value->>'learningKnowledgeBaseId')=k.id::text),'[]'),
			COALESCE((SELECT json_agg(json_build_object('id',c.id::text,'name',COALESCE(NULLIF(c.name,''),c.platform_id),'type',c.type) ORDER BY c.name,c.id)
				FROM conversations c LEFT JOIN bot_services s ON s.id=c.service_id LEFT JOIN system_settings g ON g.key='agent_settings'
				WHERE COALESCE(s.settings->'knowledgeBaseIds',g.value->'knowledgeBaseIds') ? k.id::text
				   OR COALESCE(s.settings->>'learningKnowledgeBaseId',g.value->>'learningKnowledgeBaseId')=k.id::text
				   OR EXISTS (SELECT FROM conversation_knowledge_bases b WHERE b.conversation_id=c.id AND b.knowledge_base_id=k.id)),'[]'),
			(SELECT count(*) FROM knowledge_documents d WHERE d.knowledge_base_id=k.id),
			(SELECT count(*) FROM knowledge_documents d WHERE d.knowledge_base_id=k.id AND d.status='ready'),
			(SELECT count(*) FROM knowledge_entries e WHERE e.knowledge_base_id=k.id AND e.status='published'),
			(SELECT count(*) FROM knowledge_entries e WHERE e.knowledge_base_id=k.id AND e.status='published' AND e.index_status='ready'),
			(SELECT count(*) FROM knowledge_chunks ch WHERE ch.knowledge_base_id=k.id),
			(SELECT count(*) FROM message_feedback f WHERE f.knowledge_base_id=k.id AND NOT f.is_read AND f.rating NOT IN ('good','helpful')),
			COALESCE((SELECT value->>'learningKnowledgeBaseId' FROM system_settings WHERE key='agent_settings'), '') = k.id::text
		FROM knowledge_bases k WHERE k.id=$1
	`, kbID).Scan(
		&name, &desc, &profileID, &embeddingModel,
		&boundAgentsRaw, &boundConvsRaw,
		&docCount, &readyDocCount, &entryCount, &readyEntryCount, &chunkCount, &unreadNegative, &isDefaultLearning,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "知识库不存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "读取知识库全景失败")
		return
	}

	boundAgents := make([]knowledgeMapBoundItem, 0)
	_ = json.Unmarshal(boundAgentsRaw, &boundAgents)
	boundConvs := make([]knowledgeMapBoundItem, 0)
	_ = json.Unmarshal(boundConvsRaw, &boundConvs)

	// 文档及首段预览
	documents := make([]knowledgeMapDocument, 0)
	docRows, err := a.db.Query(c, `
		SELECT d.id::text, d.name, d.status, d.size_bytes,
			COALESCE(dc.chunk_count, 0), COALESCE(dc.image_count, 0),
			COALESCE(d.last_error, ''), d.updated_at,
			COALESCE(left(dc.first_content, 160), '')
		FROM knowledge_documents d
		LEFT JOIN LATERAL (
			SELECT count(*) AS chunk_count,
				count(ci.chunk_id) AS image_count,
				(SELECT c2.content FROM knowledge_chunks c2 WHERE c2.document_id=d.id ORDER BY c2.chunk_index ASC LIMIT 1) AS first_content
			FROM knowledge_chunks c LEFT JOIN knowledge_chunk_images ci ON ci.chunk_id=c.id
			WHERE c.document_id=d.id
		) dc ON true
		WHERE d.knowledge_base_id=$1
		ORDER BY d.created_at DESC
	`, kbID)
	if err != nil {
		fail(c, 500, "database_error", "读取知识文档失败")
		return
	}
	for docRows.Next() {
		var d knowledgeMapDocument
		if err := docRows.Scan(&d.ID, &d.Name, &d.Status, &d.SizeBytes, &d.ChunkCount, &d.ImageCount, &d.LastError, &d.UpdatedAt, &d.Preview); err == nil {
			ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(d.Name), "."))
			if ext == "JPEG" {
				ext = "JPG"
			}
			if ext == "" {
				ext = "TXT"
			}
			d.Type = ext
			documents = append(documents, d)
		}
	}
	docRows.Close()

	// 问答条目
	entries := make([]knowledgeMapEntry, 0)
	entryRows, err := a.db.Query(c, `
		SELECT e.id::text, e.question, e.answer, e.aliases, e.status, e.index_status, e.source,
			e.hit_count, e.helpful_count, e.unhelpful_count,
			jsonb_array_length(COALESCE(e.material_meta, '[]'::jsonb)),
			e.updated_at
		FROM knowledge_entries e
		WHERE e.knowledge_base_id=$1 AND e.status<>'archived'
		ORDER BY e.hit_count DESC, e.updated_at DESC
		LIMIT 200
	`, kbID)
	if err != nil {
		fail(c, 500, "database_error", "读取问答条目失败")
		return
	}
	for entryRows.Next() {
		var e knowledgeMapEntry
		var aliasesRaw json.RawMessage
		if err := entryRows.Scan(&e.ID, &e.Question, &e.Answer, &aliasesRaw, &e.Status, &e.IndexStatus, &e.Source, &e.HitCount, &e.HelpfulCount, &e.UnhelpfulCount, &e.MaterialCount, &e.UpdatedAt); err == nil {
			e.Aliases = make([]string, 0)
			_ = json.Unmarshal(aliasesRaw, &e.Aliases)
			entries = append(entries, e)
		}
	}
	entryRows.Close()

	// 待审核候选
	candidates := make([]knowledgeMapCandidate, 0)
	candRows, err := a.db.Query(c, `
		SELECT k.id::text, k.question, k.answer, k.confidence, k.source, k.status, k.created_at
		FROM knowledge_candidates k
		WHERE k.status='pending' AND (k.knowledge_base_id=$1 OR ($2 AND k.knowledge_base_id IS NULL))
		ORDER BY k.created_at DESC
		LIMIT 30
	`, kbID, isDefaultLearning)
	if err != nil {
		fail(c, 500, "database_error", "读取待审核候选失败")
		return
	}
	for candRows.Next() {
		var item knowledgeMapCandidate
		if err := candRows.Scan(&item.ID, &item.Question, &item.Answer, &item.Confidence, &item.Source, &item.Status, &item.CreatedAt); err == nil {
			candidates = append(candidates, item)
		}
	}
	candRows.Close()

	// 没答上的问题（以及它们的向量）
	gaps := make([]knowledgeMapGap, 0)
	gapItems := make([]mapRawVectorItem, 0)
	gapRows, err := a.db.Query(c, `
		SELECT g.id::text, g.question, g.occurrences, g.last_seen_at, COALESCE(g.embedding::text, '')
		FROM knowledge_gaps g
		WHERE g.status='open' AND g.knowledge_base_id=$1
		ORDER BY g.occurrences DESC, g.last_seen_at DESC
		LIMIT 40
	`, kbID)
	if err != nil {
		fail(c, 500, "database_error", "读取知识缺口失败")
		return
	}
	for gapRows.Next() {
		var g knowledgeMapGap
		var vecText string
		if err := gapRows.Scan(&g.ID, &g.Question, &g.Occurrences, &g.LastSeenAt, &vecText); err == nil {
			gaps = append(gaps, g)
			if vec := parsePGVector(vecText); len(vec) > 0 {
				gapItems = append(gapItems, mapRawVectorItem{
					Point: knowledgeMapPoint{
						ID:       "gap:" + g.ID,
						Kind:     "gap",
						SourceID: g.ID,
						Title:    truncateText(g.Question, 48),
						Snippet:  truncateText(g.Question, 220),
						HitCount: g.Occurrences,
					},
					Vector: vec,
					Text:   g.Question,
				})
			}
		}
	}
	gapRows.Close()

	// 向量分块采样：优先全部已索引问答分块，再按文档轮询均匀采样至上限 360 条
	rawItems := make([]mapRawVectorItem, 0, 360)
	chunkRows, err := a.db.Query(c, `
		WITH entry_chunks AS (
			SELECT c.id, c.document_id, c.entry_id, c.chunk_index, c.content, c.embedding::text AS vec,
				EXISTS(SELECT 1 FROM knowledge_chunk_images ci WHERE ci.chunk_id=c.id) AS has_image,
				COALESCE(e.question, '') AS title,
				COALESCE(e.hit_count, 0) AS hit_count,
				COALESCE(e.helpful_count, 0) AS helpful_count,
				COALESCE(e.unhelpful_count, 0) AS unhelpful_count,
				0 AS priority, 0 AS rn
			FROM knowledge_chunks c
			JOIN knowledge_entries e ON e.id=c.entry_id
			WHERE c.knowledge_base_id=$1
		),
		doc_chunks AS (
			SELECT c.id, c.document_id, c.entry_id, c.chunk_index, c.content, c.embedding::text AS vec,
				EXISTS(SELECT 1 FROM knowledge_chunk_images ci WHERE ci.chunk_id=c.id) AS has_image,
				COALESCE(d.name, '文档片段') AS title,
				0::bigint AS hit_count,
				0::bigint AS helpful_count,
				0::bigint AS unhelpful_count,
				1 AS priority,
				row_number() OVER (PARTITION BY c.document_id ORDER BY c.chunk_index) AS rn
			FROM knowledge_chunks c
			JOIN knowledge_documents d ON d.id=c.document_id
			WHERE c.knowledge_base_id=$1
		)
		SELECT id::text, COALESCE(document_id::text,''), COALESCE(entry_id::text,''), chunk_index, content, vec, has_image, title, hit_count, helpful_count, unhelpful_count
		FROM (
			SELECT * FROM entry_chunks
			UNION ALL
			SELECT * FROM doc_chunks
		) merged
		ORDER BY priority ASC, rn ASC, chunk_index ASC
		LIMIT 360
	`, kbID)
	if err != nil {
		fail(c, 500, "database_error", "读取知识分块向量失败")
		return
	}
	baseDim := 0
	for chunkRows.Next() {
		var chunkID, docID, entryID, content, vecText, title string
		var chunkIdx int
		var hasImage bool
		var hits, helpful, unhelpful int64
		if err := chunkRows.Scan(&chunkID, &docID, &entryID, &chunkIdx, &content, &vecText, &hasImage, &title, &hits, &helpful, &unhelpful); err == nil {
			vec := parsePGVector(vecText)
			if len(vec) == 0 {
				continue
			}
			if baseDim == 0 {
				baseDim = len(vec)
			} else if len(vec) != baseDim {
				continue
			}
			kind := "document"
			sourceID := docID
			if entryID != "" {
				kind = "entry"
				sourceID = entryID
			}
			imageURL := ""
			if hasImage {
				imageURL = fmt.Sprintf("/api/knowledge-bases/%s/chunks/%s/image", kbID, chunkID)
			}
			rawItems = append(rawItems, mapRawVectorItem{
				Point: knowledgeMapPoint{
					ID:             chunkID,
					Kind:           kind,
					SourceID:       sourceID,
					Title:          truncateText(title, 48),
					Snippet:        truncateText(content, 220),
					ChunkIndex:     chunkIdx,
					HasImage:       hasImage,
					ImageUrl:       imageURL,
					HitCount:       hits,
					HelpfulCount:   helpful,
					UnhelpfulCount: unhelpful,
				},
				Vector: vec,
				Text:   title + " " + content,
			})
		}
	}
	chunkRows.Close()

	// 把维度匹配的 gap 也一起投影到同一张星图
	for _, gItem := range gapItems {
		if baseDim == 0 {
			baseDim = len(gItem.Vector)
		}
		if len(gItem.Vector) == baseDim && len(rawItems) < 400 {
			rawItems = append(rawItems, gItem)
		}
	}

	vecs := make([][]float64, len(rawItems))
	texts := make([]string, len(rawItems))
	for i, item := range rawItems {
		vecs[i] = item.Vector
		texts[i] = item.Text
	}
	coords := projectVectorsPCA3D(vecs)
	assignments, clusters := clusterPoints3D(coords, texts)

	points := make([]knowledgeMapPoint, len(rawItems))
	for i, item := range rawItems {
		pt := item.Point
		if i < len(coords) {
			pt.X = coords[i][0]
			pt.Y = coords[i][1]
			pt.Z = coords[i][2]
		}
		if i < len(assignments) {
			pt.ClusterID = assignments[i]
		}
		points[i] = pt
	}

	ok(c, gin.H{
		"id":                     kbID,
		"name":                   name,
		"description":            desc,
		"embeddingProfileId":     profileID,
		"embeddingModel":         embeddingModel,
		"documentCount":          docCount,
		"readyDocumentCount":     readyDocCount,
		"entryCount":             entryCount,
		"readyEntryCount":        readyEntryCount,
		"chunkCount":             chunkCount,
		"unreadNegativeFeedback": unreadNegative,
		"boundAgents":            boundAgents,
		"boundConversations":     boundConvs,
		"documents":              documents,
		"entries":                entries,
		"candidates":             candidates,
		"gaps":                   gaps,
		"clusters":               clusters,
		"points":                 points,
	})
}

func (a *App) listDocumentChunks(c *gin.Context) {
	kbID := c.Param("id")
	docID := c.Param("documentId")
	if uuid.Validate(kbID) != nil || uuid.Validate(docID) != nil {
		fail(c, 400, "invalid_id", "参数编号无效")
		return
	}
	rows, err := a.db.Query(c, `
		SELECT c.id::text, c.chunk_index, c.content,
			EXISTS(SELECT 1 FROM knowledge_chunk_images ci WHERE ci.chunk_id=c.id) AS has_image
		FROM knowledge_chunks c
		WHERE c.knowledge_base_id=$1 AND c.document_id=$2
		ORDER BY c.chunk_index ASC
		LIMIT 200
	`, kbID, docID)
	if err != nil {
		fail(c, 500, "database_error", "读取文档分块失败")
		return
	}
	defer rows.Close()

	chunks := make([]knowledgeDocumentChunk, 0)
	for rows.Next() {
		var item knowledgeDocumentChunk
		if err := rows.Scan(&item.ID, &item.ChunkIndex, &item.Content, &item.HasImage); err == nil {
			if item.HasImage {
				item.ImageUrl = fmt.Sprintf("/api/knowledge-bases/%s/chunks/%s/image", kbID, item.ID)
			}
			chunks = append(chunks, item)
		}
	}
	ok(c, chunks)
}

func truncateText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

func parsePGVector(raw string) []float64 {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '[' || raw[len(raw)-1] != ']' {
		return nil
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	out := make([]float64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		out = append(out, v)
	}
	return out
}

func projectVectorsPCA2D(vecs [][]float64) [][2]float64 {
	coords3 := projectVectorsPCA3D(vecs)
	out := make([][2]float64, len(coords3))
	for i, c := range coords3 {
		out[i] = [2]float64{c[0], c[1]}
	}
	return out
}

func projectVectorsPCA3D(vecs [][]float64) [][3]float64 {
	n := len(vecs)
	if n == 0 {
		return [][3]float64{}
	}
	if n == 1 {
		return [][3]float64{{0, 0, 0}}
	}
	if n == 2 {
		return [][3]float64{{-0.5, 0, 0}, {0.5, 0, 0}}
	}
	dim := 0
	for _, v := range vecs {
		if len(v) > 0 {
			dim = len(v)
			break
		}
	}
	if dim == 0 {
		return make([][3]float64, n)
	}

	// 计算均值并中心化
	mean := make([]float64, dim)
	validCount := 0
	for _, v := range vecs {
		if len(v) != dim {
			continue
		}
		validCount++
		for j, val := range v {
			mean[j] += val
		}
	}
	if validCount == 0 {
		return make([][3]float64, n)
	}
	for j := range mean {
		mean[j] /= float64(validCount)
	}

	centered := make([][]float64, n)
	for i, v := range vecs {
		row := make([]float64, dim)
		if len(v) == dim {
			for j, val := range v {
				row[j] = val - mean[j]
			}
		}
		centered[i] = row
	}

	var axes [][]float64
	deflate := func(src [][]float64, axis []float64) [][]float64 {
		res := make([][]float64, len(src))
		for i, row := range src {
			dot := dotProduct(row, axis)
			d := make([]float64, dim)
			for j, val := range row {
				d[j] = val - dot*axis[j]
			}
			res[i] = d
		}
		return res
	}

	// 依次提取 3 个正交主成分
	pc1 := dominantAxis(centered, dim, nil)
	if pc1 == nil {
		return make([][3]float64, n)
	}
	axes = append(axes, pc1)

	mat2 := deflate(centered, pc1)
	pc2 := dominantAxis(mat2, dim, axes)
	if pc2 != nil {
		axes = append(axes, pc2)
	}

	var pc3 []float64
	if pc2 != nil && dim >= 3 {
		mat3 := deflate(mat2, pc2)
		pc3 = dominantAxis(mat3, dim, axes)
	}

	coords := make([][3]float64, n)
	maxX, maxY, maxZ := 0.0, 0.0, 0.0
	for i, row := range centered {
		x := dotProduct(row, pc1)
		var y, z float64
		if pc2 != nil {
			y = dotProduct(row, pc2)
		}
		if pc3 != nil {
			z = dotProduct(row, pc3)
		}
		if math.IsNaN(x) || math.IsInf(x, 0) {
			x = 0
		}
		if math.IsNaN(y) || math.IsInf(y, 0) {
			y = 0
		}
		if math.IsNaN(z) || math.IsInf(z, 0) {
			z = 0
		}
		coords[i] = [3]float64{x, y, z}
		if ax := math.Abs(x); ax > maxX {
			maxX = ax
		}
		if ay := math.Abs(y); ay > maxY {
			maxY = ay
		}
		if az := math.Abs(z); az > maxZ {
			maxZ = az
		}
	}
	for i := range coords {
		if maxX > 1e-9 {
			coords[i][0] = coords[i][0] / maxX * 0.86
		} else {
			coords[i][0] = 0
		}
		if maxY > 1e-9 {
			coords[i][1] = coords[i][1] / maxY * 0.86
		} else {
			coords[i][1] = 0
		}
		if maxZ > 1e-9 {
			coords[i][2] = coords[i][2] / maxZ * 0.86
		} else {
			coords[i][2] = 0
		}
	}
	return coords
}

func dominantAxis(matrix [][]float64, dim int, orthoAxes [][]float64) []float64 {
	// 选择模最大的行作为确定性初值，避免合成向量陷入正交奇异点
	bestNorm := 0.0
	var initVec []float64
	for _, row := range matrix {
		var sq float64
		for _, v := range row {
			sq += v * v
		}
		if sq > bestNorm {
			bestNorm = sq
			initVec = row
		}
	}
	if bestNorm < 1e-12 || initVec == nil {
		return nil
	}
	u := make([]float64, dim)
	copy(u, initVec)
	for _, axis := range orthoAxes {
		proj := dotProduct(u, axis)
		for j := range u {
			u[j] -= proj * axis[j]
		}
	}
	if !normalizeVec(u) {
		return nil
	}

	next := make([]float64, dim)
	for iter := 0; iter < 20; iter++ {
		for j := range next {
			next[j] = 0
		}
		for _, row := range matrix {
			s := dotProduct(row, u)
			if s == 0 {
				continue
			}
			for j, val := range row {
				next[j] += s * val
			}
		}
		for _, axis := range orthoAxes {
			proj := dotProduct(next, axis)
			for j := range next {
				next[j] -= proj * axis[j]
			}
		}
		if !normalizeVec(next) {
			break
		}
		copy(u, next)
	}
	return u
}

func dotProduct(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func normalizeVec(v []float64) bool {
	var sq float64
	for _, val := range v {
		sq += val * val
	}
	if sq < 1e-18 || math.IsNaN(sq) || math.IsInf(sq, 0) {
		return false
	}
	inv := 1.0 / math.Sqrt(sq)
	for i := range v {
		v[i] *= inv
	}
	return true
}

func clusterPoints2D(coords [][2]float64, texts []string) ([]int, []knowledgeMapCluster) {
	coords3 := make([][3]float64, len(coords))
	for i, c := range coords {
		coords3[i] = [3]float64{c[0], c[1], 0}
	}
	return clusterPoints3D(coords3, texts)
}

func clusterPoints3D(coords [][3]float64, texts []string) ([]int, []knowledgeMapCluster) {
	n := len(coords)
	if n == 0 {
		return []int{}, []knowledgeMapCluster{}
	}
	k := 1
	if n > 3 {
		k = int(math.Round(math.Sqrt(float64(n) / 3.0)))
		if k < 2 {
			k = 2
		}
		if k > 6 {
			k = 6
		}
		if k > n {
			k = n
		}
	}

	// 确定性最远点初始化质心（三维欧氏距离）
	centers := make([][3]float64, 0, k)
	used := make([]bool, n)
	firstIdx := 0
	maxOriginDist := -1.0
	for i, pt := range coords {
		d := pt[0]*pt[0] + pt[1]*pt[1] + pt[2]*pt[2]
		if d > maxOriginDist {
			maxOriginDist = d
			firstIdx = i
		}
	}
	centers = append(centers, coords[firstIdx])
	used[firstIdx] = true

	for len(centers) < k {
		bestIdx := -1
		bestMinDist := -1.0
		for i, pt := range coords {
			if used[i] {
				continue
			}
			minD := math.MaxFloat64
			for _, c := range centers {
				dx := pt[0] - c[0]
				dy := pt[1] - c[1]
				dz := pt[2] - c[2]
				if d := dx*dx + dy*dy + dz*dz; d < minD {
					minD = d
				}
			}
			if minD > bestMinDist {
				bestMinDist = minD
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			break
		}
		centers = append(centers, coords[bestIdx])
		used[bestIdx] = true
	}

	actualK := len(centers)
	assign := make([]int, n)
	for iter := 0; iter < 15; iter++ {
		changed := false
		for i, pt := range coords {
			bestC := 0
			bestD := math.MaxFloat64
			for cIdx, c := range centers {
				dx := pt[0] - c[0]
				dy := pt[1] - c[1]
				dz := pt[2] - c[2]
				if d := dx*dx + dy*dy + dz*dz; d < bestD {
					bestD = d
					bestC = cIdx
				}
			}
			if assign[i] != bestC {
				assign[i] = bestC
				changed = true
			}
		}
		if !changed && iter > 0 {
			break
		}
		sums := make([][3]float64, actualK)
		counts := make([]int, actualK)
		for i, pt := range coords {
			c := assign[i]
			sums[c][0] += pt[0]
			sums[c][1] += pt[1]
			sums[c][2] += pt[2]
			counts[c]++
		}
		for c := 0; c < actualK; c++ {
			if counts[c] > 0 {
				centers[c][0] = sums[c][0] / float64(counts[c])
				centers[c][1] = sums[c][1] / float64(counts[c])
				centers[c][2] = sums[c][2] / float64(counts[c])
			}
		}
	}

	// 重排非空簇为连续 ID 0..M-1
	clusterTexts := make([][]string, actualK)
	clusterIndices := make([][]int, actualK)
	for i := 0; i < n; i++ {
		c := assign[i]
		clusterIndices[c] = append(clusterIndices[c], i)
		if i < len(texts) {
			clusterTexts[c] = append(clusterTexts[c], texts[i])
		}
	}

	oldToNew := make(map[int]int, actualK)
	nonEmptyTexts := make([][]string, 0, actualK)
	nonEmptyOld := make([]int, 0, actualK)
	for c := 0; c < actualK; c++ {
		if len(clusterIndices[c]) > 0 {
			oldToNew[c] = len(nonEmptyOld)
			nonEmptyOld = append(nonEmptyOld, c)
			nonEmptyTexts = append(nonEmptyTexts, clusterTexts[c])
		}
	}
	for i := range assign {
		assign[i] = oldToNew[assign[i]]
	}

	allKeywords := extractAllClusterKeywords(nonEmptyTexts)
	clusters := make([]knowledgeMapCluster, 0, len(nonEmptyOld))
	for newID, oldID := range nonEmptyOld {
		idxs := clusterIndices[oldID]
		cx, cy, cz := centers[oldID][0], centers[oldID][1], centers[oldID][2]
		maxR := 0.16
		for _, idx := range idxs {
			dx := coords[idx][0] - cx
			dy := coords[idx][1] - cy
			dz := coords[idx][2] - cz
			r := math.Sqrt(dx*dx+dy*dy+dz*dz) + 0.08
			if r > maxR {
				maxR = r
			}
		}
		kws := allKeywords[newID]
		label := fmt.Sprintf("主题簇 #%d", newID+1)
		if len(kws) > 0 {
			limit := 2
			if len(kws) < limit {
				limit = len(kws)
			}
			label = strings.Join(kws[:limit], " · ")
		}
		clusters = append(clusters, knowledgeMapCluster{
			ID:         newID,
			Label:      label,
			Keywords:   kws,
			Count:      len(idxs),
			X:          cx,
			Y:          cy,
			Z:          cz,
			Radius:     math.Min(maxR, 0.78),
			ColorIndex: newID % 6,
		})
	}
	return assign, clusters
}

var mapStopWords = map[string]bool{
	"可以": true, "如何": true, "怎么": true, "什么": true, "为什么": true, "问题": true,
	"答案": true, "需要": true, "如果": true, "这个": true, "那个": true, "没有": true,
	"我们": true, "你们": true, "他们": true, "以及": true, "或者": true, "使用": true,
	"进行": true, "通过": true, "时候": true, "是否": true, "支持": true, "文档": true,
	"片段": true, "一下": true, "就是": true, "不是": true, "已经": true, "这样": true,
	"因为": true, "所以": true, "但是": true, "然后": true, "请问": true, "这里": true,
	"其中": true, "其他": true, "一般": true, "无法": true, "可能": true, "以下": true,
}

func extractAllClusterKeywords(clusterTexts [][]string) [][]string {
	m := len(clusterTexts)
	if m == 0 {
		return nil
	}
	clusterTF := make([]map[string]int, m)
	globalDF := make(map[string]int)

	for cIdx, docs := range clusterTexts {
		tf := make(map[string]int)
		seenInCluster := make(map[string]bool)
		for _, text := range docs {
			for tok := range tokenizeKeywords(text) {
				tf[tok]++
				if !seenInCluster[tok] {
					seenInCluster[tok] = true
					globalDF[tok]++
				}
			}
		}
		clusterTF[cIdx] = tf
	}

	result := make([][]string, m)
	for cIdx := 0; cIdx < m; cIdx++ {
		type scored struct {
			word  string
			score float64
		}
		candidates := make([]scored, 0, len(clusterTF[cIdx]))
		for word, freq := range clusterTF[cIdx] {
			df := globalDF[word]
			idf := 1.0 + math.Log(float64(m+1)/(float64(df)+0.5))
			lenBonus := 1.0
			if len([]rune(word)) >= 3 {
				lenBonus = 1.15
			}
			candidates = append(candidates, scored{word: word, score: float64(freq) * idf * lenBonus})
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].score != candidates[j].score {
				return candidates[i].score > candidates[j].score
			}
			return candidates[i].word < candidates[j].word
		})

		chosen := make([]string, 0, 3)
		for _, cand := range candidates {
			overlap := false
			for _, prev := range chosen {
				if strings.Contains(prev, cand.word) || strings.Contains(cand.word, prev) {
					overlap = true
					break
				}
			}
			if !overlap {
				chosen = append(chosen, cand.word)
				if len(chosen) >= 3 {
					break
				}
			}
		}
		result[cIdx] = chosen
	}
	return result
}

func tokenizeKeywords(text string) map[string]struct{} {
	tokens := make(map[string]struct{})
	var hanBuf []rune
	var latinBuf []rune

	flushHan := func() {
		if len(hanBuf) >= 2 {
			for i := 0; i < len(hanBuf); i++ {
				for size := 2; size <= 3 && i+size <= len(hanBuf); size++ {
					w := string(hanBuf[i : i+size])
					if !mapStopWords[w] {
						tokens[w] = struct{}{}
					}
				}
			}
		}
		hanBuf = hanBuf[:0]
	}
	flushLatin := func() {
		if len(latinBuf) >= 2 {
			w := strings.ToLower(string(latinBuf))
			// 过滤纯数字
			hasLetter := false
			for _, r := range w {
				if r >= 'a' && r <= 'z' {
					hasLetter = true
					break
				}
			}
			if hasLetter {
				tokens[w] = struct{}{}
			}
		}
		latinBuf = latinBuf[:0]
	}

	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			hanBuf = append(hanBuf, r)
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_':
			flushHan()
			latinBuf = append(latinBuf, r)
		default:
			flushHan()
			flushLatin()
		}
	}
	flushHan()
	flushLatin()
	return tokens
}
