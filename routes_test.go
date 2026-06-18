package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"necore/controller/middleware"
	"necore/dao"
	"necore/database"
	"necore/model"
	"necore/service"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type testEnv struct {
	app        *fiber.App
	adminToken string
	userToken  string
	tmpDir     string
}

type testResponse struct {
	StatusCode int
	Body       []byte
	Header     http.Header
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(tmpDir, "data"), 0o755))
	must(t, os.MkdirAll(filepath.Join(tmpDir, "contents"), 0o755))

	envContent := "SECRET=unit-test-secret\nBOT_LOG_BUFFER_SIZE=100\n"
	must(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(envContent), 0o600))

	oldWd, err := os.Getwd()
	must(t, err)
	must(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})

	t.Setenv("SECRET", "unit-test-secret")
	t.Setenv("BOT_LOG_BUFFER_SIZE", "100")

	database.ConnectSqlite()

	// 测试会故意访问不存在的节点和 token；关闭 GORM 的 record not found 日志，
	// 避免把预期分支误看成测试故障。
	setGormLoggerSilent(database.GetUserDatabase())
	setGormLoggerSilent(database.GetArticleDatabase())
	setGormLoggerSilent(database.GetServerDatabase())
	setGormLoggerSilent(database.GetDocumentDatabase())
	setGormLoggerSilent(database.GetBotTokenDatabase())

	// 必须在 Windows 删除 TempDir 前关闭 SQLite 连接池，否则数据库文件会被锁定。
	t.Cleanup(func() {
		closeGormDB(t, database.GetUserDatabase())
		closeGormDB(t, database.GetArticleDatabase())
		closeGormDB(t, database.GetServerDatabase())
		closeGormDB(t, database.GetDocumentDatabase())
		closeGormDB(t, database.GetBotTokenDatabase())
	})

	must(t, dao.AddUserByUsername("admin", "admin-pass"))
	must(t, dao.AddUserByUsername("alice", "alice-pass"))

	must(t, database.GetUserDatabase().
		Model(&model.User{}).
		Where("username = ?", "admin").
		Updates(model.User{
			Group:  `["admin","news_admin","server_admin","document_admin","bot_admin"]`,
			Tags:   `[]`,
			Avatar: "admin-avatar",
		}).Error)

	must(t, database.GetUserDatabase().
		Model(&model.User{}).
		Where("username = ?", "alice").
		Updates(model.User{
			Group:  `[]`,
			Tags:   `[]`,
			Avatar: "alice-avatar",
		}).Error)

	adminToken, err := dao.CreateToken(model.User{
		Username: "admin",
		Group:    `["admin","news_admin","server_admin","document_admin","bot_admin"]`,
		Tags:     `[]`,
	})
	must(t, err)

	userToken, err := dao.CreateToken(model.User{
		Username: "alice",
		Group:    `[]`,
		Tags:     `[]`,
	})
	must(t, err)

	app := fiber.New(fiber.Config{BodyLimit: 512 * 1024 * 1024})
	registerRoutes(app)
	t.Cleanup(func() {
		_ = app.Shutdown()
	})

	return &testEnv{
		app:        app,
		adminToken: adminToken,
		userToken:  userToken,
		tmpDir:     tmpDir,
	}
}

func setGormLoggerSilent(db *gorm.DB) {
	if db != nil {
		db.Logger = logger.Default.LogMode(logger.Silent)
	}
}

func closeGormDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Errorf("get underlying SQL DB: %v", err)
		return
	}
	if err := sqlDB.Close(); err != nil {
		t.Errorf("close SQL DB: %v", err)
	}
}

