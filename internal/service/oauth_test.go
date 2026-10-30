package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"

	"wb-connector/internal/domain/oauthdomain"
)

// testChallenge 由给定的 verifier 计算出 PKCE 挑战值。
func testChallenge(verifier string) string {
	var digest = sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func TestOAuthService_Authenticate(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()

	tests := []struct {
		name    string
		req     oauthdomain.AuthenticateRequest
		want    string
		wantErr bool
	}{
		{name: "正确账号密码", req: oauthdomain.AuthenticateRequest{Username: "demo", Password: "demo123"}, want: "demo"},
		{name: "密码错误", req: oauthdomain.AuthenticateRequest{Username: "demo", Password: "wrong"}, wantErr: true},
		{name: "账号不存在", req: oauthdomain.AuthenticateRequest{Username: "nobody", Password: "demo123"}, wantErr: true},
		{name: "账号为空", req: oauthdomain.AuthenticateRequest{Username: "", Password: "demo123"}, wantErr: true},
		{name: "密码为空", req: oauthdomain.AuthenticateRequest{Username: "demo", Password: ""}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := service.Authenticate(ctx, tt.req)
			if tt.wantErr {
				if !errors.Is(err, oauthdomain.ErrInvalidAccount) {
					t.Fatalf("错误 = %v, 期望 %v", err, oauthdomain.ErrInvalidAccount)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应返回错误: %v", err)
			}
			if got != tt.want {
				t.Errorf("用户标识 = %q, 期望 %q", got, tt.want)
			}
		})
	}
}

func TestOAuthService_注册与授权码(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var redirectURI = "workbuddy://workbuddy/mcp/connector%3Atest/oauth/callback"
	var verifier = "test-verifier-value"
	var challenge = testChallenge(verifier)

	// 注册客户端
	client, err := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
		RedirectURIs: []string{redirectURI},
		ClientName:   "test",
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if client.ClientID == "" {
		t.Fatal("client_id 不应为空")
	}
	if len(client.RedirectURIs) != 1 || client.RedirectURIs[0] != redirectURI {
		t.Errorf("回调地址列表 = %v, 期望 [%v]", client.RedirectURIs, redirectURI)
	}

	// 签发授权码
	code, err := service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         redirectURI,
		UserID:              "demo",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("签发授权码失败: %v", err)
	}
	if code.Code == "" {
		t.Fatal("授权码不应为空")
	}
	if code.UserID != "demo" {
		t.Errorf("用户标识 = %q, 期望 demo", code.UserID)
	}
}

func TestOAuthService_授权参数校验(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var redirectURI = "workbuddy://callback"

	var client, _ = service.RegisterClient(ctx, oauthdomain.RegisterRequest{RedirectURIs: []string{redirectURI}})

	tests := []struct {
		name    string
		req     oauthdomain.AuthorizeRequest
		wantErr error
	}{
		{
			name: "客户端未注册",
			req: oauthdomain.AuthorizeRequest{
				ClientID: "client_unknown", RedirectURI: redirectURI,
				CodeChallenge: "abc", CodeChallengeMethod: "S256",
			},
			wantErr: oauthdomain.ErrInvalidClient,
		},
		{
			name: "回调地址不匹配",
			req: oauthdomain.AuthorizeRequest{
				ClientID: client.ClientID, RedirectURI: "https://evil.example.com",
				CodeChallenge: "abc", CodeChallengeMethod: "S256",
			},
			wantErr: oauthdomain.ErrInvalidRedirectURI,
		},
		{
			name: "PKCE 算法不是 S256",
			req: oauthdomain.AuthorizeRequest{
				ClientID: client.ClientID, RedirectURI: redirectURI,
				CodeChallenge: "abc", CodeChallengeMethod: "plain",
			},
			wantErr: oauthdomain.ErrUnsupportedChallengeMethod,
		},
		{
			name: "缺少挑战值",
			req: oauthdomain.AuthorizeRequest{
				ClientID: client.ClientID, RedirectURI: redirectURI,
				CodeChallengeMethod: "S256",
			},
			wantErr: oauthdomain.ErrUnsupportedChallengeMethod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.ValidateAuthorizeRequest(ctx, tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("错误 = %v, 期望 %v", err, tt.wantErr)
			}
		})
	}
}

