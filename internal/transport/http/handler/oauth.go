// Package handler 是业务接口的 HTTP 适配层。
//
// 这里只做协议适配：绑定请求、调用用例、输出结果。
// 业务错误到状态码的映射由 jsoncodec 统一完成。
package handler

import (
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"wb-connector/internal/domain/oauthdomain"

	"github.com/smartwalle/rx"
	"github.com/smartwalle/rx/router"
)

// OAuth 的端点路径与标准路径保持一致。
const (
	// PathRegister 是动态客户端注册端点。
	PathRegister = "/oauth/register"

	// PathAuthorize 是授权端点。
	PathAuthorize = "/oauth/authorize"

	// PathLogin 是登录表单提交端点。
	PathLogin = "/oauth/login"

	// PathToken 是令牌端点。
	PathToken = "/oauth/token"

	// PathRevoke 是令牌撤销端点。
	PathRevoke = "/oauth/revoke"
)

// OAuth 协议相关的取值。
const (
	// grantTypeAuthorizationCode 是授权码流程的授权类型。
	grantTypeAuthorizationCode = "authorization_code"

	// grantTypeRefreshToken 是刷新流程的授权类型。
	grantTypeRefreshToken = "refresh_token"

	// codeChallengeMethodS256 是唯一支持的 PKCE 挑战算法。
	codeChallengeMethodS256 = "S256"
)

// OAuthHandler 提供 MCP 授权服务器的相关端点。
//
// service 是领域包定义的 oauthdomain.Service 契约，具体实现由应用组装层注入。
type OAuthHandler struct {
	service oauthdomain.Service
}

// NewOAuthHandler 创建 OAuthHandler。
func NewOAuthHandler(service oauthdomain.Service) *OAuthHandler {
	return &OAuthHandler{service: service}
}

// RegisterRoutes 声明本 OAuthHandler 负责的路由。
func (h *OAuthHandler) RegisterRoutes(r router.Router) {
	r.POST(PathRegister, h.Register)
	r.GET(PathAuthorize, h.Authorize)
	r.POST(PathLogin, h.Login)
	r.POST(PathToken, h.Token)
	r.POST(PathRevoke, h.Revoke)
}

// Register 处理动态客户端注册请求。
//
// 响应必须回显 redirect_uris，否则客户端后续的授权请求无法通过校验。
func (h *OAuthHandler) Register(c *rx.Context) {
	var req = oauthdomain.RegisterRequest{}
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		h.writeJSON(c, http.StatusBadRequest, map[string]any{
			"error":             "invalid_client_metadata",
			"error_description": "request body must be JSON",
		})
		return
	}

	client, err := h.service.RegisterClient(c.Context(), req)
	if err != nil {
		h.writeJSON(c, http.StatusBadRequest, map[string]any{
			"error":             "invalid_client_metadata",
			"error_description": err.Error(),
		})
		return
	}

	// 回显登记内容，并分配 client_id。
	h.writeJSON(c, http.StatusCreated, map[string]any{
		"client_id":                  client.ClientID,
		"redirect_uris":              client.RedirectURIs,
		"client_name":                client.ClientName,
		"grant_types":                []string{grantTypeAuthorizationCode, grantTypeRefreshToken},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"client_id_issued_at":        client.CreatedAt.Unix(),
	})
}

// Authorize 处理授权请求。
//
// 未登录时返回登录页；登录成功后签发授权码并跳回客户端回调地址。
func (h *OAuthHandler) Authorize(c *rx.Context) {
	var query = c.Request.URL.Query()
	var clientID = query.Get("client_id")
	var redirectURI = query.Get("redirect_uri")
	var state = query.Get("state")
	var scope = query.Get("scope")
	var codeChallenge = query.Get("code_challenge")
	var codeChallengeMethod = query.Get("code_challenge_method")

	// 回调地址缺失时无法继续，直接报错。
	if clientID == "" || redirectURI == "" {
		h.renderErrorPage(c, http.StatusBadRequest, "invalid_request",
			"请求缺少 client_id 或 redirect_uri 参数。")
		return
	}

	// 先校验授权参数合法，再渲染登录页，避免用非法回调地址把用户骗走。
	if err := h.service.ValidateAuthorizeRequest(c.Context(), oauthdomain.AuthorizeRequest{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
	}); err != nil {
		h.renderErrorPage(c, http.StatusBadRequest, oauthErrorCode(err), err.Error())
		return
	}

	h.renderLoginPage(c, clientID, redirectURI, state, scope, codeChallenge, codeChallengeMethod, "")
}