func registerRoutes(app *fiber.App) {
	api := app.Group("/necore")
	api.Get("/slogan", service.SloganHandler)

	authGroup := api.Group("/auth")
	authGroup.Get("/status", middleware.AuthNeeded(), service.GetStatus)
	authGroup.Post("/login", service.Login)
	authGroup.Post("/register", middleware.AuthNeeded(), service.AddUser)
	authGroup.Get("/user/:id", service.GetUserInfo)
	authGroup.Get("/avatar/:id", service.GetUserAvatar)
	authGroup.Get("/userlist", service.GetUserList)
	authGroup.Delete("/user/:id", middleware.AuthNeeded(), service.DeleteUser)
	authGroup.Post("/password", middleware.AuthNeeded(), service.UpdateUserPassword)
	authGroup.Post("/avatar", middleware.AuthNeeded(), service.UpdateUserAvatar)
	authGroup.Patch("/user", middleware.AuthNeeded(), service.UpdateUserInfo)

	articleGroup := api.Group("/news")
	articleGroup.Get("/total/:target", service.GetArticleCountByCategory)
	articleGroup.Post("/list", service.GetArticleList)
	articleGroup.Get("/detail/:id", service.GetArticleById)
	articleGroup.Patch("/:id", middleware.AuthNeeded(), service.UpdateArticle)
	articleGroup.Post("/upload/:id", middleware.AuthNeeded(), service.UploadArticleFile)
	articleGroup.Delete("/upload/:id", middleware.AuthNeeded(), service.DeleteArticleFile)
	articleGroup.Post("/create", middleware.AuthNeeded(), service.CreateArticle)
	articleGroup.Delete("/:id", middleware.AuthNeeded(), service.DeleteArticle)

	serverGroup := api.Group("/server")
	serverGroup.Get("/", service.GetServerList)
	serverGroup.Post("/status", service.GetServerStatus)
	serverGroup.Get("/create", middleware.AuthNeeded(), service.AddServer)
	serverGroup.Delete("/:id", middleware.AuthNeeded(), service.DeleteServer)
	serverGroup.Patch("/", middleware.AuthNeeded(), service.UpdateServer)

	documentGroup := api.Group("/documents")
	documentGroup.Delete("/node/:id", middleware.AuthNeeded(), service.DeleteDocumentNode)
	documentGroup.Post("/node/:id", middleware.AuthNeeded(), service.UpdateDocumentNodeParentId)
	documentGroup.Put("/node/:id", middleware.AuthNeeded(), service.UpdateDocumentNodeContent)
	documentGroup.Patch("/node/:id", middleware.AuthNeeded(), service.UpdateDocumentNodeName)
	documentGroup.Post("/node", middleware.AuthNeeded(), service.CreateDocumentNode)
	documentGroup.Get("/layer/private/:parentId", middleware.AuthNeeded(), service.GetDocumentNodeChildrenPrivate)
	documentGroup.Get("/layer/:parentId", service.GetDocumentNodeChildren)
	documentGroup.Get("/private/:id", middleware.AuthNeeded(), service.GetDocumentNodeContentPrivate)
	documentGroup.Get("/:id", service.GetDocumentNodeContent)
	documentGroup.Post("/upload/:id", middleware.AuthNeeded(), service.UploadDocumentFile)
	documentGroup.Delete("/upload/:id", middleware.AuthNeeded(), service.DeleteDocumentFile)
	api.Static("/contents", "./contents")

	botGroup := api.Group("/bots")
	botGroup.Post("/token", middleware.AuthNeeded(), service.CreateBotToken)
	botGroup.Get("/token", middleware.AuthNeeded(), service.GetBotTokenList)
	botGroup.Get("/token/:id", middleware.AuthNeeded(), service.GetBotToken)
	botGroup.Delete("/token/:id", middleware.AuthNeeded(), service.DeleteBotToken)
	botGroup.Get("/status", middleware.AuthNeeded(), service.GetWSStatus)
	botGroup.Get("/ws/updates", websocket.New(service.HandleWSConnection))
	botGroup.Delete("/ws/kick/:session_id", middleware.AuthNeeded(), service.KickConnection)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func doJSON(t *testing.T, env *testEnv, method, path, token string, body any) testResponse {
	t.Helper()

	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		must(t, err)
		r = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return executeRequest(t, env, req)
}

func doRaw(t *testing.T, env *testEnv, method, path, token, contentType, body string) testResponse {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return executeRequest(t, env, req)
}

func doMultipartFile(t *testing.T, env *testEnv, path, token, field, filename, content string) testResponse {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, filename)
	must(t, err)
	_, err = part.Write([]byte(content))
	must(t, err)
	must(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return executeRequest(t, env, req)
}

func executeRequest(t *testing.T, env *testEnv, req *http.Request) testResponse {
	t.Helper()

	resp, err := env.app.Test(req, -1)
	must(t, err)

	// 立即读取并关闭响应体。Fiber 的静态文件响应在 Windows 上会保持文件句柄；
	// 不关闭会导致后续 os.Remove 和 TempDir 清理报 ERROR_SHARING_VIOLATION。
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	must(t, readErr)
	must(t, closeErr)

	return testResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
		Header:     resp.Header.Clone(),
	}
}

