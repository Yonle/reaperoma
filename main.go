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

	// time.ParseDuration doesn't support "d", so add it ourselves.
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
	host string,
	adminToken string,
	nicknames []string,
) error {
	body := DeleteUsersRequest{
		Nicknames: nicknames,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal delete request: %w", err)
	}

	endpoint := strings.TrimRight(host, "/") +
		"/api/v1/pleroma/admin/users"

	req, err := http.NewRequest(
		http.MethodDelete,
		endpoint,
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	req.Header.Set("X-Admin-Token", adminToken)
	req.Header.Set("Content-Type", "application/json")

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

func main() {
	var (
		host = flag.String(
			"host",
			"",
			"Pleroma instance base URL, e.g. https://pleroma.web.com",
		)
		adminToken = flag.String(
			"admintoken",
			"",
			"Pleroma admin token",
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

	if *host == "" {
		fmt.Fprintln(os.Stderr, "error: -host is required")
		os.Exit(2)
	}

	if *adminToken == "" {
		fmt.Fprintln(os.Stderr, "error: -admintoken is required")
		os.Exit(2)
	}

	if cleanAge <= 0 {
		fmt.Fprintln(os.Stderr, "error: -cleanAge must be greater than 0")
		os.Exit(2)
	}

	parsedHost, err := url.Parse(*host)
	if err != nil {
		log.Fatalf("invalid -host: %v", err)
	}

	if parsedHost.Scheme == "" || parsedHost.Host == "" {
		log.Fatalf("invalid -host: expected something like https://example.com")
	}

	if err := cleanUp(
		client,
		*host,
		*adminToken,
		cleanAge,
	); err != nil {
		log.Println("error during cleanup: ", err)
	}

	if checkInterval > 0 {
		ticker := time.NewTicker(time.Duration(checkInterval))
		defer ticker.Stop()

		for range ticker.C {
			if err := cleanUp(
				client,
				*host,
				*adminToken,
				cleanAge,
			); err != nil {
				log.Println("error during cleanup: ", err)
			}
		}
	}
}

func cleanUp(
	client *http.Client,
	host string,
	adminToken string,
	cleanAge durationValue,
) error {

	log.Println("--- reaperoma cleanup initiated ---")
	defer log.Println("--- reaperoma cleanup done ---")

	endpoint := strings.TrimRight(host, "/") +
		"/api/v1/pleroma/admin/users?page=1&filters=local,unconfirmed"

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create GET request: %v", err)
	}

	req.Header.Set("X-Admin-Token", adminToken)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET request failed: %v", err)
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
		return fmt.Errorf("decode response: %v", err)
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

	if err := deleteUsers(client, host, adminToken, nicknames); err != nil {
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