func TestOAuthService_换取令牌与PKCE校验(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var redirectURI = "workbuddy://callback"
	var verifier = "correct-verifier"
	var challenge = testChallenge(verifier)

	var client, _ = service.RegisterClient(ctx, oauthdomain.RegisterRequest{RedirectURIs: []string{redirectURI}})
	var code, _ = service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID: client.ClientID, RedirectURI: redirectURI, UserID: "demo",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})

	// PKCE 校验不通过
	_, err := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code.Code,
		RedirectURI: redirectURI, ClientID: client.ClientID, CodeVerifier: "wrong",
	})
	if !errors.Is(err, oauthdomain.ErrInvalidCodeVerifier) {
		t.Fatalf("错误 = %v, 期望 %v", err, oauthdomain.ErrInvalidCodeVerifier)
	}

	// 授权码已被消耗，需要重新签发
	var code2, _ = service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID: client.ClientID, RedirectURI: redirectURI, UserID: "demo",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})
	var token *oauthdomain.Token
	token, err = service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code2.Code,
		RedirectURI: redirectURI, ClientID: client.ClientID, CodeVerifier: verifier,
	})
	if err != nil {
		t.Fatalf("换取令牌失败: %v", err)
	}
	if token.AccessToken == "" || token.RefreshToken == "" {
		t.Fatal("令牌不应为空")
	}
	if token.UserID != "demo" {
		t.Errorf("用户标识 = %q, 期望 demo", token.UserID)
	}
	if token.TokenType != "Bearer" {
		t.Errorf("令牌类型 = %q, 期望 Bearer", token.TokenType)
	}
	// 令牌必须记录归属客户端，刷新时据此校验。
	if token.ClientID != client.ClientID {
		t.Errorf("令牌归属客户端 = %q, 期望 %q", token.ClientID, client.ClientID)
	}
}

func TestOAuthService_授权码一次性(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var redirectURI = "workbuddy://callback"
	var verifier = "v"
	var challenge = testChallenge(verifier)

	var client, _ = service.RegisterClient(ctx, oauthdomain.RegisterRequest{RedirectURIs: []string{redirectURI}})
	var code, _ = service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID: client.ClientID, RedirectURI: redirectURI, UserID: "demo",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})

	// 第一次成功
	if _, err := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code.Code,
		RedirectURI: redirectURI, ClientID: client.ClientID, CodeVerifier: verifier,
	}); err != nil {
		t.Fatalf("首次换取令牌失败: %v", err)
	}
	// 第二次必须失败
	_, err := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code.Code,
		RedirectURI: redirectURI, ClientID: client.ClientID, CodeVerifier: verifier,
	})
	if !errors.Is(err, oauthdomain.ErrInvalidAuthCode) {
		t.Fatalf("授权码复用应失败, 实际错误 = %v", err)
	}
}

// TestOAuthService_换取令牌必须带clientID 验证换令牌时必须提供 client_id。
//
// 省略它等于放弃客户端身份校验——拿到授权码的一方就能直接换令牌。
func TestOAuthService_换取令牌必须带clientID(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var redirectURI = "workbuddy://callback"
	var verifier = "v"
	var challenge = testChallenge(verifier)

	var client, _ = service.RegisterClient(ctx, oauthdomain.RegisterRequest{RedirectURIs: []string{redirectURI}})
	var code, _ = service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID: client.ClientID, RedirectURI: redirectURI, UserID: "demo",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})

	// 不带 client_id
	_, err := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code.Code,
		RedirectURI: redirectURI, CodeVerifier: verifier,
	})
	if !errors.Is(err, oauthdomain.ErrInvalidClient) {
		t.Fatalf("不带 client_id 应报 %v, 实际 = %v", oauthdomain.ErrInvalidClient, err)
	}
}

