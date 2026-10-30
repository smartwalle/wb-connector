package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"wb-connector/internal/domain/oauthdomain"

	"github.com/smartwalle/rx"
	"github.com/smartwalle/rx/router"
)

// stubOAuthService 是 oauthdomain.Service 的测试替身。
//
// 各用例只关心自己用得上的方法，其余返回零值。
type stubOAuthService struct {
	// validateErr 是 ValidateAuthorizeRequest 的返回值。
	validateErr error

	// issueCodeErr 是 IssueAuthCode 的返回值。
	issueCodeErr error

	// issueTokenErr 是 IssueToken 的返回值。
	issueTokenErr error

	// revokeErr 是 RevokeToken 的返回值。
	revokeErr error

	// client 是 RegisterClient 返回的客户端。
	client *oauthdomain.Client
}

func (s *stubOAuthService) Authenticate(ctx context.Context, req oauthdomain.AuthenticateRequest) (string, error) {
	return "demo", nil
}

func (s *stubOAuthService) RegisterClient(ctx context.Context, req oauthdomain.RegisterRequest) (*oauthdomain.Client, error) {
	if s.client != nil {
		return s.client, nil
	}
	return &oauthdomain.Client{ClientID: "client_test", RedirectURIs: req.RedirectURIs}, nil
}

func (s *stubOAuthService) ValidateAuthorizeRequest(ctx context.Context, req oauthdomain.AuthorizeRequest) error {
	return s.validateErr
}

func (s *stubOAuthService) IssueAuthCode(ctx context.Context, req oauthdomain.AuthorizeRequest) (*oauthdomain.AuthCode, error) {
	if s.issueCodeErr != nil {
		return nil, s.issueCodeErr
	}
	return &oauthdomain.AuthCode{Code: "code_test", ClientID: req.ClientID, RedirectURI: req.RedirectURI}, nil
}

func (s *stubOAuthService) IssueToken(ctx context.Context, req oauthdomain.TokenRequest) (*oauthdomain.Token, error) {
	if s.issueTokenErr != nil {
		return nil, s.issueTokenErr
	}
	return &oauthdomain.Token{AccessToken: "at_test", RefreshToken: "rt_test", TokenType: "Bearer"}, nil
}

func (s *stubOAuthService) Issuer() string {
	return "http://127.0.0.1:8080"
}

func (s *stubOAuthService) Resource() string {
	return "http://127.0.0.1:8080/mcp"
}

func (s *stubOAuthService) VerifyToken(ctx context.Context, accessToken string) (*oauthdomain.Token, error) {
	return &oauthdomain.Token{AccessToken: accessToken}, nil
}

func (s *stubOAuthService) RevokeToken(ctx context.Context, req oauthdomain.RevokeRequest) error {
	return s.revokeErr
}

// newTestServer 搭一个真实的 rx 引擎并注册 OAuthHandler 的路由。
//
// 走真实引擎而不是直接构造 Context，是为了连中间件与响应写出逻辑一起覆盖。
func newTestServer(t *testing.T, service oauthdomain.Service) http.Handler {
	t.Helper()
	var engine = rx.New()
	var r = router.New()
	NewMetadataHandler(service).RegisterRoutes(r)
	NewOAuthHandler(service).RegisterRoutes(r)
	engine.UseRouter(r)
	return engine
}

