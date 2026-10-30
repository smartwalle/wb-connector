// Package service 实现业务用例。
//
// 每个业务模块一个文件，实现领域包定义的 Service 契约。
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"wb-connector/internal/domain/oauthdomain"
)

// 认证相关的时长与规模上限。
const (
	// authCodeTTL 是授权码有效期。规范建议约 10 分钟。
	authCodeTTL = 10 * time.Minute

	// accessTokenTTL 是访问令牌有效期。
	//
	// 开发测试阶段取 5 分钟，便于观察令牌过期后的续期行为；
	// 正式部署应放宽到 1 小时左右，与 RFC 6749 第 4.2.2 节的建议一致。
	accessTokenTTL = time.Minute * 5

	// refreshTokenTTL 是刷新令牌有效期。
	//
	// 开发测试阶段取 10 分钟；正式部署应不少于 30 天
	// （RFC 6749 第 6 节要求刷新令牌不应轻易过期）。
	refreshTokenTTL = time.Minute * 10

	// maxClients 是允许登记的客户端数量上限，防止内存无限增长。
	maxClients = 1000

	// maxCodes 是同时有效的授权码数量上限，防止内存无限增长。
	maxCodes = 1000

	// maxTokens 是同时有效的访问令牌数量上限，防止内存无限增长。
	maxTokens = 5000

	// codeChallengeMethodS256 是唯一支持的 PKCE 挑战算法。
	codeChallengeMethodS256 = "S256"

	// loopbackCallbackScheme 是本机回环回调使用的协议。
	loopbackCallbackScheme = "http"
)

// dangerousRedirectSchemes 是禁止用于回调地址的协议。
//
// 这些协议能执行脚本或读取本地内容，把授权码送进去等于把凭证交出去。
var dangerousRedirectSchemes = map[string]bool{
	"javascript": true,
	"data":       true,
	"vbscript":   true,
	"file":       true,
}

// 固定的账号表。
//
// 这是示例实现，不接入数据库。生产环境应替换为真实用户体系，
// 并且不要把密码明文写在这里。
var accounts = map[string]string{
	"demo": "demo123",
}

// OAuthService 实现认证模块的业务用例。
//
// 授权码、客户端与令牌都保存在内存中，进程重启后失效。
type OAuthService struct {
	mu sync.RWMutex

	// clients 是已注册的客户端，键为 client_id。
	clients map[string]*oauthdomain.Client

	// codes 是已签发的授权码，键为授权码本身。
	codes map[string]*oauthdomain.AuthCode

	// tokens 是已签发的访问令牌，键为访问令牌本身。
	tokens map[string]*oauthdomain.Token

	// refreshTokens 是刷新令牌索引，键为刷新令牌本身，值为访问令牌。
	refreshTokens map[string]*oauthdomain.Token
}

// NewAuthService 创建 OAuthService。
func NewAuthService() *OAuthService {
	return &OAuthService{
		clients:       make(map[string]*oauthdomain.Client),
		codes:         make(map[string]*oauthdomain.AuthCode),
		tokens:        make(map[string]*oauthdomain.Token),
		refreshTokens: make(map[string]*oauthdomain.Token),
	}
}

// Authenticate 校验账号密码，成功返回用户标识。
//
// 账号不存在与密码错误返回同一个错误，避免暴露账号是否存在。
func (s *OAuthService) Authenticate(ctx context.Context, req oauthdomain.AuthenticateRequest) (string, error) {
	var username = strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		return "", oauthdomain.ErrInvalidAccount
	}

	expected, ok := accounts[username]
	if !ok {
		// 账号不存在时也走一次比较，避免通过响应耗时区分账号是否存在。
		_ = constantTimeEqual(req.Password, expected)
		return "", oauthdomain.ErrInvalidAccount
	}
	if !constantTimeEqual(req.Password, expected) {
		return "", oauthdomain.ErrInvalidAccount
	}
	return username, nil
}

// RegisterClient 动态注册客户端，返回新分配的 client_id。
func (s *OAuthService) RegisterClient(ctx context.Context, req oauthdomain.RegisterRequest) (*oauthdomain.Client, error) {
	if len(req.RedirectURIs) == 0 {
		return nil, fmt.Errorf("redirect_uris is required")
	}
	if err := validateRedirectURIs(req.RedirectURIs); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 超出上限时清理最早登记的客户端，避免内存无限增长。
	if len(s.clients) >= maxClients {
		s.evictOldestClient()
	}

	var client = &oauthdomain.Client{
		ClientID:     randomID("client"),
		RedirectURIs: req.RedirectURIs,
		ClientName:   req.ClientName,
		CreatedAt:    time.Now(),
	}
	s.clients[client.ClientID] = client
	return client, nil
}