// TestOAuthService_令牌校验与刷新 验证令牌校验与刷新流程。
func TestOAuthService_令牌校验与刷新(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var redirectURI = "workbuddy://callback"
	var verifier = "v"
	var challenge = testChallenge(verifier)

	var client, _ = service.RegisterClient(ctx, oauthdomain.RegisterRequest{RedirectURIs: []string{redirectURI}})
	var code, _ = service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID: client.ClientID, RedirectURI: redirectURI, UserID: "demo",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})
	var token, _ = service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code.Code,
		RedirectURI: redirectURI, ClientID: client.ClientID, CodeVerifier: verifier,
	})

	// 校验有效令牌
	var verified, err = service.VerifyToken(ctx, token.AccessToken)
	if err != nil {
		t.Fatalf("校验有效令牌失败: %v", err)
	}
	if verified.UserID != "demo" {
		t.Errorf("用户标识 = %q, 期望 demo", verified.UserID)
	}

	// 无效令牌
	if _, err = service.VerifyToken(ctx, "invalid"); !errors.Is(err, oauthdomain.ErrInvalidToken) {
		t.Fatalf("无效令牌应报错, 实际 = %v", err)
	}

	// 刷新令牌换取新令牌，且旧令牌立即失效
	var refreshed *oauthdomain.Token
	refreshed, err = service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "refresh_token", RefreshToken: token.RefreshToken,
		ClientID: client.ClientID,
	})
	if err != nil {
		t.Fatalf("刷新失败: %v", err)
	}
	if refreshed.AccessToken == token.AccessToken {
		t.Error("刷新后访问令牌应变化")
	}
	if _, err = service.VerifyToken(ctx, token.AccessToken); !errors.Is(err, oauthdomain.ErrInvalidToken) {
		t.Errorf("刷新后旧访问令牌应失效, 实际 = %v", err)
	}
	if _, err = service.VerifyToken(ctx, refreshed.AccessToken); err != nil {
		t.Errorf("刷新后新访问令牌应有效: %v", err)
	}
}

// TestOAuthService_刷新令牌绑定客户端 验证刷新令牌不能跨客户端使用。
func TestOAuthService_刷新令牌绑定客户端(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var verifier = "v"
	var challenge = testChallenge(verifier)

	clientA, _ := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
		RedirectURIs: []string{"workbuddy://callback/a"}})
	clientB, _ := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
		RedirectURIs: []string{"workbuddy://callback/b"}})

	var code, _ = service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID: clientA.ClientID, RedirectURI: "workbuddy://callback/a", UserID: "demo",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})
	token, _ := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code.Code,
		RedirectURI: "workbuddy://callback/a", ClientID: clientA.ClientID, CodeVerifier: verifier,
	})

	// 拿 A 的刷新令牌冒充 B
	_, err := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "refresh_token", RefreshToken: token.RefreshToken,
		ClientID: clientB.ClientID,
	})
	if !errors.Is(err, oauthdomain.ErrInvalidClient) {
		t.Fatalf("跨客户端刷新应报 %v, 实际 = %v", oauthdomain.ErrInvalidClient, err)
	}

	// 不带 client_id
	_, err = service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "refresh_token", RefreshToken: token.RefreshToken,
	})
	if !errors.Is(err, oauthdomain.ErrInvalidClient) {
		t.Fatalf("不带 client_id 刷新应报 %v, 实际 = %v", oauthdomain.ErrInvalidClient, err)
	}
}

func TestOAuthService_注册缺少回调地址(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	_, err := service.RegisterClient(context.Background(), oauthdomain.RegisterRequest{})
	if err == nil {
		t.Fatal("缺少 redirect_uris 应返回错误")
	}
}

// TestOAuthService_注册校验回调地址 验证动态注册时的回调地址校验。
//
// RFC 7591 第 2 节要求：必须是绝对 URI，且不能带 fragment。
func TestOAuthService_注册校验回调地址(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()

	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{name: "私有协议回调地址", uri: "workbuddy://workbuddy/mcp/connector%3Atest/oauth/callback"},
		{name: "回环回调地址", uri: "http://127.0.0.1:8080/oauth/callback"},
		{name: "https 回调地址", uri: "https://example.com/cb"},
		{name: "带 fragment", uri: "https://example.com/cb#frag", wantErr: true},
		{name: "相对路径", uri: "/callback", wantErr: true},
		{name: "javascript 协议", uri: "javascript:alert(1)", wantErr: true},
		{name: "data 协议", uri: "data:text/html,x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
				RedirectURIs: []string{tt.uri},
			})
			if tt.wantErr && err == nil {
				t.Errorf("地址 %q 被接受, 期望拒绝", tt.uri)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("地址 %q 被拒绝: %v", tt.uri, err)
			}
		})
	}
}

