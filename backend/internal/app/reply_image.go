package app

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

const maxReplyImage = 5 << 20

type replyImage struct {
	mime string
	data []byte
	plan *materialPlan
}

// Text-only clients can keep using JSON. Multipart requests carry the same fields.
func bindReplyImage(c *gin.Context, dst any) (replyImage, error) {
	var img replyImage
	if c.ContentType() != "multipart/form-data" {
		return img, c.ShouldBindJSON(dst)
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxMaterialTotal+(1<<20))
	if err := c.Request.ParseMultipartForm(maxReplyImage); err != nil {
		return img, errors.New("表单无效或素材总计超过 64 MiB")
	}
	form := c.Request.MultipartForm
	defer form.RemoveAll()
	if err := c.ShouldBind(dst); err != nil {
		return img, errors.New("表单字段无效")
	}
	if form.File["files"] != nil || form.Value["materials"] != nil {
		if form.File["image"] != nil {
			return img, errors.New("请使用 files 上传素材")
		}
		p, err := readMaterialPlan(form)
		return replyImage{plan: p}, err
	}
	if len(form.File) == 0 {
		return img, nil
	}
	files := form.File["image"]
	if len(files) != 1 || len(form.File) != 1 || files[0].Size > maxReplyImage {
		return img, errors.New("请在 image 字段上传一张 5 MiB 以内的图片")
	}
	f, err := files[0].Open()
	if err != nil {
		return img, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return img, err
	}
	return decodeReplyImage(data)
}

func decodeReplyImage(data []byte) (replyImage, error) {
	var img replyImage
	if len(data) > maxReplyImage {
		return img, errors.New("图片超过 5 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width) > 16000000/int64(cfg.Height) {
		return img, errors.New("图片须为 PNG 或 JPEG，最多 1600 万像素")
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return img, errors.New("图片数据损坏")
	}
	return replyImage{mime: "image/" + format, data: data}, nil
}

func (a *App) replyImagePreview(table string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var mime string
		var data []byte
		if err := a.db.QueryRow(c, "SELECT image_mime,image_data FROM "+table+" WHERE id=$1 AND image_data IS NOT NULL", c.Param("id")).Scan(&mime, &data); err != nil {
			fail(c, 404, "not_found", "配图不存在")
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Data(200, mime, data)
	}
}
