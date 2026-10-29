package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Run every credential-handling flow and check that no log line, audit
// snapshot or unrelated response carries a password, hash, session token or
// CSRF token.
func TestNoSecretsInLogsAuditOrBodies(t *testing.T) {
	f := newAPI(t, "")
	f.users.Remove(f.owner.PublicID)
	var (
		secrets   []string
		responses []*httptest.ResponseRecorder
	)
	track := func(b *browser) {
		for _, v := range b.cookies {
			secrets = append(secrets, v)
		}
	}
	send := func(b *browser, method, target, body string) *httptest.ResponseRecorder {
		w := f.send(b, method, target, body)
		responses = append(responses, w)
		track(b)
		return w
	}
	tempPassword := func(w *httptest.ResponseRecorder) string {
		var body struct {
			User              struct{ ID string } `json:"user"`
			TemporaryPassword string              `json:"temporary_password"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.TemporaryPassword == "" {
			t.Fatalf("no temporary password: %d %s", w.Code, w.Body.String())
		}
		secrets = append(secrets, body.TemporaryPassword)
		return body.TemporaryPassword
	}

	const ownerPass, newOwnerPass, viewerPass = "setup-secret-pass", "rotated-secret-pass", "viewer-own-secret"
	secrets = append(secrets, ownerPass, newOwnerPass, viewerPass, "wrong-secret-pass")

	owner := &browser{cookies: map[string]string{}, remote: "192.0.2.10:5000"}
	send(owner, http.MethodGet, "/admin/setup", "")
	send(owner, http.MethodPost, "/admin/setup", `{"email":"owner@example.com","name":"Owner","password":"`+ownerPass+`"}`)
	send(owner, http.MethodPost, "/admin/me/password", `{"current_password":"`+ownerPass+`","new_password":"`+newOwnerPass+`"}`)

	created := send(owner, http.MethodPost, "/admin/users", `{"email":"viewer@example.com","name":"Viewer","role":"viewer"}`)
	firstTemp := tempPassword(created)
	var createdBody struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &createdBody)
	reset := send(owner, http.MethodPost, "/admin/users/"+createdBody.User.ID+"/reset-password", "")
	secondTemp := tempPassword(reset)

	viewer := newBrowser()
	send(viewer, http.MethodPost, "/admin/login", `{"email":"viewer@example.com","password":"`+firstTemp+`"}`)
	send(viewer, http.MethodPost, "/admin/login", `{"email":"viewer@example.com","password":"wrong-secret-pass"}`)
	send(viewer, http.MethodPost, "/admin/login", `{"email":"viewer@example.com","password":"`+secondTemp+`"}`)
	send(viewer, http.MethodPost, "/admin/me/password", `{"current_password":"`+secondTemp+`","new_password":"`+viewerPass+`"}`)
	send(viewer, http.MethodPost, "/admin/logout", "")
	send(owner, http.MethodPost, "/admin/logout", "")

	// Every flow must really have run, or the checks below prove nothing. The
	// first temporary password was replaced by the reset, so it fails too.
	want := []string{"user.setup", "user.change_password", "user.create", "user.reset_password",
		"user.login_failed", "user.login_failed", "user.login", "user.change_password", "user.logout", "user.logout"}
	if got := f.audit.actions(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}

	for _, u := range f.users.All() {
		secrets = append(secrets, u.PasswordHash)
	}
	if len(secrets) < 12 {
		t.Fatalf("collected only %d secrets", len(secrets))
	}

	for _, e := range f.logs.All() {
		line := e.Message + fmt.Sprint(e.ContextMap())
		for _, s := range secrets {
			if s != "" && strings.Contains(line, s) {
				t.Fatalf("log line leaks a secret: %s", line)
			}
		}
	}
	if len(f.audit.logs) == 0 {
		t.Fatal("no audit entries")
	}
	for _, l := range f.audit.logs {
		raw := string(l.Before) + string(l.After)
		for _, s := range secrets {
			if s != "" && strings.Contains(raw, s) {
				t.Fatalf("%s snapshot leaks a secret: %s", l.Action, raw)
			}
		}
		if strings.Contains(raw, "$2a$") {
			t.Fatalf("%s snapshot holds a hash: %s", l.Action, raw)
		}
	}
	for _, w := range responses {
		body := w.Body.String()
		for _, s := range secrets {
			if s == "" || strings.Contains(body, `"temporary_password":"`+s+`"`) {
				continue
			}
			if strings.Contains(body, s) {
				t.Fatalf("response body leaks a secret: %s", body)
			}
		}
	}
}

// No response, audit snapshot or log line carries an endpoint secret or a
// custom header value.
func TestWebhookSecretsNeverLeak(t *testing.T) {
	f := newAPI(t, "")
	owner := f.signIn(t, f.owner)
	app := f.createApp(t, owner, `{"name":"Acme","slug":"acme","environment":"test"}`)
	created := f.createEndpoint(t, owner, app.id, `{"url":"https://hooks.example.com","events":["*"],
		"headers":[{"name":"Authorization","value":"Bearer header-secret-value"},{"name":"X-Short","value":"tiny-val"}]}`)
	path := "/admin/webhook-endpoints/" + created["endpoint"].(map[string]any)["id"].(string)
	rotated := decode(t, f.send(owner, http.MethodPost, path+"/rotate-secret", ""), http.StatusOK)
	decode(t, f.send(owner, http.MethodPatch, path, `{"headers":[{"name":"X-New","value":"another-header-secret"}],"description":"d"}`), 200)
	decode(t, f.send(owner, http.MethodPost, path+"/test", `{"event_type":"payment.paid"}`), 202)
	secrets := []string{created["secret"].(string), rotated["secret"].(string), "header-secret-value", "tiny-val", "another-header-secret"}
	for _, e := range f.hooks.AllEndpoints() {
		secrets = append(secrets, e.SecretEnc)
	}

	var bodies []string
	for _, p := range []string{path, "/admin/apps/" + app.id + "/webhook-endpoints", "/admin/audit-logs", "/admin/webhook-deliveries"} {
		bodies = append(bodies, decodeRawBody(t, f.send(owner, http.MethodGet, p, "")))
	}
	for _, l := range f.audit.logs {
		bodies = append(bodies, string(l.Before)+string(l.After))
	}
	for _, e := range f.logs.All() {
		raw, _ := json.Marshal(e.ContextMap())
		bodies = append(bodies, e.Message+string(raw))
	}
	for _, body := range bodies {
		for _, s := range secrets {
			if strings.Contains(body, s) {
				t.Fatalf("leak of %q in %s", s[:6], body)
			}
		}
	}
}