// TestOAuthService_回调地址匹配规则 验证回调地址的匹配规则。
//
// 精确匹配优先；回环地址只放宽端口（RFC 8252 第 7.3 节），
// 且必须与登记列表里的某个回环地址对应，不能凭空放行。
func TestOAuthService_回调地址匹配规则(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()

	// 客户端登记了私有协议回调地址与一个回环地址。
	var registered = "workbuddy://workbuddy/mcp/connector%3Atest/oauth/callback"
	var loopback = "http://127.0.0.1:8080/oauth/callback"
	client, err := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
		RedirectURIs: []string{registered, loopback},
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	tests := []struct {
		name        string
		redirectURI string
		wantErr     error
	}{
		{
			name:        "登记过的私有协议回调地址",
			redirectURI: registered,
		},
		{
			name:        "登记过的回环地址原样",
			redirectURI: loopback,
		},
		{
			name:        "回环地址端口不同",
			redirectURI: "http://127.0.0.1:57099/oauth/callback",
		},
		{
			name:        "未登记的外部地址",
			redirectURI: "https://evil.example.com/oauth/callback",
			wantErr:     oauthdomain.ErrInvalidRedirectURI,
		},
		{
			name:        "回环地址但路径不同",
			redirectURI: "http://127.0.0.1:57099/steal",
			wantErr:     oauthdomain.ErrInvalidRedirectURI,
		},
		{
			name:        "回环地址但查询串不同",
			redirectURI: "http://127.0.0.1:57099/oauth/callback?a=1",
			wantErr:     oauthdomain.ErrInvalidRedirectURI,
		},
		{
			name:        "回环地址但协议不是 http",
			redirectURI: "https://127.0.0.1:57099/oauth/callback",
			wantErr:     oauthdomain.ErrInvalidRedirectURI,
		},
		{
			name:        "主机名不是回环 IP 字面量",
			redirectURI: "http://localhost:57099/oauth/callback",
			wantErr:     oauthdomain.ErrInvalidRedirectURI,
		},
		{
			name:        "IPv4 与 IPv6 回环不是同一主机",
			redirectURI: "http://[::1]:57099/oauth/callback",
			wantErr:     oauthdomain.ErrInvalidRedirectURI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.ValidateAuthorizeRequest(ctx, oauthdomain.AuthorizeRequest{
				ClientID:            client.ClientID,
				RedirectURI:         tt.redirectURI,
				CodeChallenge:       "abc",
				CodeChallengeMethod: "S256",
			})
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("不应返回错误: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("错误 = %v, 期望 %v", err, tt.wantErr)
			}
		})
	}
}

// TestOAuthService_IPv6回环端口可不同 验证登记了 IPv6 回环地址时端口可变化。
func TestOAuthService_IPv6回环端口可不同(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()

	client, err := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
		RedirectURIs: []string{"http://[::1]:8080/oauth/callback"},
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	if err := service.ValidateAuthorizeRequest(ctx, oauthdomain.AuthorizeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "http://[::1]:57099/oauth/callback",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
	}); err != nil {
		t.Errorf("IPv6 回环地址端口不同时应放行: %v", err)
	}
}

// TestOAuthService_未登记的回环地址被拒绝 验证回环放宽不会放过未登记的地址。
//
// 若不比对登记列表，攻击者能构造一个从未登记的回环地址，
// 把用户授权后拿到的授权码引到自己的本地服务。
func TestOAuthService_未登记的回环地址被拒绝(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()

	// 登记列表里只有私有协议地址，没有任何回环地址。
	client, err := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
		RedirectURIs: []string{"workbuddy://workbuddy/mcp/connector%3Atest/oauth/callback"},
	})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	var err2 = service.ValidateAuthorizeRequest(ctx, oauthdomain.AuthorizeRequest{
		ClientID:            client.ClientID,
		RedirectURI:         "http://127.0.0.1:6666/oauth/callback",
		CodeChallenge:       "abc",
		CodeChallengeMethod: "S256",
	})
	if !errors.Is(err2, oauthdomain.ErrInvalidRedirectURI) {
		t.Fatalf("未登记的回环地址应被拒绝, 实际 = %v", err2)
	}
}

