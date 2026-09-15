package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// testErrorWriter mirrors the real error envelope closely enough to
// assert status and code without importing httpapi.
func testErrorWriter(w http.ResponseWriter, _ *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

// okHandler records whether the request got past the middleware.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthenticateAcceptsAValidToken(t *testing.T) {
	iss := testIssuer(t)
	userID := uuid.New()

	token, err := iss.IssueAccessToken(userID, uuid.New(), RoleUser)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	var reached bool
	var gotPrincipal Principal
	handler := Authenticate(iss, testErrorWriter)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFrom(r.Context())
			if !ok {
				t.Error("no Principal in the request context")
			}
			gotPrincipal = p
			reached = true
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !reached {
		t.Fatal("a valid token did not reach the handler")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotPrincipal.UserID != userID {
		t.Errorf("UserID = %s, want %s", gotPrincipal.UserID, userID)
	}
	if gotPrincipal.Role != RoleUser {
		t.Errorf("Role = %q, want %q", gotPrincipal.Role, RoleUser)
	}
	if gotPrincipal.TokenID == "" {
		t.Error("TokenID is empty; per-token revocation would be impossible")
	}
}

// Every way of presenting no credential, or a bad one, must be refused.
// A handler behind this middleware is entitled to assume it ran.
func TestAuthenticateRejectsEverythingElse(t *testing.T) {
	iss := testIssuer(t)

	valid, err := iss.IssueAccessToken(uuid.New(), uuid.New(), RoleUser)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	cases := map[string]string{
		"no header":       "",
		"empty bearer":    "Bearer ",
		"scheme only":     "Bearer",
		"wrong scheme":    "Basic " + valid,
		"no scheme":       valid,
		"garbage token":   "Bearer not-a-jwt",
		"truncated token": "Bearer " + valid[:len(valid)-8],
		"alg none":        "Bearer eyJhbGciOiJub25lIn0.eyJzdWIiOiJ4In0.",
		"only whitespace": "Bearer    ",
	}

	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			var reached bool
			handler := Authenticate(iss, testErrorWriter)(okHandler(&reached))

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if header != "" {
				// http.Header rejects embedded newlines, so set it raw.
				req.Header["Authorization"] = []string{header}
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if reached {
				t.Fatal("the request reached the handler")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

// Surrounding whitespace is trimmed rather than rejected.
//
// A trailing space or newline from a sloppy client does not change which
// token was presented — validation is by signature, not string equality
// — so refusing would break a caller for no security gain. An embedded
// CRLF cannot reach here in any case: Go's HTTP parser rejects it at the
// wire format.
func TestAuthenticateTrimsSurroundingWhitespace(t *testing.T) {
	iss := testIssuer(t)
	token, err := iss.IssueAccessToken(uuid.New(), uuid.New(), RoleUser)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	for name, header := range map[string]string{
		"trailing newline": "Bearer " + token + "\n",
		"trailing space":   "Bearer " + token + "  ",
		"extra leading":    "Bearer  " + token,
	} {
		t.Run(name, func(t *testing.T) {
			var reached bool
			handler := Authenticate(iss, testErrorWriter)(okHandler(&reached))

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header["Authorization"] = []string{header}
			handler.ServeHTTP(httptest.NewRecorder(), req)

			if !reached {
				t.Error("a valid token was rejected over surrounding whitespace")
			}
		})
	}
}

// The scheme is case-insensitive per RFC 7235, and real clients send
// "bearer".
func TestAuthenticateAcceptsAnyCaseScheme(t *testing.T) {
	iss := testIssuer(t)
	token, err := iss.IssueAccessToken(uuid.New(), uuid.New(), RoleUser)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		var reached bool
		handler := Authenticate(iss, testErrorWriter)(okHandler(&reached))

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", scheme+" "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !reached {
			t.Errorf("scheme %q was rejected", scheme)
		}
	}
}

// Expiry gets its own code because the client CAN act on it: refresh and
// retry, rather than log in again. The message stays generic.
func TestExpiredTokenReportsADistinctCode(t *testing.T) {
	iss := testIssuer(t)
	base := time.Now()
	iss.now = func() time.Time { return base }

	token, err := iss.IssueAccessToken(uuid.New(), uuid.New(), RoleUser)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	iss.now = func() time.Time { return base.Add(time.Hour) }

	var reached bool
	handler := Authenticate(iss, testErrorWriter)(okHandler(&reached))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if reached {
		t.Fatal("an expired token reached the handler")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "token_expired" {
		t.Errorf("code = %q, want token_expired", body["code"])
	}
	// The message must not explain which check failed.
	if strings.Contains(strings.ToLower(body["message"]), "expire") {
		t.Errorf("the client-facing message leaks the reason: %q", body["message"])
	}
}

// ─── RBAC ────────────────────────────────────────────────────────────

// The gate item: "RBAC enforced; a user cannot reach an admin route."
//
// A matrix rather than one example, because the failure that matters is
// a single role being wrongly admitted somewhere.
func TestRequireRoleMatrix(t *testing.T) {
	iss := testIssuer(t)

	routes := map[string][]string{
		"user-only":        {RoleUser},
		"astrologer-only":  {RoleAstrologer},
		"admin-only":       {RoleAdmin, RoleSuperAdmin},
		"super-admin-only": {RoleSuperAdmin},
		"any-staff":        {RoleAstrologer, RoleAdmin, RoleSuperAdmin},
	}
	allRoles := []string{RoleUser, RoleAstrologer, RoleAdmin, RoleSuperAdmin}

	for routeName, allowed := range routes {
		allowedSet := map[string]bool{}
		for _, r := range allowed {
			allowedSet[r] = true
		}

		for _, role := range allRoles {
			t.Run(routeName+"/"+role, func(t *testing.T) {
				token, err := iss.IssueAccessToken(uuid.New(), uuid.New(), role)
				if err != nil {
					t.Fatalf("IssueAccessToken: %v", err)
				}

				var reached bool
				handler := Authenticate(iss, testErrorWriter)(
					RequireRole(testErrorWriter, allowed...)(okHandler(&reached)))

				req := httptest.NewRequest(http.MethodGet, "/"+routeName, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				want := allowedSet[role]
				if reached != want {
					t.Fatalf("role %q on %q: reached=%v, want %v", role, routeName, reached, want)
				}
				if !want && rec.Code != http.StatusForbidden {
					t.Errorf("role %q on %q: status = %d, want 403", role, routeName, rec.Code)
				}
			})
		}
	}
}

// There is no role hierarchy. An implicit "admin includes user" ordering
// reads well and goes wrong the first time a role is added in the
// middle, so a route admitting both must say so.
func TestRolesHaveNoImplicitHierarchy(t *testing.T) {
	iss := testIssuer(t)

	token, err := iss.IssueAccessToken(uuid.New(), uuid.New(), RoleSuperAdmin)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	var reached bool
	handler := Authenticate(iss, testErrorWriter)(
		RequireRole(testErrorWriter, RoleUser)(okHandler(&reached)))

	req := httptest.NewRequest(http.MethodGet, "/user-only", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if reached {
		t.Error("super_admin reached a user-only route — an implicit hierarchy has crept in")
	}
}

// Without a Principal there is nobody to deny, so 401 rather than 403 —
// reporting 403 would imply an identity that does not exist.
func TestRequireRoleWithoutAuthenticateIs401(t *testing.T) {
	var reached bool
	handler := RequireRole(testErrorWriter, RoleAdmin)(okHandler(&reached))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))

	if reached {
		t.Fatal("an unauthenticated request reached an admin handler")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// An empty allow-list must admit nobody. Returning "allow all" on an
// empty list would turn a typo into an open door.
func TestRequireRoleWithNoRolesDeniesEveryone(t *testing.T) {
	iss := testIssuer(t)
	token, err := iss.IssueAccessToken(uuid.New(), uuid.New(), RoleSuperAdmin)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	var reached bool
	handler := Authenticate(iss, testErrorWriter)(
		RequireRole(testErrorWriter)(okHandler(&reached)))

	req := httptest.NewRequest(http.MethodGet, "/nobody", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if reached {
		t.Error("RequireRole() with no roles admitted a request")
	}
}

// ─── Client IP ───────────────────────────────────────────────────────

// The reason chi's middleware.RealIP is not used.
//
// It trusts X-Forwarded-For unconditionally. Phase 1 rate limits per IP,
// so a spoofable IP makes the limiter decorative: an attacker sends a
// different header on every request and never hits a limit, and a forged
// header belonging to someone else gets that person throttled.
func TestClientIPIgnoresForwardedHeadersByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.9:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")

	if got := ClientIP(req, false); got != "198.51.100.9" {
		t.Errorf("ClientIP = %q, want the socket address 198.51.100.9 — "+
			"a client-supplied header was trusted", got)
	}
}

func TestClientIPUsesForwardedHeaderWhenTrusted(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.5, 10.0.0.6")

	// Leftmost is the original client; the rest were added by proxies.
	if got := ClientIP(req, true); got != "203.0.113.7" {
		t.Errorf("ClientIP = %q, want 203.0.113.7", got)
	}
}

func TestClientIPFallsBackCleanly(t *testing.T) {
	// Trusted but no headers present.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.9:1234"
	if got := ClientIP(req, true); got != "198.51.100.9" {
		t.Errorf("ClientIP = %q, want 198.51.100.9", got)
	}

	// RemoteAddr without a port must not yield an empty subject, which
	// would put every caller in one rate-limit bucket.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "198.51.100.9"
	if got := ClientIP(req2, false); got == "" {
		t.Error("ClientIP returned empty; every caller would share one bucket")
	}
}