// do 发起一次请求并返回响应记录。
func do(t *testing.T, handler http.Handler, method string, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req = httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	var rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// validQuery 是一组合法的授权请求参数。
const validQuery = "client_id=client_test&redirect_uri=" +
	"workbuddy%3A%2F%2Fworkbuddy%2Fmcp%2Fcustom-mcp%253Awb-connector%2Foauth%2Fcallback" +
	"&code_challenge=abc&code_challenge_method=S256&state=xyz"

// TestAuthorizeErrorsRenderHTML 验证授权端点的错误用 HTML 输出。
//
// 授权端点是在浏览器里打开的，出错时必须返回可读页面而不是裸 JSON。
func TestAuthorizeErrorsRenderHTML(t *testing.T) {
	var cases = []struct {
		name       string
		query      string
		service    *stubOAuthService
		wantStatus int
		wantCode   string
	}{
		{
			name:       "缺少 client_id",
			query:      "redirect_uri=http%3A%2F%2F127.0.0.1%2Fcb",
			service:    &stubOAuthService{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "缺少 redirect_uri",
			query:      "client_id=client_test",
			service:    &stubOAuthService{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "客户端未注册",
			query:      validQuery,
			service:    &stubOAuthService{validateErr: oauthdomain.ErrInvalidClient},
			wantStatus: http.StatusBadRequest,
			wantCode:   "unauthorized_client",
		},
		{
			name:       "回调地址不匹配",
			query:      validQuery,
			service:    &stubOAuthService{validateErr: oauthdomain.ErrInvalidRedirectURI},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec = do(t, newTestServer(t, tc.service), http.MethodGet, PathAuthorize+"?"+tc.query, "")

			if rec.Code != tc.wantStatus {
				t.Fatalf("状态码 = %d, 期望 %d", rec.Code, tc.wantStatus)
			}
			var ct = rec.Header().Get("Content-Type")
			if !strings.HasPrefix(ct, "text/html") {
				t.Errorf("Content-Type = %q, 期望 text/html", ct)
			}
			var body = rec.Body.String()
			if !strings.Contains(body, "<html") {
				t.Errorf("响应不是 HTML 页面: %q", body)
			}
			if !strings.Contains(body, tc.wantCode) {
				t.Errorf("页面未包含错误码 %q: %q", tc.wantCode, body)
			}
			// 页面不该出现 JSON 响应。
			if strings.Contains(body, `"error"`) {
				t.Errorf("页面混入了 JSON 内容: %q", body)
			}
		})
	}
}

// TestAuthorizeSuccessRendersLoginPage 验证参数合法时渲染登录页。
func TestAuthorizeSuccessRendersLoginPage(t *testing.T) {
	var rec = do(t, newTestServer(t, &stubOAuthService{}), http.MethodGet, PathAuthorize+"?"+validQuery, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	var body = rec.Body.String()
	if !strings.Contains(body, "<form") {
		t.Errorf("响应不是登录页: %q", body)
	}
	// 表单必须带回授权参数，登录成功后才能原样使用。
	for _, field := range []string{"client_id", "redirect_uri", "state", "code_challenge", "code_challenge_method"} {
		if !strings.Contains(body, `name="`+field+`"`) {
			t.Errorf("登录页缺少隐藏字段 %q", field)
		}
	}
}

// TestLoginErrorsRenderHTML 验证登录端点的错误用 HTML 输出。
func TestLoginErrorsRenderHTML(t *testing.T) {
	var form = url.Values{
		"client_id":             {"client_test"},
		"redirect_uri":          {"http://127.0.0.1/cb"},
		"code_challenge":        {"abc"},
		"code_challenge_method": {"S256"},
	}
	var service = &stubOAuthService{issueCodeErr: oauthdomain.ErrInvalidClient}
	var rec = do(t, newTestServer(t, service), http.MethodPost, PathLogin, form.Encode())

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, 期望 text/html", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("响应不是 HTML 页面: %q", rec.Body.String())
	}
}

// TestLoginSuccessRendersSuccessPage 验证登录成功后渲染授权成功页。
//
// 客户端回调地址用的是 workbuddy:// 自定义协议，浏览器直接跳转只会显示
// "无法访问此网站"，因此由成功页负责跳转，并留手动链接兜底。
func TestLoginSuccessRendersSuccessPage(t *testing.T) {
	var form = url.Values{
		"client_id":             {"client_test"},
		"redirect_uri":          {"workbuddy://workbuddy/mcp/custom-mcp%3Awb-connector/oauth/callback"},
		"state":                 {"xyz"},
		"code_challenge":        {"abc"},
		"code_challenge_method": {"S256"},
		"username":              {"demo"},
		"password":              {"demo123"},
	}
	var rec = do(t, newTestServer(t, &stubOAuthService{}), http.MethodPost, PathLogin, form.Encode())

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, 期望 text/html", rec.Header().Get("Content-Type"))
	}
	var body = rec.Body.String()
	if !strings.Contains(body, "授权成功") {
		t.Errorf("页面缺少成功提示: %q", body)
	}
	// 回调地址必须以 workbuddy:// 开头——html/template 对 href 有协议白名单，
	// 自定义协议若未被放行会被替换成 #ZgotmplZ。
	if !strings.Contains(body, `href="workbuddy://workbuddy/mcp/`) {
		t.Errorf("页面缺少可用的回调链接: %q", body)
	}
	if strings.Contains(body, "#ZgotmplZ") {
		t.Errorf("回调地址被 html/template 的协议白名单拦掉了: %q", body)
	}
	// 三层跳转兜底：meta refresh、脚本、手动链接。
	if !strings.Contains(body, `http-equiv="refresh"`) {
		t.Errorf("页面缺少 meta refresh 跳转: %q", body)
	}
	if !strings.Contains(body, "window.location.replace") {
		t.Errorf("页面缺少脚本跳转: %q", body)
	}
	// 授权码要带上，客户端靠它换令牌。
	if !strings.Contains(body, "code=code_test") {
		t.Errorf("回调地址缺少授权码: %q", body)
	}
	// state 必须原样带回，供客户端校验。
	if !strings.Contains(body, "state=xyz") {
		t.Errorf("回调地址缺少 state: %q", body)
	}
}

// TestLoginInvalidRedirectURIRendersHTML 验证回调地址无法解析时也返回 HTML。
func TestLoginInvalidRedirectURIRendersHTML(t *testing.T) {
	// 形如 "://bad" 的地址无法被 url.Parse 解析。
	var form = url.Values{
		"client_id":             {"client_test"},
		"redirect_uri":          {"://bad"},
		"code_challenge":        {"abc"},
		"code_challenge_method": {"S256"},
	}
	var rec = do(t, newTestServer(t, &stubOAuthService{}), http.MethodPost, PathLogin, form.Encode())

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码 = %d, 期望 400", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type = %q, 期望 text/html", rec.Header().Get("Content-Type"))
	}
}