func decodeBody(t *testing.T, resp testResponse) map[string]any {
	t.Helper()
	var got map[string]any
	must(t, json.Unmarshal(resp.Body, &got))
	return got
}

func assertStatus(t *testing.T, resp testResponse, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d, body = %s", resp.StatusCode, want, string(resp.Body))
	}
}

func TestPublicAndAuthRoutes(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/slogan", "", nil), http.StatusOK)

	// 当前 jwt 中间件对无 Authorization 头实际返回 401，而不是 400。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", "", nil), http.StatusUnauthorized)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/status", env.adminToken, nil), http.StatusOK)

	loginResp := doJSON(t, env, http.MethodPost, "/necore/auth/login", "", fiber.Map{
		"username": "admin",
		"password": "admin-pass",
	})
	assertStatus(t, loginResp, http.StatusOK)
	loginBody := decodeBody(t, loginResp)
	if loginBody["token"] == "" || loginBody["user"] == nil {
		t.Fatalf("login response should contain token and user, got %#v", loginBody)
	}

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/login", "", fiber.Map{
		"username": "admin",
		"password": "wrong",
	}), http.StatusUnauthorized)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/register", env.userToken, fiber.Map{
		"username": "bob",
		"password": "p",
	}), http.StatusForbidden)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/register", env.adminToken, fiber.Map{
		"username": "bob",
		"password": "p",
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/user/admin", "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/avatar/admin", "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/auth/userlist", "", nil), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/password", env.userToken, fiber.Map{
		"id":           "admin",
		"new_password": "new",
	}), http.StatusForbidden)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/password", env.userToken, fiber.Map{
		"id":           "alice",
		"new_password": "new",
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/avatar", env.userToken, fiber.Map{
		"username": "admin",
		"avatar":   "x",
	}), http.StatusForbidden)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/auth/avatar", env.userToken, fiber.Map{
		"username": "alice",
		"avatar":   "new-avatar",
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/auth/user", env.userToken, fiber.Map{
		"username": "alice",
		"group":    []string{},
		"Tags":     []any{},
	}), http.StatusForbidden)

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/auth/user", env.adminToken, fiber.Map{
		"username": "alice",
		"group":    []string{"document_admin"},
		"Tags":     []any{},
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/auth/user/alice", env.userToken, nil), http.StatusForbidden)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/auth/user/bob", env.adminToken, nil), http.StatusOK)
}

