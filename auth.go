package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type AppRequest struct {
	ClientName   string `json:"client_name"`
	RedirectURIs string `json:"redirect_uris"`
	Scopes       string `json:"scopes"`
}

type AppResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
}

type TokenRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	GrantType    string `json:"grant_type"`
	Code         string `json:"code"`
	RedirectURI  string `json:"redirect_uri"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	Me           string `json:"me"`
	ExpiresIn    int64  `json:"expires_in"`
}

type Auth struct {
	Client *http.Client `json:"-"`

	Host      string `json:"host"`
	StatePath string `json:"-"`

	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func (a *Auth) Header(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
}

func (a *Auth) RedirectURI() string {
	return "http://127.0.0.1:8732/oauth-callback"
}

func (a *Auth) CreateApp() error {
	body := AppRequest{
		ClientName:   "reaperoma",
		RedirectURIs: a.RedirectURI(),
		Scopes:       "read write follow push admin",
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal app request: %w", err)
	}

	endpoint := strings.TrimRight(a.Host, "/") + "/api/v1/apps"

	req, err := http.NewRequest(
		http.MethodPost,
		endpoint,
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("create app request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("create app request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("create app returned HTTP %s", resp.Status)
	}

	var app AppResponse

	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return fmt.Errorf("decode app response: %w", err)
	}

	if app.ClientID == "" || app.ClientSecret == "" {
		return fmt.Errorf("app response missing client credentials")
	}

	a.ClientID = app.ClientID
	a.ClientSecret = app.ClientSecret

	return nil
}

func (a *Auth) AuthorizationURL() string {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", a.ClientID)
	query.Set("redirect_uri", a.RedirectURI())
	query.Set("scope", "read write follow push admin")

	return strings.TrimRight(a.Host, "/") +
		"/oauth/authorize?" +
		query.Encode()
}

func (a *Auth) Login(code string) error {
	body := TokenRequest{
		ClientID:     a.ClientID,
		ClientSecret: a.ClientSecret,
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  a.RedirectURI(),
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal token request: %w", err)
	}

	endpoint := strings.TrimRight(a.Host, "/") + "/oauth/token"

	req, err := http.NewRequest(
		http.MethodPost,
		endpoint,
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

		return fmt.Errorf(
			"token request returned HTTP %s: %s",
			resp.Status,
			strings.TrimSpace(string(responseBody)),
		)
	}

	var token TokenResponse

	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}

	if token.AccessToken == "" {
		return fmt.Errorf("token response did not contain an access token")
	}

	a.AccessToken = token.AccessToken
	a.RefreshToken = token.RefreshToken

	return nil
}

func (a *Auth) SaveClientState() error {
	data, err := json.MarshalIndent(a, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal client state: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(a.StatePath), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	if err := os.WriteFile(a.StatePath, data, 0o600); err != nil {
		return fmt.Errorf("write client state: %w", err)
	}

	return nil
}

func (a *Auth) SaveLoginState() error {
	data, err := json.MarshalIndent(a, "", "\t")
	if err != nil {
		return fmt.Errorf("marshal login state: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(a.StatePath), 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	if err := os.WriteFile(a.StatePath, data, 0o600); err != nil {
		return fmt.Errorf("write login state: %w", err)
	}

	return nil
}

func (a *Auth) LoadState() error {
	data, err := os.ReadFile(a.StatePath)
	if err != nil {
		return err
	}

	var state Auth

	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode auth state: %w", err)
	}

	a.Host = state.Host
	a.ClientID = state.ClientID
	a.ClientSecret = state.ClientSecret
	a.AccessToken = state.AccessToken
	a.RefreshToken = state.RefreshToken

	return nil
}

func (a *Auth) PerformAuth() (string, error) {
	if err := a.LoadState(); err == nil {
		if a.AccessToken != "" {
			return "", nil
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("load auth state: %w", err)
	}

	if a.ClientID == "" || a.ClientSecret == "" {
		if err := a.CreateApp(); err != nil {
			return "", fmt.Errorf("create app: %w", err)
		}

		if err := a.SaveClientState(); err != nil {
			return "", fmt.Errorf("save client state: %w", err)
		}
	}

	return a.AuthorizationURL(), nil
}
