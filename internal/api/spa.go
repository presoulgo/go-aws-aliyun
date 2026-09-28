package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

const notBuiltHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>云枢</title></head>
<body style="font-family:sans-serif;padding:48px;color:#151821">
<h1 style="font-size:20px">前端尚未构建</h1>
<p>请在项目根目录执行 <code>make web</code> 后重新编译，或在开发时运行 <code>cd web &amp;&amp; npm run dev</code>。</p>
<p>API 已在 <code>/api/v1</code> 下提供服务。</p></body></html>`

// spaHandler serves built assets and falls back to index.html for client-side
// routes. Unknown /api paths get a JSON 404.
func spaHandler(files fs.FS) gin.HandlerFunc {
	var index []byte
	if files != nil {
		index, _ = fs.ReadFile(files, "index.html")
	}
	var fileServer http.Handler
	if files != nil {
		fileServer = http.FileServer(http.FS(files))
	}
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if p == "/api" || strings.HasPrefix(p, "/api/") {
			fail(c, http.StatusNotFound, "接口不存在")
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			fail(c, http.StatusMethodNotAllowed, "不支持的请求方法")
			return
		}
		name := strings.TrimPrefix(path.Clean(p), "/")
		if files != nil && name != "" && name != "index.html" {
			if st, err := fs.Stat(files, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					c.Header("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
			// A missing file with an extension is a real 404, not a route.
			if path.Ext(name) != "" {
				c.Status(http.StatusNotFound)
				return
			}
		}
		if len(index) == 0 {
			c.Data(http.StatusServiceUnavailable, "text/html; charset=utf-8", []byte(notBuiltHTML))
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	}
}
