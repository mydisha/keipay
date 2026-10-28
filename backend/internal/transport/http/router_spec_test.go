package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/mydisha/keipay/backend/internal/domain"
	"github.com/mydisha/keipay/backend/internal/service"
)

type specOperation struct {
	Permission string                 `yaml:"x-keipay-permission"`
	Security   *[]map[string][]string `yaml:"security"`
}

type spec struct {
	Info struct {
		Description string `yaml:"description"`
	} `yaml:"info"`
	Paths      map[string]map[string]yaml.Node `yaml:"paths"`
	Components struct {
		Schemas struct {
			Permission struct {
				Enum []string `yaml:"enum"`
			} `yaml:"Permission"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func loadSpec(t *testing.T) spec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var s spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	return s
}

var httpMethods = map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}

// specAdminAccess maps "METHOD /admin/path/{id}" to its access level: the
// x-keipay-permission, AccessPublic when security has no adminSession, or
// AccessAuthenticated for the global session requirement.
func specAdminAccess(t *testing.T, s spec) map[string]string {
	t.Helper()
	out := map[string]string{}
	for path, item := range s.Paths {
		if !strings.HasPrefix(path, "/admin/") {
			continue
		}
		for method, node := range item {
			if !httpMethods[method] {
				continue
			}
			var op specOperation
			if err := node.Decode(&op); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			access := AccessAuthenticated
			if op.Security != nil {
				needsSession := false
				for _, req := range *op.Security {
					if _, ok := req["adminSession"]; ok {
						needsSession = true
					}
				}
				if !needsSession {
					access = AccessPublic
				}
			}
			if op.Permission != "" {
				if access == AccessPublic {
					t.Fatalf("%s %s: public operation with a permission", method, path)
				}
				access = op.Permission
			}
			out[strings.ToUpper(method)+" "+path] = access
		}
	}
	return out
}

var ginParam = regexp.MustCompile(`:([a-z_]+)`)

func routerAdminAccess() map[string]string {
	out := map[string]string{}
	for _, rt := range adminRoutes(Deps{}) {
		out[rt.method+" /admin"+ginParam.ReplaceAllString(rt.path, "{$1}")] = rt.access
	}
	return out
}

// T-16 / AC-15
func TestRouterPermissionsMatchSpec(t *testing.T) {
	want := specAdminAccess(t, loadSpec(t))
	got := routerAdminAccess()
	if len(want) == 0 {
		t.Fatal("no admin operations in the spec")
	}
	for k, access := range want {
		if got[k] != access {
			t.Errorf("%s: router access %q, spec %q", k, got[k], access)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("%s is routed but not in the spec", k)
		}
	}
	for _, public := range []string{"GET /admin/setup", "POST /admin/setup", "POST /admin/login", "POST /admin/logout"} {
		if got[public] != AccessPublic {
			t.Errorf("%s = %q, want public", public, got[public])
		}
	}
}

// Every /admin route gin serves comes from adminRoutes, so none can skip the
// access table.
func TestEveryRegisteredAdminRouteIsInTheTable(t *testing.T) {
	table := routerAdminAccess()
	r := newAPI(t, "").router
	n := 0
	for _, ri := range r.Routes() {
		if !strings.HasPrefix(ri.Path, "/admin") {
			continue
		}
		n++
		key := ri.Method + " " + ginParam.ReplaceAllString(ri.Path, "{$1}")
		if _, ok := table[key]; !ok {
			t.Errorf("%s registered outside the access table", key)
		}
	}
	if n != len(table) {
		t.Fatalf("gin has %d admin routes, table has %d", n, len(table))
	}
}

// T-2: permission constants and the role table equal the contract.
func TestPermissionsMatchSpec(t *testing.T) {
	s := loadSpec(t)
	want := slices.Clone(s.Components.Schemas.Permission.Enum)
	slices.Sort(want)
	var got []string
	for _, p := range domain.AllPermissions() {
		got = append(got, string(p))
	}
	if !slices.Equal(got, want) {
		t.Fatalf("domain %v\nspec   %v", got, want)
	}

	row := regexp.MustCompile("^\\| `([a-z:]+)` \\| (yes|no) \\| (yes|no) \\| (yes|no) \\|$")
	roles := []domain.Role{domain.RoleOwner, domain.RoleAdmin, domain.RoleViewer}
	rows := 0
	for _, line := range strings.Split(s.Info.Description, "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		rows++
		for i, role := range roles {
			if role.Can(domain.Permission(m[1])) != (m[i+2] == "yes") {
				t.Errorf("%s can %s = %v, spec says %s", role, m[1], role.Can(domain.Permission(m[1])), m[i+2])
			}
		}
	}
	if rows != len(want) {
		t.Fatalf("parsed %d table rows, want %d", rows, len(want))
	}
}

// T-17 / AC-15, AC-17: every admin operation for every kind of caller. The
// expected access comes from the spec, not from the router under test.
func TestRoleRouteMatrix(t *testing.T) {
	specAccess := specAdminAccess(t, loadSpec(t))
	f := newAPI(t, "")
	target := f.addUser(t, "target@example.com", domain.RoleViewer)
	pending := f.addUser(t, "pending@example.com", domain.RoleOwner)
	pending.MustChangePassword = true
	if err := f.users.Update(t.Context(), pending); err != nil {
		t.Fatal(err)
	}
	actors := map[string]*domain.User{
		"owner":  f.owner,
		"admin":  f.addUser(t, "admin@example.com", domain.RoleAdmin),
		"viewer": f.addUser(t, "viewer@example.com", domain.RoleViewer),
	}
	app, err := f.appSvc.Create(domain.WithActor(t.Context(), domain.SystemActor("test")),
		service.CreateAppInput{Name: "Matrix", Slug: "matrix", Environment: domain.EnvTest})
	if err != nil {
		t.Fatal(err)
	}
	paths := strings.NewReplacer("/users/:id", "/users/"+target.PublicID, "/apps/:id", "/apps/"+app.App.PublicID,
		":key_id", app.Key.PublicID, "/webhook-deliveries/:id", "/webhook-deliveries/3")

	for _, rt := range adminRoutes(Deps{}) {
		path := "/admin" + paths.Replace(rt.path)
		name := rt.method + " /admin" + rt.path
		access, ok := specAccess[rt.method+" /admin"+ginParam.ReplaceAllString(rt.path, "{$1}")]
		if !ok {
			t.Errorf("%s is not in the spec", name)
			continue
		}
		call := func(b *browser) (int, domain.ErrorCode) {
			body := ""
			if unsafeMethod(rt.method) {
				body = "{}"
			}
			w := f.send(b, rt.method, path, body)
			var env errorEnvelope
			_ = json.Unmarshal(w.Body.Bytes(), &env)
			return w.Code, env.Error.Code
		}

		for role, u := range actors {
			status, code := call(f.signIn(t, u))
			denied := access != AccessPublic && access != AccessAuthenticated && !u.Role.Can(domain.Permission(access))
			if denied && (status != http.StatusForbidden || code != domain.CodeForbidden) {
				t.Errorf("%s as %s: %d %s, want 403 forbidden", name, role, status, code)
			}
			if !denied && (code == domain.CodeForbidden || status == http.StatusUnauthorized && code != domain.CodeInvalidCredentials) {
				t.Errorf("%s as %s: %d %s, want allowed", name, role, status, code)
			}
		}

		status, code := call(newBrowser())
		if access == AccessPublic {
			if code == domain.CodeSessionRequired || code == domain.CodeForbidden {
				t.Errorf("%s anonymous: %d %s, want public", name, status, code)
			}
		} else if status != http.StatusUnauthorized || code != domain.CodeSessionRequired {
			t.Errorf("%s anonymous: %d %s, want 401 session_required", name, status, code)
		}

		status, code = call(f.signIn(t, pending))
		blocked := access != AccessPublic && !(rt.method == http.MethodGet && rt.path == "/me") &&
			!(rt.method == http.MethodPost && rt.path == "/me/password")
		if blocked != (code == domain.CodePasswordChangeRequired) {
			t.Errorf("%s with a pending password change: %d %s (blocked=%v)", name, status, code, blocked)
		}
	}

	allowed := []string{}
	for _, rt := range adminRoutes(Deps{}) {
		if rt.access != AccessPublic && rt.duringPasswordChange {
			allowed = append(allowed, rt.method+" "+rt.path)
		}
	}
	if !slices.Equal(allowed, []string{"GET /me", "POST /me/password"}) {
		t.Fatalf("routes open during a password change: %v (plus the public logout)", allowed)
	}

	forbidden := f.logs.FilterField(stringField("event", "admin.forbidden")).All()
	if len(forbidden) == 0 {
		t.Fatal("no admin.forbidden security log")
	}
	fields := forbidden[0].ContextMap()
	if !strings.HasPrefix(fields["user_id"].(string), "usr_") || fields["route"] == "" || fields["permission"] == "" {
		t.Fatalf("forbidden log fields = %v", fields)
	}
}
