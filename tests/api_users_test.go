package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

const baseURL = "http://127.0.0.1:8081"

func login(t *testing.T, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := http.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("login status %d: %s", resp.StatusCode, b)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	return out.AccessToken
}

func apiReq(t *testing.T, method, path, token string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, baseURL+path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	return resp
}

func TestAdminDeleteUser(t *testing.T) {
	adminToken := login(t, "Wirezat", "admin123")

	// Register a throwaway user via admin-enabled registration.
	const testUser = "test_delete_target"
	regBody, _ := json.Marshal(map[string]string{"username": testUser, "password": "testpass123"})
	regResp, err := http.Post(baseURL+"/api/auth/register", "application/json", bytes.NewReader(regBody))
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	defer regResp.Body.Close()
	if regResp.StatusCode != http.StatusCreated && regResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(regResp.Body)
		t.Fatalf("register status %d: %s", regResp.StatusCode, b)
	}

	// Find the new user's ID in the user list.
	listResp := apiReq(t, "GET", "/api/admin/users", adminToken, nil)
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list users status %d", listResp.StatusCode)
	}
	var users []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		IsAdmin  bool   `json:"is_admin"`
		IsOwner  bool   `json:"is_owner"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	var targetID string
	for _, u := range users {
		if u.Username == testUser {
			targetID = u.ID
		}
	}
	if targetID == "" {
		t.Fatalf("registered user %q not found in user list", testUser)
	}

	// Delete the user.
	delResp := apiReq(t, "DELETE", fmt.Sprintf("/api/admin/users/%s", targetID), adminToken, nil)
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete user status %d, want 204", delResp.StatusCode)
	}

	// Confirm user is gone from the list.
	listResp2 := apiReq(t, "GET", "/api/admin/users", adminToken, nil)
	defer listResp2.Body.Close()
	var users2 []struct {
		ID string `json:"id"`
	}
	json.NewDecoder(listResp2.Body).Decode(&users2)
	for _, u := range users2 {
		if u.ID == targetID {
			t.Fatalf("deleted user %s still appears in user list", targetID)
		}
	}
}

func TestAdminCannotDeleteOwner(t *testing.T) {
	adminToken := login(t, "Wirezat", "admin123")

	listResp := apiReq(t, "GET", "/api/admin/users", adminToken, nil)
	defer listResp.Body.Close()
	var users []struct {
		ID      string `json:"id"`
		IsOwner bool   `json:"is_owner"`
	}
	json.NewDecoder(listResp.Body).Decode(&users)
	var ownerID string
	for _, u := range users {
		if u.IsOwner {
			ownerID = u.ID
			break
		}
	}
	if ownerID == "" {
		t.Skip("no owner found, skipping")
	}

	resp := apiReq(t, "DELETE", fmt.Sprintf("/api/admin/users/%s", ownerID), adminToken, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("deleting owner: want 400, got %d", resp.StatusCode)
	}
}

func TestOwnerIsAdmin(t *testing.T) {
	adminToken := login(t, "Wirezat", "admin123")

	resp := apiReq(t, "GET", "/api/me", adminToken, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me status %d", resp.StatusCode)
	}
	var me struct {
		IsAdmin bool `json:"is_admin"`
		IsOwner bool `json:"is_owner"`
	}
	json.NewDecoder(resp.Body).Decode(&me)
	if !me.IsOwner {
		t.Skip("Wirezat is not the owner, skipping owner-is-admin check")
	}
	if !me.IsAdmin {
		t.Fatalf("owner is_admin = false, want true")
	}
}

func TestUnauthenticatedCannotListUsers(t *testing.T) {
	resp := apiReq(t, "GET", "/api/admin/users", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list users: want 401, got %d", resp.StatusCode)
	}
}