// Login 处理登录表单提交。
//
// 校验账号密码，签发授权码后跳回客户端回调地址。
func (h *OAuthHandler) Login(c *rx.Context) {
	if err := c.Request.ParseForm(); err != nil {
		h.renderErrorPage(c, http.StatusBadRequest, "invalid_request", "无法解析提交的表单。")
		return
	}

	var clientID = c.Request.Form.Get("client_id")
	var redirectURI = c.Request.Form.Get("redirect_uri")
	var state = c.Request.Form.Get("state")
	var scope = c.Request.Form.Get("scope")
	var codeChallenge = c.Request.Form.Get("code_challenge")
	var codeChallengeMethod = c.Request.Form.Get("code_challenge_method")
	var username = c.Request.Form.Get("username")
	var password = c.Request.Form.Get("password")

	userID, err := h.service.Authenticate(c.Context(), oauthdomain.AuthenticateRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		// 登录失败时重新渲染登录页并提示，不跳转。
		h.renderLoginPage(c, clientID, redirectURI, state, scope, codeChallenge, codeChallengeMethod, "账号或密码不正确")
		return
	}

	code, err := h.service.IssueAuthCode(c.Context(), oauthdomain.AuthorizeRequest{
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		UserID:              userID,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
	})
	if err != nil {
		h.renderErrorPage(c, http.StatusBadRequest, oauthErrorCode(err), err.Error())
		return
	}

	// 拼装回调地址，携带授权码与原始 state。
	var target, parseErr = url.Parse(redirectURI)
	if parseErr != nil {
		h.renderErrorPage(c, http.StatusBadRequest, "invalid_request", "回调地址无法解析。")
		return
	}
	var q = target.Query()
	q.Set("code", code.Code)
	if state != "" {
		q.Set("state", state)
	}
	target.RawQuery = q.Encode()

	// 客户端的回调地址通常是 workbuddy:// 这类自定义协议，
	// 浏览器直接跳过去只会显示"无法访问此网站"。
	// 改为渲染成功页，由页面负责跳转。
	h.renderSuccessPage(c, target.String())
}

// Token 处理令牌请求。
//
// 支持两种授权类型：授权码换令牌、刷新令牌换新令牌。
func (h *OAuthHandler) Token(c *rx.Context) {
	var req = oauthdomain.TokenRequest{}

	// 令牌端点按规范用表单编码提交。
	if err := c.Request.ParseForm(); err != nil {
		h.writeJSON(c, http.StatusBadRequest, map[string]any{
			"error":             "invalid_request",
			"error_description": "invalid form body",
		})
		return
	}
	req.GrantType = c.Request.Form.Get("grant_type")
	req.Code = c.Request.Form.Get("code")
	req.RedirectURI = c.Request.Form.Get("redirect_uri")
	req.ClientID = c.Request.Form.Get("client_id")
	req.CodeVerifier = c.Request.Form.Get("code_verifier")
	req.RefreshToken = c.Request.Form.Get("refresh_token")

	token, err := h.service.IssueToken(c.Context(), req)
	if err != nil {
		h.writeJSON(c, http.StatusBadRequest, map[string]any{
			"error":             tokenErrorCode(err),
			"error_description": err.Error(),
		})
		return
	}

	h.writeJSON(c, http.StatusOK, map[string]any{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"token_type":    token.TokenType,
		"expires_in":    int(time.Until(token.ExpiresAt).Seconds()),
		"scope":         token.Scope,
	})
}

// Revoke 处理令牌撤销请求。
//
// 对应 RFC 7009。按规范，无论令牌是否存在都返回 200，
// 避免调用方通过响应差异探测令牌是否存在。
func (h *OAuthHandler) Revoke(c *rx.Context) {
	if err := c.Request.ParseForm(); err != nil {
		h.writeJSON(c, http.StatusBadRequest, map[string]any{
			"error":             "invalid_request",
			"error_description": "invalid form body",
		})
		return
	}

	var req = oauthdomain.RevokeRequest{
		Token:         c.Request.Form.Get("token"),
		TokenTypeHint: c.Request.Form.Get("token_type_hint"),
		ClientID:      c.Request.Form.Get("client_id"),
	}
	if req.Token == "" {
		h.writeJSON(c, http.StatusBadRequest, map[string]any{
			"error":             "invalid_request",
			"error_description": "token is required",
		})
		return
	}

	if err := h.service.RevokeToken(c.Context(), req); err != nil {
		h.writeJSON(c, http.StatusBadRequest, map[string]any{
			"error":             tokenErrorCode(err),
			"error_description": err.Error(),
		})
		return
	}

	// RFC 7009：撤销成功返回 200 空体。
	h.writeJSON(c, http.StatusOK, map[string]any{})
}