// ValidateAuthorizeRequest 校验授权请求的参数是否合法。
func (s *OAuthService) ValidateAuthorizeRequest(ctx context.Context, req oauthdomain.AuthorizeRequest) error {
	// PKCE 必须使用 S256。
	if req.CodeChallengeMethod != codeChallengeMethodS256 {
		return oauthdomain.ErrUnsupportedChallengeMethod
	}
	if req.CodeChallenge == "" {
		return oauthdomain.ErrUnsupportedChallengeMethod
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// 客户端必须已注册。
	client, ok := s.clients[req.ClientID]
	if !ok {
		return oauthdomain.ErrInvalidClient
	}
	// 回调地址必须与登记列表中的某一项字符串精确匹配，
	// 本机回环回调地址除外。
	if !matchRedirectURI(client.RedirectURIs, req.RedirectURI) {
		return oauthdomain.ErrInvalidRedirectURI
	}
	return nil
}

// IssueAuthCode 为已登录的用户签发授权码。
func (s *OAuthService) IssueAuthCode(ctx context.Context, req oauthdomain.AuthorizeRequest) (*oauthdomain.AuthCode, error) {
	if err := s.ValidateAuthorizeRequest(ctx, req); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.codes) >= maxCodes {
		s.evictExpiredCodes()
	}

	var now = time.Now()
	var code = &oauthdomain.AuthCode{
		Code:                randomID("code"),
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		Scope:               req.Scope,
		UserID:              req.UserID,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		ExpiresAt:           now.Add(authCodeTTL),
	}
	s.codes[code.Code] = code
	return code, nil
}

// IssueToken 用授权码或刷新令牌换取访问令牌。
func (s *OAuthService) IssueToken(ctx context.Context, req oauthdomain.TokenRequest) (*oauthdomain.Token, error) {
	switch req.GrantType {
	case "authorization_code":
		return s.exchangeAuthCode(req)
	case "refresh_token":
		return s.refreshAccessToken(req)
	default:
		return nil, oauthdomain.ErrUnsupportedGrant
	}
}

// exchangeAuthCode 用授权码换取访问令牌。
//
// 授权码一次性使用：用过即从存储中移除，无论后续校验是否成功。
func (s *OAuthService) exchangeAuthCode(req oauthdomain.TokenRequest) (*oauthdomain.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var code, ok = s.codes[req.Code]
	if !ok {
		return nil, oauthdomain.ErrInvalidAuthCode
	}
	// 一次性使用：先删除再校验，避免同一个授权码被并发使用两次。
	delete(s.codes, req.Code)

	var now = time.Now()
	if code.IsExpired(now) {
		return nil, oauthdomain.ErrInvalidAuthCode
	}
	// client_id 必须提供且与授权时一致。
	// 省略它等于放弃客户端身份校验——拿到授权码的一方就能直接换令牌。
	if req.ClientID == "" || req.ClientID != code.ClientID {
		return nil, oauthdomain.ErrInvalidClient
	}
	// 回调地址必须与授权时一致。
	if req.RedirectURI != "" && req.RedirectURI != code.RedirectURI {
		return nil, oauthdomain.ErrInvalidRedirectURI
	}
	// PKCE 校验：用 code_verifier 的 SHA-256 摘要与挑战值比对。
	if !verifyPKCE(code.CodeChallenge, req.CodeVerifier) {
		return nil, oauthdomain.ErrInvalidCodeVerifier
	}

	return s.issueToken(code.ClientID, code.UserID, code.Scope), nil
}

// refreshAccessToken 用刷新令牌换取新的访问令牌。
func (s *OAuthService) refreshAccessToken(req oauthdomain.TokenRequest) (*oauthdomain.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var old, ok = s.refreshTokens[req.RefreshToken]
	if !ok {
		return nil, oauthdomain.ErrInvalidToken
	}
	// 刷新令牌绑定到签发它的客户端：否则拿到刷新令牌的一方，
	// 换个客户端身份就能无限续期。
	if req.ClientID == "" || req.ClientID != old.ClientID {
		return nil, oauthdomain.ErrInvalidClient
	}
	if time.Now().After(old.RefreshExpiresAt) {
		delete(s.refreshTokens, req.RefreshToken)
		delete(s.tokens, old.AccessToken)
		return nil, oauthdomain.ErrInvalidToken
	}

	// 旧的访问令牌立即失效，刷新令牌轮换。
	delete(s.tokens, old.AccessToken)
	delete(s.refreshTokens, req.RefreshToken)

	return s.issueToken(old.ClientID, old.UserID, old.Scope), nil
}