func TestNewsRoutes(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/news/total/notice", "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/news/list", "", fiber.Map{
		"target":    "notice",
		"page":      1,
		"page_size": 10,
		"pin":       false,
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/news/create", env.userToken, nil), http.StatusForbidden)

	createResp := doJSON(t, env, http.MethodPost, "/necore/news/create", env.adminToken, nil)
	assertStatus(t, createResp, http.StatusOK)
	articleID, _ := decodeBody(t, createResp)["id"].(string)
	if articleID == "" {
		t.Fatal("create article should return id")
	}

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/news/"+articleID, env.adminToken, fiber.Map{
		"entity": fiber.Map{
			"pin":     true,
			"title":   "Title",
			"brief":   "Brief",
			"date":    "2026-01-01",
			"endDate": "",
			"image":   "",
		},
		"content":    []fiber.Map{{"type": "markdown", "content": "hello"}},
		"category":   "notice",
		"doesNotify": false,
	}), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/news/detail/"+articleID, "", nil), http.StatusOK)
	assertStatus(t, doMultipartFile(t, env, "/necore/news/upload/"+articleID, env.adminToken, "file", "hello.txt", "hello"), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/news/upload/"+articleID, env.adminToken, fiber.Map{
		"url": "/contents/" + articleID + "/hello.txt",
	}), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/news/"+articleID, env.adminToken, nil), http.StatusOK)
}

func TestServerRoutes(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/server/", "", nil), http.StatusOK)
	assertStatus(t, doRaw(t, env, http.MethodPost, "/necore/server/status", "", "application/json", "{"), http.StatusBadRequest)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/server/create", env.userToken, nil), http.StatusForbidden)

	createResp := doJSON(t, env, http.MethodGet, "/necore/server/create", env.adminToken, nil)
	assertStatus(t, createResp, http.StatusOK)
	serverID, _ := decodeBody(t, createResp)["id"].(string)
	if serverID == "" {
		t.Fatal("create server should return id")
	}

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/server/", env.adminToken, fiber.Map{
		"id":           serverID,
		"name":         "S1",
		"icon":         "icon",
		"description":  "desc",
		"realtime":     false,
		"onlineMapUrl": "https://example.test/map",
		"serverUrl":    "example.test:25565",
	}), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/server/"+serverID, env.adminToken, nil), http.StatusOK)
}

func TestDocumentRoutes(t *testing.T) {
	env := setupTestEnv(t)

	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/documents/node", env.userToken, fiber.Map{
		"parentId": "root",
		"isFolder": true,
		"private":  false,
		"name":     "Root",
	}), http.StatusForbidden)

	parentResp := doJSON(t, env, http.MethodPost, "/necore/documents/node", env.adminToken, fiber.Map{
		"parentId": "root",
		"isFolder": true,
		"private":  false,
		"name":     "Destination",
	})
	assertStatus(t, parentResp, http.StatusOK)
	parentID, _ := decodeBody(t, parentResp)["id"].(string)
	if parentID == "" {
		t.Fatal("create parent document node should return id")
	}

	createResp := doJSON(t, env, http.MethodPost, "/necore/documents/node", env.adminToken, fiber.Map{
		"parentId": "root",
		"isFolder": false,
		"private":  false,
		"name":     "Doc",
	})
	assertStatus(t, createResp, http.StatusOK)
	nodeID, _ := decodeBody(t, createResp)["id"].(string)
	if nodeID == "" {
		t.Fatal("create document node should return id")
	}

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/layer/root", "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/layer/private/root", env.adminToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/"+nodeID, "", nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/documents/private/"+nodeID, env.adminToken, nil), http.StatusOK)

	assertStatus(t, doJSON(t, env, http.MethodPatch, "/necore/documents/node/"+nodeID, env.adminToken, fiber.Map{"name": "Renamed"}), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodPut, "/necore/documents/node/"+nodeID, env.adminToken, fiber.Map{
		"private": false,
		"content": []fiber.Map{{"type": "markdown", "content": "body"}},
	}), http.StatusOK)

	// 使用真实存在的父节点，避免 GORM 输出无意义的 record not found 日志。
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/documents/node/"+nodeID, env.adminToken, fiber.Map{
		"parentId": parentID,
	}), http.StatusOK)

	assertStatus(t, doMultipartFile(t, env, "/necore/documents/upload/"+nodeID, env.adminToken, "file", "doc.txt", "file body"), http.StatusOK)

	// 不通过 Fiber Static 读取随后需要删除的同一个文件。
	// Fiber/fasthttp 在 Windows 下可能让 SendFile 的文件句柄存活到请求上下文回收，
	// 即使 net/http 响应体已读取并关闭，立即 os.Remove 仍可能得到 ERROR_SHARING_VIOLATION。
	uploadedPath := filepath.Join(env.tmpDir, "contents", nodeID, "doc.txt")
	uploadedBody, err := os.ReadFile(uploadedPath)
	must(t, err)
	if string(uploadedBody) != "file body" {
		t.Fatalf("uploaded document body = %q, want %q", string(uploadedBody), "file body")
	}

	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/documents/upload/"+nodeID, env.adminToken, fiber.Map{
		"url": "/contents/" + nodeID + "/doc.txt",
	}), http.StatusOK)
	if _, err := os.Stat(uploadedPath); !os.IsNotExist(err) {
		t.Fatalf("uploaded file should be deleted, stat err = %v", err)
	}

	// 仍覆盖静态文件路由，但请求不存在的文件，避免 Windows 文件句柄影响后续删除。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/contents/not-found.txt", "", nil), http.StatusNotFound)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/documents/node/"+nodeID, env.adminToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/documents/node/"+parentID, env.adminToken, nil), http.StatusOK)
}