// loginPage 是登录页模板。
var loginPage = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>登录 - {{.ServiceName}}</title>
<style>
  body { font-family: -apple-system, "PingFang SC", sans-serif; max-width: 360px;
         margin: 60px auto; padding: 0 20px; color: #1f2328; }
  h1 { font-size: 18px; font-weight: 500; margin-bottom: 4px; }
  p.sub { color: #656d76; font-size: 13px; margin-bottom: 24px; }
  label { display: block; font-size: 13px; margin-bottom: 6px; }
  input { width: 100%; box-sizing: border-box; padding: 8px 10px; font-size: 14px;
          border: 1px solid #d0d7de; border-radius: 6px; margin-bottom: 16px; }
  button { width: 100%; padding: 9px; font-size: 14px; color: #fff; background: #1f6feb;
           border: 0; border-radius: 6px; cursor: pointer; }
  .err { color: #cf222e; font-size: 13px; margin-bottom: 16px; }
</style>
</head>
<body>
  <h1>登录 {{.ServiceName}}</h1>
  <p class="sub">连接前需要登录你的账号，凭证不会离开本机。</p>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  <form method="post" action="{{.LoginPath}}">
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="state" value="{{.State}}">
    <input type="hidden" name="scope" value="{{.Scope}}">
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
    <label for="username">账号</label>
    <input id="username" name="username" autocomplete="username" autofocus required>
    <label for="password">密码</label>
    <input id="password" name="password" type="password" autocomplete="current-password" required>
    <button type="submit">登录</button>
  </form>
</body>
</html>`))

// loginPageData 是登录页模板的数据。
type loginPageData struct {
	// ServiceName 是服务名称，显示在页面上。
	ServiceName string

	// LoginPath 是表单提交地址。
	LoginPath string

	// ClientID 等是授权请求参数，登录成功后原样带回。
	ClientID            string
	RedirectURI         string
	State               string
	Scope               string
	CodeChallenge       string
	CodeChallengeMethod string

	// Error 是登录失败时的提示，为空则不显示。
	Error string
}

// errorPage 是错误页模板。
//
// 授权端点与登录端点是在浏览器里打开的，出错时也应该给出可读页面，
// 而不是把 JSON 直接甩到浏览器里——用户既看不懂，也无从下手。
var errorPage = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>无法继续 - {{.ServiceName}}</title>
<style>
  body { font-family: -apple-system, "PingFang SC", sans-serif; max-width: 360px;
         margin: 60px auto; padding: 0 20px; color: #1f2328; }
  h1 { font-size: 18px; font-weight: 500; margin-bottom: 4px; }
  p.sub { color: #656d76; font-size: 13px; margin-bottom: 16px; }
  .err { color: #cf222e; font-size: 13px; margin-bottom: 16px; }
  code { background: #f6f8fa; padding: 2px 6px; border-radius: 4px;
         font-size: 12px; color: #656d76; }
</style>
</head>
<body>
  <h1>{{.Title}}</h1>
  <p class="sub">{{.ServiceName}}</p>
  <div class="err">{{.Description}}</div>
  {{if .ErrorCode}}<p class="sub">错误码：<code>{{.ErrorCode}}</code></p>{{end}}
  <p class="sub">{{.Tip}}</p>
</body>
</html>`))

// errorPageData 是错误页模板的数据。
type errorPageData struct {
	// ServiceName 是服务名称，显示在页面上。
	ServiceName string

	// Title 是错误标题。
	Title string

	// Description 是面向用户的错误说明。
	Description string

	// ErrorCode 是 OAuth 错误码，便于排查问题。
	ErrorCode string

	// Tip 是给用户的下一步指引。
	Tip string
}

// successPage 是授权成功页模板。
//
// 客户端的回调地址通常用的是 workbuddy:// 之类的自定义协议，
// 浏览器直接 302 过去会显示"无法访问此网站"这类原生错误页。
// 中间插一页明确告知成功，再由页面跳转到回调地址。
var successPage = template.Must(template.New("success").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>授权成功 - {{.ServiceName}}</title>
{{if .RedirectURL}}<meta http-equiv="refresh" content="0;url={{.RedirectURL}}">{{end}}
<style>
  body { font-family: -apple-system, "PingFang SC", sans-serif; max-width: 360px;
         margin: 80px auto; padding: 0 20px; color: #1f2328; text-align: center; }
  .icon { width: 48px; height: 48px; margin: 0 auto 16px; border-radius: 50%;
          background: #dafbe1; color: #1a7f37; font-size: 26px; line-height: 48px; }
  h1 { font-size: 18px; font-weight: 500; margin-bottom: 8px; }
  p.sub { color: #656d76; font-size: 13px; margin-bottom: 20px; }
  a { color: #1f6feb; font-size: 13px; }
</style>
</head>
<body>
  <div class="icon">&#10003;</div>
  <h1>授权成功</h1>
  <p class="sub">正在返回客户端，请稍候…</p>
  {{if .RedirectURL}}<p class="sub"><a href="{{.RedirectURL}}">如果没有自动跳转，点击这里返回</a></p>{{end}}
  {{if .RedirectURL}}<script>window.location.replace({{.RedirectURL}});</script>{{end}}
</body>
</html>`))

// successPageData 是授权成功页的数据。
type successPageData struct {
	// ServiceName 是服务名称，显示在页面上。
	ServiceName string

	// RedirectURL 是客户端回调地址，登录成功后浏览器要导航到它。
	//
	// 用 template.URL 标记以绕开 html/template 对 href 的协议白名单——
	// 回调地址用的是 workbuddy:// 自定义协议，不在白名单内，
	// 未经标记会被替换成 #ZgotmplZ。
	RedirectURL template.URL
}

// renderSuccessPage 渲染授权成功页。
//
// 页面负责把浏览器导航到客户端回调地址：优先用 meta refresh，
// 再用脚本跳转，并留一个手动链接兜底。
func (h *OAuthHandler) renderSuccessPage(c *rx.Context, redirectURL string) {
	var data = successPageData{
		ServiceName: "连接器服务",
		RedirectURL: template.URL(redirectURL),
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Writer.WriteHeader(http.StatusOK)
	if err := successPage.Execute(c.Writer, data); err != nil {
		// 模板执行失败时页面已经发出，无法再改状态码，只记录日志。
		slog.ErrorContext(c.Context(), "渲染授权成功页失败", slog.Any("error", err))
	}
}

// renderLoginPage 渲染登录页。
func (h *OAuthHandler) renderLoginPage(c *rx.Context, clientID string, redirectURI string, state string,
	scope string, codeChallenge string, codeChallengeMethod string, errMsg string) {
	var data = loginPageData{
		ServiceName:         "连接器服务",
		LoginPath:           PathLogin,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		State:               state,
		Scope:               scope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		Error:               errMsg,
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Writer.WriteHeader(http.StatusOK)
	if err := loginPage.Execute(c.Writer, data); err != nil {
		// 模板执行失败时页面已经发出，无法再改状态码，只记录日志。
		slog.ErrorContext(c.Context(), "渲染登录页失败", slog.Any("error", err))
	}
}

// renderErrorPage 渲染错误页。
//
// 只用于浏览器直接打开的端点（授权端点、登录端点）。
// 出错时不能把用户重定向回客户端——此时客户端身份可能还没验证通过，
// 重定向会把用户送到未经验证的地址；只能在页面上说明原因。
func (h *OAuthHandler) renderErrorPage(c *rx.Context, status int, errorCode string, description string) {
	var data = errorPageData{
		ServiceName: "连接器服务",
		Title:       "授权请求无法继续",
		Description: description,
		ErrorCode:   errorCode,
		Tip:         "请返回客户端重新发起授权。若持续出现，请检查连接器配置后重试。",
	}

	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Writer.WriteHeader(status)
	if err := errorPage.Execute(c.Writer, data); err != nil {
		// 模板执行失败时页面已经发出，无法再改状态码，只记录日志。
		slog.ErrorContext(c.Context(), "渲染错误页失败", slog.Any("error", err))
	}
}

// oauthErrorCode 把领域错误映射成 OAuth 规范定义的错误码。
//
// 错误码会显示在错误页上，用规范里的取值便于与客户端日志对照排查。
func oauthErrorCode(err error) string {
	if errors.Is(err, oauthdomain.ErrInvalidClient) {
		return "unauthorized_client"
	}
	return "invalid_request"
}

// tokenErrorCode 把领域错误映射成令牌端点的错误码。
//
// RFC 6749 第 5.2 节定义的取值：invalid_request、invalid_client、
// invalid_grant、unauthorized_client、unsupported_grant_type、invalid_scope。
func tokenErrorCode(err error) string {
	switch {
	case errors.Is(err, oauthdomain.ErrInvalidClient):
		return "invalid_client"
	case errors.Is(err, oauthdomain.ErrUnsupportedGrant):
		return "unsupported_grant_type"
	case errors.Is(err, oauthdomain.ErrInvalidAuthCode),
		errors.Is(err, oauthdomain.ErrInvalidCodeVerifier),
		errors.Is(err, oauthdomain.ErrInvalidToken):
		return "invalid_grant"
	case errors.Is(err, oauthdomain.ErrInvalidRedirectURI):
		return "invalid_grant"
	default:
		return "invalid_request"
	}
}

// writeJSON 写出 JSON 响应。
//
// 只用于客户端与客户端之间的端点（元数据、注册、令牌）。
// 浏览器直接打开的端点用 renderErrorPage 输出 HTML。
func (h *OAuthHandler) writeJSON(c *rx.Context, status int, body any) {
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(status)
	if err := json.NewEncoder(c.Writer).Encode(body); err != nil {
		slog.ErrorContext(c.Context(), "写出 JSON 响应失败", slog.Any("error", err))
	}
}
