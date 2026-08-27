package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/11DingKing/cultivar-trial-governance/internal/apperror"
	"github.com/11DingKing/cultivar-trial-governance/internal/audit"
	"github.com/11DingKing/cultivar-trial-governance/internal/auth"
	"github.com/11DingKing/cultivar-trial-governance/internal/clock"
	"github.com/11DingKing/cultivar-trial-governance/internal/domain"
	"github.com/11DingKing/cultivar-trial-governance/internal/idgen"
	"github.com/11DingKing/cultivar-trial-governance/internal/service"
	"github.com/11DingKing/cultivar-trial-governance/internal/store"
)

type httpFixture struct {
	db      *store.DB
	handler http.Handler
	auth    auth.Service
	clock   *clock.Manual
	breeder domain.User
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	db, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "http.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	manual := clock.NewManual(now)
	ids := idgen.NewSequence(1)
	institution := domain.Institution{ID: "breeding", Name: "育种中心", Region: "north", Active: true, CreatedAt: now}
	hash, err := auth.HashPassword("http-password-2026")
	if err != nil {
		t.Fatal(err)
	}
	breeder := domain.User{
		ID: "breeder", InstitutionID: institution.ID, Email: "http@example.test", DisplayName: "接口育种员",
		Role: domain.RoleBreeder, Region: "north", Active: true, PasswordHash: hash, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.InsertInstitution(context.Background(), nil, institution); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertUser(context.Background(), nil, breeder); err != nil {
		t.Fatal(err)
	}
	authService := auth.Service{Store: db, Clock: manual, IDs: ids, SessionTTL: time.Hour}
	business := service.Service{Store: db, Clock: manual, IDs: ids, Audit: audit.Factory{IDs: ids}, WorkerMaxAttempts: 3}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := Server{Store: db, Auth: authService, Service: business, IDs: ids, Logger: logger}
	return &httpFixture{db: db, handler: server.Handler(), auth: authService, clock: manual, breeder: breeder}
}

func perform(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func loginHTTP(t *testing.T, fixture *httpFixture) string {
	t.Helper()
	recorder := perform(fixture.handler, http.MethodPost, "/v1/login", `{"email":"http@example.test","password":"http-password-2026"}`, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response auth.LoginResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Token == "" {
		t.Fatal("missing token")
	}
	return response.Token
}

func TestHealthAndReadinessExposeDependencyState(t *testing.T) {
	fixture := newHTTPFixture(t)
	health := perform(fixture.handler, http.MethodGet, "/healthz", "", "")
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"alive"`) {
		t.Fatalf("health status=%d body=%s", health.Code, health.Body.String())
	}
	ready := perform(fixture.handler, http.MethodGet, "/readyz", "", "")
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"database":"available"`) {
		t.Fatalf("ready status=%d body=%s", ready.Code, ready.Body.String())
	}
	if ready.Header().Get("X-Request-ID") == "" {
		t.Fatal("readiness lacks request ID")
	}
}

func TestLoginUsesUniformUnauthorizedContract(t *testing.T) {
	fixture := newHTTPFixture(t)
	wrong := perform(fixture.handler, http.MethodPost, "/v1/login", `{"email":"http@example.test","password":"wrong"}`, "")
	missing := perform(fixture.handler, http.MethodPost, "/v1/login", `{"email":"missing@example.test","password":"wrong"}`, "")
	if wrong.Code != http.StatusUnauthorized || missing.Code != http.StatusUnauthorized {
		t.Fatalf("wrong=%d missing=%d", wrong.Code, missing.Code)
	}
	var wrongBody, missingBody errorBody
	if err := json.Unmarshal(wrong.Body.Bytes(), &wrongBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(missing.Body.Bytes(), &missingBody); err != nil {
		t.Fatal(err)
	}
	if wrongBody.Error.Code != "unauthenticated" || missingBody.Error.Code != "unauthenticated" || wrongBody.Error.Message != missingBody.Error.Message {
		t.Fatalf("wrong=%+v missing=%+v", wrongBody, missingBody)
	}
}

func TestProtectedRouteRequiresBearerToken(t *testing.T) {
	fixture := newHTTPFixture(t)
	for _, header := range []string{"", "Basic abc", "Bearer", "Bearer invalid"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/applications", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		fixture.handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("header=%q status=%d body=%s", header, recorder.Code, recorder.Body.String())
		}
	}
}

func TestLogoutRevokesTokenAndIsIdempotentAtServiceBoundary(t *testing.T) {
	fixture := newHTTPFixture(t)
	token := loginHTTP(t, fixture)
	logout := perform(fixture.handler, http.MethodPost, "/v1/logout", "", token)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}
	protected := perform(fixture.handler, http.MethodGet, "/v1/applications", "", token)
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("revoked status=%d body=%s", protected.Code, protected.Body.String())
	}
	if err := fixture.auth.Logout(context.Background(), token); err != nil {
		t.Fatalf("service logout replay: %v", err)
	}
}