// issueToken 签发一组访问令牌与刷新令牌。
//
// 调用方必须已持有写锁。
func (s *OAuthService) issueToken(clientID string, userID string, scope string) *oauthdomain.Token {
	if len(s.tokens) >= maxTokens {
		s.evictExpiredTokens()
	}

	var now = time.Now()
	var token = &oauthdomain.Token{
		AccessToken:      randomID("at"),
		RefreshToken:     randomID("rt"),
		ClientID:         clientID,
		TokenType:        "Bearer",
		Scope:            scope,
		UserID:           userID,
		ExpiresAt:        now.Add(accessTokenTTL),
		RefreshExpiresAt: now.Add(refreshTokenTTL),
	}
	s.tokens[token.AccessToken] = token
	s.refreshTokens[token.RefreshToken] = token
	return token
}

// RevokeToken 撤销令牌。
//
// 令牌不存在时也返回成功——让调用方无法通过响应差异探测令牌是否存在。
// 撤销访问令牌时，其配套的刷新令牌一并失效。
func (s *OAuthService) RevokeToken(ctx context.Context, req oauthdomain.RevokeRequest) error {
	if req.Token == "" {
		return oauthdomain.ErrInvalidToken
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 先按刷新令牌找，再按访问令牌找；类型提示只影响查找顺序。
	if token, ok := s.refreshTokens[req.Token]; ok {
		if !s.canRevoke(req.ClientID, token) {
			return oauthdomain.ErrInvalidClient
		}
		delete(s.refreshTokens, req.Token)
		delete(s.tokens, token.AccessToken)
		return nil
	}
	if token, ok := s.tokens[req.Token]; ok {
		if !s.canRevoke(req.ClientID, token) {
			return oauthdomain.ErrInvalidClient
		}
		delete(s.tokens, req.Token)
		delete(s.refreshTokens, token.RefreshToken)
		return nil
	}
	// 令牌不存在：按 RFC 7009 仍返回成功。
	return nil
}

// canRevoke 判断请求方是否有权撤销该令牌。
//
// 提供了 client_id 时必须与令牌归属一致，防止别的客户端顺手撤掉别人的令牌。
func (s *OAuthService) canRevoke(clientID string, token *oauthdomain.Token) bool {
	if clientID == "" {
		return true
	}
	return clientID == token.ClientID
}

// VerifyToken 校验访问令牌，返回令牌携带的用户标识与权限范围。
func (s *OAuthService) VerifyToken(ctx context.Context, accessToken string) (*oauthdomain.Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var token, ok = s.tokens[accessToken]
	if !ok {
		return nil, oauthdomain.ErrInvalidToken
	}
	if token.IsExpired(time.Now()) {
		return nil, oauthdomain.ErrInvalidToken
	}
	return token, nil
}

// evictOldestClient 移除最早登记的客户端。
//
// 调用方必须已持有写锁。
func (s *OAuthService) evictOldestClient() {
	var oldest *oauthdomain.Client
	for _, client := range s.clients {
		if oldest == nil || client.CreatedAt.Before(oldest.CreatedAt) {
			oldest = client
		}
	}
	if oldest != nil {
		delete(s.clients, oldest.ClientID)
	}
}

// evictExpiredCodes 移除已过期的授权码。
//
// 调用方必须已持有写锁。
func (s *OAuthService) evictExpiredCodes() {
	var now = time.Now()
	for key, code := range s.codes {
		if code.IsExpired(now) {
			delete(s.codes, key)
		}
	}
}

// evictExpiredTokens 移除已过期的令牌。
//
// 调用方必须已持有写锁。
func (s *OAuthService) evictExpiredTokens() {
	var now = time.Now()
	for key, token := range s.tokens {
		if token.IsExpired(now) {
			delete(s.tokens, key)
			delete(s.refreshTokens, token.RefreshToken)
		}
	}
}

// verifyPKCE 校验 PKCE 的 code_verifier。
//
// 校验方式：base64url(SHA-256(code_verifier)) 与 code_challenge 精确比对。
func verifyPKCE(codeChallenge string, codeVerifier string) bool {
	if codeChallenge == "" || codeVerifier == "" {
		return false
	}
	var digest = sha256.Sum256([]byte(codeVerifier))
	var computed = base64.RawURLEncoding.EncodeToString(digest[:])
	return constantTimeEqual(computed, codeChallenge)
}

// randomID 生成随机标识。
//
// 前缀便于日志排查时区分标识类型。
func randomID(prefix string) string {
	var buf = make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// 随机数源不可用属于系统级故障，直接 panic 优于返回可预测的标识。
		panic(fmt.Sprintf("generate random id failed: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(buf)
}

// constantTimeEqual 常量时间比较两个字符串，避免通过耗时差异推断内容。
func constantTimeEqual(a string, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// containsString 判断列表中是否包含指定字符串。
func containsString(list []string, target string) bool {
	return slices.Contains(list, target)
}

// validateRedirectURIs 校验客户端登记的回调地址。
//
// RFC 7591 第 2 节要求：redirect_uris 必须是绝对 URI，且不能带 fragment。
// 这里另外拒绝 javascript: 这类可执行协议——把授权码送进可执行上下文，
// 等于把凭证交出去。
//
// 协议不做白名单限制：桌面客户端会用 workbuddy:// 之类的自定义协议，
// 把它们一并拒掉会让正常的连接器无法注册。
func validateRedirectURIs(uris []string) error {
	for _, item := range uris {
		var parsed, err = url.Parse(item)
		if err != nil {
			return fmt.Errorf("invalid redirect_uri %q: %w", item, err)
		}
		if !parsed.IsAbs() {
			return fmt.Errorf("redirect_uri %q must be an absolute URI", item)
		}
		if parsed.Fragment != "" || strings.Contains(item, "#") {
			return fmt.Errorf("redirect_uri %q must not include a fragment component", item)
		}
		if dangerousRedirectSchemes[strings.ToLower(parsed.Scheme)] {
			return fmt.Errorf("redirect_uri %q uses a disallowed scheme %q", item, parsed.Scheme)
		}
	}
	return nil
}

// matchRedirectURI 判断请求的回调地址是否被允许。
//
// 允许两种情形：
//  1. 与客户端登记的地址完全相同；
//  2. 本机回环上的 OAuth 回调地址，且与某个已登记的回环地址
//     相比只有端口不同。
//
// 第二种是 RFC 8252 第 7.3 节的要求：原生应用把回调放在回环地址上时，
// 端口由客户端在运行时分配，授权服务器必须接受任意端口，
// 否则客户端拿到的动态端口会让授权流程中断。
//
// 放宽范围严格限于端口——scheme、主机、路径、查询串都必须与登记值一致。
// 若不与登记列表比对，攻击者就能构造一个从未登记的回环地址，
// 把用户授权后拿到的授权码引到自己的本地服务。
func matchRedirectURI(registered []string, requested string) bool {
	if containsString(registered, requested) {
		return true
	}
	return matchLoopbackIgnoringPort(registered, requested)
}

// matchLoopbackIgnoringPort 判断请求地址是否是某个已登记回环地址的变体，
// 区别仅在端口。
func matchLoopbackIgnoringPort(registered []string, requested string) bool {
	var req = parseLoopbackURI(requested)
	if req == nil {
		return false
	}
	for _, item := range registered {
		var reg = parseLoopbackURI(item)
		if reg == nil {
			continue
		}
		if reg.scheme == req.scheme &&
			reg.host == req.host &&
			reg.path == req.path &&
			reg.query == req.query {
			return true
		}
	}
	return false
}

// loopbackURI 是回环回调地址的解析结果。
//
// 只保留匹配时需要比对的字段，端口刻意排除——它正是允许变化的部分。
type loopbackURI struct {
	// scheme 是协议，固定为 http。
	scheme string

	// host 是主机名，回环地址恒为 127.0.0.1 或 ::1。
	host string

	// path 是回调路径。
	path string

	// query 是查询串。
	query string
}

// parseLoopbackURI 解析回环回调地址。
//
// 地址不是本机回环上的 HTTP 地址时返回 nil。
func parseLoopbackURI(uri string) *loopbackURI {
	var parsed, err = url.Parse(uri)
	if err != nil {
		return nil
	}
	if parsed.Scheme != loopbackCallbackScheme {
		return nil
	}
	// 主机必须是回环 IP 字面量，与客户端判定回环的方式保持一致。
	var ip = net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil
	}
	return &loopbackURI{
		scheme: parsed.Scheme,
		host:   parsed.Hostname(),
		path:   parsed.Path,
		query:  parsed.RawQuery,
	}
}
