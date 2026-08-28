package main

import (
	"fmt"
	"log"
	"net/http"
)

func performLogin(auth *Auth) {
	authURL, err := auth.PerformAuth()
	if err != nil {
		log.Fatalf("authentication failed: %v", err)
	}

	if authURL == "" {
		return
	}

	codeChan := make(chan string, 1)

	server := &http.Server{
		Addr: "127.0.0.1:8732",
	}

	http.HandleFunc("/oauth-callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")

		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}

		fmt.Fprintln(w, "Authorization successful. You may close this window.")

		codeChan <- code
	})

	go func() {
		if err := server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			log.Fatalf("callback server failed: %v", err)
		}
	}()

	fmt.Println("Open this URL in your browser:")
	fmt.Println("  " + authURL + "\n")

	code := <-codeChan

	if err := server.Close(); err != nil {
		log.Printf("close callback server: %v", err)
	}

	if err := auth.Login(code); err != nil {
		log.Fatalf("login failed: %v", err)
	}

	if err := auth.SaveLoginState(); err != nil {
		log.Fatalf("save login state: %v", err)
	}

	fmt.Println("login successful.")
}