// TestOAuthService_撤销令牌 验证令牌撤销行为。
func TestOAuthService_撤销令牌(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var redirectURI = "workbuddy://callback"
	var verifier = "v"
	var challenge = testChallenge(verifier)

	client, _ := service.RegisterClient(ctx, oauthdomain.RegisterRequest{RedirectURIs: []string{redirectURI}})
	code, _ := service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID: client.ClientID, RedirectURI: redirectURI, UserID: "demo",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})
	token, _ := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code.Code,
		RedirectURI: redirectURI, ClientID: client.ClientID, CodeVerifier: verifier,
	})

	// 撤销访问令牌后，它与配套的刷新令牌都应失效。
	if err := service.RevokeToken(ctx, oauthdomain.RevokeRequest{
		Token: token.AccessToken, ClientID: client.ClientID,
	}); err != nil {
		t.Fatalf("撤销失败: %v", err)
	}
	if _, err := service.VerifyToken(ctx, token.AccessToken); !errors.Is(err, oauthdomain.ErrInvalidToken) {
		t.Errorf("撤销后访问令牌应失效, 实际 = %v", err)
	}
	if _, err := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "refresh_token", RefreshToken: token.RefreshToken, ClientID: client.ClientID,
	}); err == nil {
		t.Error("撤销访问令牌后配套的刷新令牌应失效")
	}

	// 撤销不存在的令牌也要成功，避免被用来探测令牌是否存在。
	if err := service.RevokeToken(ctx, oauthdomain.RevokeRequest{
		Token: "at_not_exist", ClientID: client.ClientID,
	}); err != nil {
		t.Errorf("撤销不存在的令牌应返回成功, 实际 = %v", err)
	}
}

// TestOAuthService_不能撤销他人令牌 验证撤销时的客户端归属校验。
func TestOAuthService_不能撤销他人令牌(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	var ctx = context.Background()
	var verifier = "v"
	var challenge = testChallenge(verifier)

	clientA, _ := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
		RedirectURIs: []string{"workbuddy://callback/a"}})
	clientB, _ := service.RegisterClient(ctx, oauthdomain.RegisterRequest{
		RedirectURIs: []string{"workbuddy://callback/b"}})

	code, _ := service.IssueAuthCode(ctx, oauthdomain.AuthorizeRequest{
		ClientID: clientA.ClientID, RedirectURI: "workbuddy://callback/a", UserID: "demo",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})
	token, _ := service.IssueToken(ctx, oauthdomain.TokenRequest{
		GrantType: "authorization_code", Code: code.Code,
		RedirectURI: "workbuddy://callback/a", ClientID: clientA.ClientID, CodeVerifier: verifier,
	})

	if err := service.RevokeToken(ctx, oauthdomain.RevokeRequest{
		Token: token.AccessToken, ClientID: clientB.ClientID,
	}); !errors.Is(err, oauthdomain.ErrInvalidClient) {
		t.Fatalf("撤销他人令牌应报 %v, 实际 = %v", oauthdomain.ErrInvalidClient, err)
	}
}

// TestOAuthService_不支持的授权类型 验证错误码符合 RFC 6749 第 5.2 节。
func TestOAuthService_不支持的授权类型(t *testing.T) {
	var service = NewOAuthService(oauthdomain.Config{Issuer: "http://127.0.0.1:8080", Resource: "http://127.0.0.1:8080/mcp"})
	_, err := service.IssueToken(context.Background(), oauthdomain.TokenRequest{
		GrantType: "client_credentials",
	})
	if !errors.Is(err, oauthdomain.ErrUnsupportedGrant) {
		t.Fatalf("错误 = %v, 期望 %v", err, oauthdomain.ErrUnsupportedGrant)
	}
}
