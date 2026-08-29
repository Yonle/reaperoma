package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type User struct {
	CreatedAt          time.Time `json:"created_at"`
	Email              string    `json:"email"`
	Nickname           string    `json:"nickname"`
	IsConfirmed        bool      `json:"is_confirmed"`
	RegistrationReason string    `json:"registration_reason"`
}

type UsersResponse struct {
	Count    int    `json:"count"`
	PageSize int    `json:"page_size"`
	Users    []User `json:"users"`
}

type DeleteUsersRequest struct {
	Nicknames []string `json:"nicknames"`
}

type durationValue time.Duration

func (d *durationValue) String() string {
	if d == nil {
		return ""
	}

	return time.Duration(*d).String()
}

func (d *durationValue) Set(s string) error {
	if s == "" {
		return nil
	}

	if strings.HasSuffix(s, "d") {
		var days float64

		if _, err := fmt.Sscanf(strings.TrimSuffix(s, "d"), "%f", &days); err != nil {
			return fmt.Errorf("invalid day duration %q", s)
		}

		*d = durationValue(time.Duration(days * float64(24*time.Hour)))
		return nil
	}

	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}

	*d = durationValue(v)
	return nil
}

func deleteUsers(
	client *http.Client,
	auth *Auth,
	nicknames []string,
) error {
	body := DeleteUsersRequest{
		Nicknames: nicknames,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal delete request: %w", err)
	}

	endpoint := strings.TrimRight(auth.Host, "/") +
		"/api/v1/pleroma/admin/users"

	req, err := http.NewRequest(
		http.MethodDelete,
		endpoint,
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	auth.Header(req)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

		return fmt.Errorf(
			"delete returned HTTP %s: %s",
			resp.Status,
			strings.TrimSpace(string(responseBody)),
		)
	}

	return nil
}

func cleanUp(
	client *http.Client,
	auth *Auth,
	cleanAge durationValue,
) error {
	log.Println("--- reaperoma cleanup initiated ---")
	defer log.Println("--- reaperoma cleanup done ---")

	endpoint := strings.TrimRight(auth.Host, "/") +
		"/api/v1/pleroma/admin/users?page=1&filters=local,unconfirmed,need_approval"

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create GET request: %w", err)
	}

	auth.Header(req)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

		return fmt.Errorf(
			"GET returned HTTP %s: %s",
			resp.Status,
			strings.TrimSpace(string(responseBody)),
		)
	}

	var result UsersResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	now := time.Now()

	var toDelete []User

	for _, user := range result.Users {
		if user.IsConfirmed {
			continue
		}

		age := now.Sub(user.CreatedAt)

		if age <= time.Duration(cleanAge) {
			log.Printf(
				"----- ------- %s | email: %s | register_reason: %s | register_age: %s",
				user.Nickname,
				user.Email,
				user.RegistrationReason,
				age.Round(time.Second),
			)

			continue
		}

		toDelete = append(toDelete, user)
	}

	if len(toDelete) == 0 {
		return nil
	}

	nicknames := make([]string, 0, len(toDelete))

	for _, user := range toDelete {
		nicknames = append(nicknames, user.Nickname)
	}

	if err := deleteUsers(client, auth, nicknames); err != nil {
		return fmt.Errorf("delete stale users: %w", err)
	}

	for _, user := range toDelete {
		age := now.Sub(user.CreatedAt)

		log.Printf(
			"DELETE %s | email: %s | register_reason: %s | register_age: %s",
			user.Nickname,
			user.Email,
			user.RegistrationReason,
			age.Round(time.Second),
		)
	}

	return nil
}

func main() {
	var (
		host = flag.String(
			"host",
			"",
			"Pleroma instance base URL",
		)

		statePath = flag.String(
			"stateFile",
			"reaperoma-state.json",
			"Reaperoma's login state file",
		)

		login = flag.Bool(
			"login",
			false,
			"Authenticate with a Pleroma instance and save the login state",
		)

		cleanAge      durationValue
		checkInterval durationValue

		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	)

	flag.Var(
		&cleanAge,
		"cleanAge",
		"Delete unconfirmed local users older than this age (e.g. 1d, 12h, 30m)",
	)

	flag.Var(
		&checkInterval,
		"checkInterval",
		"How often to run cleanup (e.g. 1h, 30m, 1d)",
	)

	flag.Parse()

	auth := &Auth{
		Client:    client,
		StatePath: *statePath,
	}

	// Explicit login mode.
	if *login {
		if *host == "" {
			log.Fatal("-host is required when using -login")
		}

		auth.Host = strings.TrimRight(*host, "/")

		if _, err := url.ParseRequestURI(auth.Host); err != nil {
			log.Fatalf("invalid -host: %v", err)
		}

		performLogin(auth)
		return
	}

	// Normal cleanup mode.
	if err := auth.LoadState(); err != nil {
		if os.IsNotExist(err) {
			log.Fatal("not authenticated yet; run with -login -host <pleroma host>")
		}

		log.Fatalf("load auth state: %v", err)
	}

	if auth.Host == "" {
		log.Fatal("auth state does not contain a Pleroma host; run with -login -host <pleroma host>")
	}

	if auth.AccessToken == "" {
		log.Fatal("auth state does not contain an access token; run with -login -host <pleroma host>")
	}

	if cleanAge <= 0 {
		log.Fatal("-cleanAge must be greater than 0")
	}

	parsedHost, err := url.Parse(auth.Host)
	if err != nil {
		log.Fatalf("invalid stored host: %v", err)
	}

	if parsedHost.Scheme == "" || parsedHost.Host == "" {
		log.Fatal("invalid stored host")
	}

	if err := cleanUp(
		client,
		auth,
		cleanAge,
	); err != nil {
		log.Println("error during cleanup:", err)
	}

	if checkInterval > 0 {
		ticker := time.NewTicker(time.Duration(checkInterval))
		defer ticker.Stop()

		for range ticker.C {
			if err := cleanUp(
				client,
				auth,
				cleanAge,
			); err != nil {
				log.Println("error during cleanup:", err)
			}
		}
	}
}