func TestBotRoutes(t *testing.T) {
	env := setupTestEnv(t)

	// Token 管理接口确实要求 bot_admin。
	assertStatus(t, doJSON(t, env, http.MethodPost, "/necore/bots/token", env.userToken, nil), http.StatusForbidden)

	createResp := doJSON(t, env, http.MethodPost, "/necore/bots/token", env.adminToken, nil)
	assertStatus(t, createResp, http.StatusOK)
	createBody := decodeBody(t, createResp)
	tokenObj, ok := createBody["token"].(map[string]any)
	if !ok || tokenObj["token"] == "" {
		t.Fatalf("create bot token should return token object, got %#v", createBody)
	}

	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/bots/token", env.adminToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/bots/token/missing", env.adminToken, nil), http.StatusInternalServerError)

	// 当前源码只要求“已登录”，没有 bot_admin 权限检查。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/bots/status", env.userToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/bots/ws/kick/not-exist", env.userToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/bots/token/missing", env.adminToken, nil), http.StatusOK)
}

func TestSecurityRegression_FileDeletePathTraversalIsCurrentlyPossible(t *testing.T) {
	env := setupTestEnv(t)

	victim := filepath.Join(env.tmpDir, "victim.txt")
	must(t, os.WriteFile(victim, []byte("do not delete"), 0o644))

	resp := doJSON(t, env, http.MethodDelete, "/necore/documents/upload/anything", env.adminToken, fiber.Map{
		"url": "victim.txt",
	})
	assertStatus(t, resp, http.StatusOK)

	if _, err := os.Stat(victim); !os.IsNotExist(err) {
		t.Fatalf("expected vulnerable handler to delete arbitrary relative file; stat err = %v", err)
	}
}

func TestSecurityRegression_BotDashboardAvailableToAnyAuthenticatedUser(t *testing.T) {
	env := setupTestEnv(t)

	// 该测试记录当前安全缺陷：普通登录用户也能查看 bot 状态并调用 kick。
	assertStatus(t, doJSON(t, env, http.MethodGet, "/necore/bots/status", env.userToken, nil), http.StatusOK)
	assertStatus(t, doJSON(t, env, http.MethodDelete, "/necore/bots/ws/kick/arbitrary-session", env.userToken, nil), http.StatusOK)
}

func TestSecurityRegression_PrivateUserDataIsPubliclyEnumerable(t *testing.T) {
	env := setupTestEnv(t)

	resp := doJSON(t, env, http.MethodGet, "/necore/auth/userlist", "", nil)
	assertStatus(t, resp, http.StatusOK)

	if !strings.Contains(string(resp.Body), "admin") || !strings.Contains(string(resp.Body), "alice") {
		t.Fatalf("expected public user list to expose usernames, body=%s", string(resp.Body))
	}
}