func TestSubmitApplicationHTTPContractAndReplay(t *testing.T) {
	fixture := newHTTPFixture(t)
	token := loginHTTP(t, fixture)
	body := `{"variety_code":"HTTP-1","variety_name":"接口品种","crop":"maize","generation":2,"traits_json":"{}","region":"north","policy_ref":"POLICY-HTTP","submission_note":"通过接口提交完整的区域试验申请说明"}`
	request := httptest.NewRequest(http.MethodPost, "/v1/applications", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", "http-idempotency-001")
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var first service.SubmitApplicationResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/v1/applications", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Idempotency-Key", "http-idempotency-001")
	replay := httptest.NewRecorder()
	fixture.handler.ServeHTTP(replay, request)
	if replay.Code != http.StatusCreated {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var second service.SubmitApplicationResult
	if err := json.Unmarshal(replay.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if first.Application.ID != second.Application.ID {
		t.Fatalf("first=%s second=%s", first.Application.ID, second.Application.ID)
	}
}

func TestStrictJSONRejectsUnknownAndMultipleValues(t *testing.T) {
	fixture := newHTTPFixture(t)
	unknown := perform(fixture.handler, http.MethodPost, "/v1/login", `{"email":"http@example.test","password":"http-password-2026","extra":true}`, "")
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	multiple := perform(fixture.handler, http.MethodPost, "/v1/login", `{"email":"http@example.test","password":"http-password-2026"} {}`, "")
	if multiple.Code != http.StatusBadRequest {
		t.Fatalf("multiple status=%d body=%s", multiple.Code, multiple.Body.String())
	}
}

func TestRequestBodyLimitRejectsOversizedPayload(t *testing.T) {
	fixture := newHTTPFixture(t)
	large := `{"email":"` + strings.Repeat("a", (1<<20)+1) + `","password":"password-2026"}`
	recorder := perform(fixture.handler, http.MethodPost, "/v1/login", large, "")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body prefix=%s", recorder.Code, recorder.Body.String()[:min(200, recorder.Body.Len())])
	}
}

func TestCallerRequestIDIsPreservedInError(t *testing.T) {
	fixture := newHTTPFixture(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/applications", nil)
	request.Header.Set("X-Request-ID", "caller-request-42")
	recorder := httptest.NewRecorder()
	fixture.handler.ServeHTTP(recorder, request)
	if recorder.Header().Get("X-Request-ID") != "caller-request-42" {
		t.Fatalf("header=%q", recorder.Header().Get("X-Request-ID"))
	}
	var body errorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.RequestID != "caller-request-42" {
		t.Fatalf("body=%+v", body)
	}
}

func TestStatusMappingKeepsStablePublicCategories(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{apperror.ErrValidation, 400, "validation_error"},
		{apperror.ErrUnauthenticated, 401, "unauthenticated"},
		{apperror.ErrForbidden, 403, "forbidden"},
		{apperror.ErrNotFound, 404, "not_found"},
		{apperror.ErrConflict, 409, "conflict"},
		{apperror.ErrStaleVersion, 409, "conflict"},
		{apperror.ErrCapacity, 409, "conflict"},
		{context.DeadlineExceeded, 504, "timeout"},
		{errors.New("database broken"), 500, "internal_error"},
	}
	for _, tc := range cases {
		if got := statusFor(tc.err); got != tc.status {
			t.Fatalf("%v status=%d want=%d", tc.err, got, tc.status)
		}
		if got := apperror.Code(tc.err); got != tc.code {
			t.Fatalf("%v code=%s want=%s", tc.err, got, tc.code)
		}
	}
}

func TestBearerTokenParsingIsStrictAndCaseInsensitive(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"Bearer token":       "token",
		"bearer token":       "token",
		"  BEARER   token  ": "token",
		"Basic token":        "",
		"Bearer":             "",
		"Bearer one two":     "",
		"":                   "",
	} {
		if got := bearerToken(input); got != want {
			t.Fatalf("%q = %q, want %q", input, got, want)
		}
	}
}

func TestDecodeRejectsMalformedJSONWithoutLeakingParserDetails(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"email":`))
	recorder := httptest.NewRecorder()
	var target map[string]any
	if decode(recorder, request, &target) {
		t.Fatal("malformed JSON decoded")
	}
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "unexpected EOF") {
		t.Fatalf("parser detail leaked: %s", recorder.Body.String())
	}
}