// TestMachineEndpointsReturnJSON 验证机器对机器的端点保持 JSON 输出。
//
// 元数据、动态注册、令牌这三个端点由客户端调用，必须保持 JSON。
func TestMachineEndpointsReturnJSON(t *testing.T) {
	var server = newTestServer(t, &stubOAuthService{})

	t.Run("受保护资源元数据", func(t *testing.T) {
		var rec = do(t, server, http.MethodGet, PathProtectedResource, "")
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("Content-Type = %q, 期望 application/json", rec.Header().Get("Content-Type"))
		}
		var body = map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("响应不是合法 JSON: %v", err)
		}
	})

	t.Run("授权服务器元数据", func(t *testing.T) {
		var rec = do(t, server, http.MethodGet, PathAuthorizationServer, "")
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("Content-Type = %q, 期望 application/json", rec.Header().Get("Content-Type"))
		}
	})

	t.Run("动态客户端注册", func(t *testing.T) {
		var req = httptest.NewRequest(http.MethodPost, PathRegister,
			strings.NewReader(`{"client_name":"t","redirect_uris":["http://127.0.0.1/cb"]}`))
		req.Header.Set("Content-Type", "application/json")
		var rec = httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("状态码 = %d, 期望 201", rec.Code)
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("Content-Type = %q, 期望 application/json", rec.Header().Get("Content-Type"))
		}
		var body = map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("响应不是合法 JSON: %v", err)
		}
	})

	t.Run("令牌端点错误", func(t *testing.T) {
		var rec = do(t, server, http.MethodPost, PathToken, "grant_type=bogus")
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("Content-Type = %q, 期望 application/json", rec.Header().Get("Content-Type"))
		}
	})
}

