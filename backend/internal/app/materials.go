package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"ai-bot/backend/internal/domain"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maxMaterials = 9
const maxVideo = 25 << 20
const maxMaterialTotal = 64 << 20

type material struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	MIME     string `json:"mime"`
	Filename string `json:"filename"`
	Size     int    `json:"sizeBytes"`
	Data     []byte `json:"-"`
}
type materialPlan struct {
	order []string
	files []material
}

func validateMaterial(name string, data []byte) (material, error) {
	m := material{ID: uuid.NewString(), Filename: path.Base(strings.ReplaceAll(name, "\\", "/")), Size: len(data), Data: data}
	if m.Filename == "." || m.Filename == "/" || len(m.Filename) > 255 {
		return m, errors.New("素材文件名无效")
	}
	if http.DetectContentType(data) == "video/mp4" {
		if len(data) > maxVideo || !validMP4(data) {
			return m, errors.New("MP4 录屏无效或超过 25 MiB")
		}
		m.Kind = "video"
		m.MIME = "video/mp4"
		return m, nil
	}
	if len(data) > maxReplyImage {
		return m, errors.New("图片超过 5 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width) > 16000000/int64(cfg.Height) {
		return m, errors.New("图片无效或超过 1600 万像素")
	}
	switch format {
	case "jpeg", "png":
	default:
		return m, errors.New("不支持的图片格式")
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return m, errors.New("图片损坏")
	}
	m.Kind = "image"
	m.MIME = "image/" + format
	return m, nil
}

// ISO BMFF video track handler: moov/trak/mdia/hdlr, handler_type "vide".
func validMP4(data []byte) bool {
	var movie, media bool
	ok := visitMP4Boxes(data, func(kind string, payload []byte) {
		switch kind {
		case "moov":
			movie = hasVideoHandler(payload, []string{"trak", "mdia", "hdlr"})
		case "mdat":
			media = media || len(payload) > 0
		}
	})
	return ok && movie && media
}
func hasVideoHandler(data []byte, levels []string) bool {
	if len(levels) == 0 {
		return len(data) >= 12 && string(data[8:12]) == "vide"
	}
	found := false
	ok := visitMP4Boxes(data, func(kind string, payload []byte) {
		if !found && kind == levels[0] {
			found = hasVideoHandler(payload, levels[1:])
		}
	})
	return ok && found
}
func visitMP4Boxes(data []byte, visit func(string, []byte)) bool {
	for len(data) >= 8 {
		size := uint64(binary.BigEndian.Uint32(data))
		header := 8
		if size == 1 {
			if len(data) < 16 {
				return false
			}
			size = binary.BigEndian.Uint64(data[8:])
			header = 16
		}
		if size == 0 {
			size = uint64(len(data))
		}
		if size < uint64(header) || size > uint64(len(data)) {
			return false
		}
		visit(string(data[4:8]), data[header:size])
		data = data[size:]
	}
	return len(data) == 0
}

func readMaterialPlan(form *multipart.Form) (*materialPlan, error) {
	files := form.File["files"]
	if len(files) > maxMaterials || len(form.File) > 1 || (len(form.File) > 0 && files == nil) {
		return nil, errors.New("最多 9 个素材，文件字段为 files")
	}
	p := &materialPlan{}
	if order, ok := form.Value["materials"]; ok {
		if len(order) != 1 || json.Unmarshal([]byte(order[0]), &p.order) != nil || p.order == nil {
			return nil, errors.New("materials 必须是 JSON 字符串数组")
		}
	} else {
		for i := range files {
			p.order = append(p.order, fmt.Sprintf("new:%d", i))
		}
	}
	if len(p.order) > maxMaterials {
		return nil, errors.New("最多 9 个素材")
	}
	for _, fh := range files {
		if fh.Size > maxVideo {
			return nil, errors.New("单个素材超过 25 MiB")
		}
		f, err := fh.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		m, err := validateMaterial(fh.Filename, data)
		if err != nil {
			return nil, err
		}
		p.files = append(p.files, m)
	}
	return p, nil
}
func (p *materialPlan) resolve(old []material) ([]material, error) {
	if p == nil {
		return old, nil
	}
	available := map[string]material{}
	for i, m := range p.files {
		available["new:"+strconv.Itoa(i)] = m
	}
	for _, m := range old {
		available["keep:"+m.ID] = m
	}
	result := make([]material, 0, len(p.order))
	total := 0
	if len(p.order) > maxMaterials {
		return nil, errors.New("最多 9 个素材")
	}
	for _, token := range p.order {
		m, ok := available[token]
		if !ok {
			return nil, errors.New("素材不存在、重复或不属于本条记录")
		}
		delete(available, token)
		result = append(result, m)
		total += m.Size
	}
	for token := range available {
		if strings.HasPrefix(token, "new:") {
			return nil, errors.New("有上传文件未列入 materials")
		}
	}
	if total > maxMaterialTotal {
		return nil, errors.New("素材总计超过 64 MiB")
	}
	return result, nil
}
func materialColumns(items []material) ([]byte, [][]byte) {
	if items == nil {
		items = []material{}
	}
	meta, _ := json.Marshal(items)
	data := make([][]byte, len(items))
	for i, m := range items {
		data[i] = m.Data
	}
	return meta, data
}
func loadMaterials(ctx context.Context, db rowQuerier, table, id string, lock bool) ([]material, error) {
	q := "SELECT material_meta,material_data FROM " + table + " WHERE id=$1"
	if lock {
		q += " FOR UPDATE"
	}
	var items []material
	var data [][]byte
	if err := db.QueryRow(ctx, q, id).Scan(&items, &data); err != nil {
		return nil, err
	}
	if len(items) != len(data) {
		return nil, errors.New("素材记录损坏")
	}
	for i := range items {
		items[i].Data = data[i]
	}
	return items, nil
}
func materialListSQL(alias, collection string) string {
	return "coalesce((select jsonb_agg(item||jsonb_build_object('url','/api/knowledge/" + collection + "/'||" + alias + ".id::text||'/materials/'||(item->>'id')) order by pos) from jsonb_array_elements(" + alias + ".material_meta) with ordinality as x(item,pos)),'[]')"
}
func (a *App) materialPreview(table string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var mime, name string
		var data []byte
		err := a.db.QueryRow(c, "SELECT item->>'mime',item->>'filename',material_data[pos::int] FROM "+table+" CROSS JOIN LATERAL jsonb_array_elements(material_meta) WITH ORDINALITY x(item,pos) WHERE id=$1 AND item->>'id'=$2", c.Param("id"), c.Param("materialId")).Scan(&mime, &name, &data)
		if err != nil {
			fail(c, 404, "not_found", "素材不存在")
			return
		}
		c.Header("Content-Type", mime)
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		http.ServeContent(c.Writer, c.Request, name, time.Time{}, bytes.NewReader(data))
	}
}
func (a *App) storeMaterials(ctx context.Context, items []material) ([]domain.ContentPart, error) {
	var parts []domain.ContentPart
	for _, m := range items {
		ext := imageExtension(m.MIME)
		if m.Kind == "video" {
			ext = ".mp4"
		}
		key, err := a.images.Save(ctx, "material"+ext, m.Data)
		if err != nil {
			for _, saved := range parts {
				_ = a.images.Delete(ctx, saved.StorageKey)
			}
			return nil, err
		}
		parts = append(parts, domain.ContentPart{Type: m.Kind, Filename: m.Filename, ContentType: m.MIME, StorageKey: key, SizeBytes: int64(m.Size)})
	}
	return parts, nil
}

func uploadedPlan(items []material) *materialPlan {
	p := &materialPlan{files: items}
	for i := range items {
		p.order = append(p.order, "new:"+strconv.Itoa(i))
	}
	return p
}