// TestOAuthErrorCode 验证领域错误到 OAuth 错误码的映射。
func TestOAuthErrorCode(t *testing.T) {
	if got := oauthErrorCode(oauthdomain.ErrInvalidClient); got != "unauthorized_client" {
		t.Errorf("ErrInvalidClient → %q, 期望 unauthorized_client", got)
	}
	if got := oauthErrorCode(oauthdomain.ErrInvalidRedirectURI); got != "invalid_request" {
		t.Errorf("ErrInvalidRedirectURI → %q, 期望 invalid_request", got)
	}
	if got := oauthErrorCode(errors.New("其他错误")); got != "invalid_request" {
		t.Errorf("未知错误 → %q, 期望 invalid_request", got)
	}
}

// TestTokenErrorCode 验证令牌端点的错误码符合 RFC 6749 第 5.2 节。
func TestTokenErrorCode(t *testing.T) {
	var cases = []struct {
		err  error
		want string
	}{
		{oauthdomain.ErrInvalidClient, "invalid_client"},
		{oauthdomain.ErrUnsupportedGrant, "unsupported_grant_type"},
		{oauthdomain.ErrInvalidAuthCode, "invalid_grant"},
		{oauthdomain.ErrInvalidCodeVerifier, "invalid_grant"},
		{oauthdomain.ErrInvalidToken, "invalid_grant"},
		{errors.New("其他错误"), "invalid_request"},
	}
	for _, tc := range cases {
		if got := tokenErrorCode(tc.err); got != tc.want {
			t.Errorf("tokenErrorCode(%v) = %q, 期望 %q", tc.err, got, tc.want)
		}
	}
}

// TestTokenErrorCodeInResponse 验证令牌端点按规范返回对应错误码。
func TestTokenErrorCodeInResponse(t *testing.T) {
	var cases = []struct {
		name string
		err  error
		want string
	}{
		{"客户端不匹配", oauthdomain.ErrInvalidClient, "invalid_client"},
		{"不支持的授权类型", oauthdomain.ErrUnsupportedGrant, "unsupported_grant_type"},
		{"授权码无效", oauthdomain.ErrInvalidAuthCode, "invalid_grant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var service = &stubOAuthService{issueTokenErr: tc.err}
			var rec = do(t, newTestServer(t, service), http.MethodPost, PathToken, "grant_type=authorization_code")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d, 期望 400", rec.Code)
			}
			var body = map[string]any{}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("响应不是合法 JSON: %v", err)
			}
			if body["error"] != tc.want {
				t.Errorf("error = %v, 期望 %q", body["error"], tc.want)
			}
		})
	}
}

// TestRevokeEndpoint 验证令牌撤销端点。
//
// 对应 RFC 7009：撤销成功返回 200，参数缺失返回 400。
func TestRevokeEndpoint(t *testing.T) {
	t.Run("撤销成功", func(t *testing.T) {
		var rec = do(t, newTestServer(t, &stubOAuthService{}), http.MethodPost, PathRevoke,
			"token=at_test&token_type_hint=access_token&client_id=client_test")
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d, 期望 200", rec.Code)
		}
	})

	t.Run("缺少 token", func(t *testing.T) {
		var rec = do(t, newTestServer(t, &stubOAuthService{}), http.MethodPost, PathRevoke, "client_id=client_test")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("状态码 = %d, 期望 400", rec.Code)
		}
		var body = map[string]any{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if body["error"] != "invalid_request" {
			t.Errorf("error = %v, 期望 invalid_request", body["error"])
		}
	})
}

// TestResourceIndicator 验证 RFC 8707 的 resource 参数校验。
//
// resource 缺省时不校验；给了就必须与本服务声明的受保护资源一致。
func TestResourceIndicator(t *testing.T) {
	var server = newTestServer(t, &stubOAuthService{})

	t.Run("resource 一致时放行", func(t *testing.T) {
		var rec = do(t, server, http.MethodPost, PathToken,
			"grant_type=authorization_code&resource=http%3A%2F%2F127.0.0.1%3A8080%2Fmcp")
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d, 期望 200", rec.Code)
		}
	})

	t.Run("resource 缺省时放行", func(t *testing.T) {
		var rec = do(t, server, http.MethodPost, PathToken, "grant_type=authorization_code")
		if rec.Code != http.StatusOK {
			t.Fatalf("状态码 = %d, 期望 200", rec.Code)
		}
	})

}
