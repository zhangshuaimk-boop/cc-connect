package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type doctorInfoAgent struct {
	stubAgent
	bin   string
	label string
}

func (a *doctorInfoAgent) CLIBinaryName() string  { return a.bin }
func (a *doctorInfoAgent) CLIDisplayName() string { return a.label }

type doctorCheckerAgent struct {
	stubAgent
}

func (a *doctorCheckerAgent) DoctorChecks(context.Context) []DoctorCheckResult {
	return []DoctorCheckResult{{Name: "custom", Status: DoctorPass, Detail: "ok"}}
}

func makeExecutable(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return path
}

func TestDoctorHelpers_CLIAndFormatting(t *testing.T) {
	dir := t.TempDir()
	bin := makeExecutable(t, dir, "fake-agent", "#!/bin/sh\necho fake-agent 1.2.3\n")
	t.Setenv("PATH", dir)

	agent := &doctorInfoAgent{bin: "fake-agent", label: "FakeAgent"}
	gotBin, gotLabel := agentCLIInfo(agent)
	if gotBin != "fake-agent" || gotLabel != "FakeAgent" {
		t.Fatalf("agentCLIInfo() = %q/%q", gotBin, gotLabel)
	}

	binary := checkAgentBinary(context.Background(), agent)
	if len(binary) != 1 || binary[0].Status != DoctorPass || !strings.Contains(binary[0].Detail, "fake-agent") {
		t.Fatalf("checkAgentBinary() = %#v, bin=%s", binary, bin)
	}

	auth := checkCLIAuth(context.Background(), "fake-agent", []string{"--version"}, "FakeAgent")
	if len(auth) != 1 || auth[0].Status != DoctorPass || auth[0].Detail != "OK" {
		t.Fatalf("checkCLIAuth success = %#v", auth)
	}

	failBin := makeExecutable(t, dir, "fake-fail", "#!/bin/sh\necho auth failed\nexit 7\n")
	auth = checkCLIAuth(context.Background(), failBin, nil, "Failing")
	if len(auth) != 1 || auth[0].Status != DoctorWarn || !strings.Contains(auth[0].Detail, "auth failed") {
		t.Fatalf("checkCLIAuth failure = %#v", auth)
	}

	if DoctorPass.Icon() == "" || DoctorWarn.Icon() == "" || DoctorFail.Icon() == "" {
		t.Fatal("doctor status icons should not be empty")
	}
	formatted := FormatDoctorResults([]DoctorCheckResult{
		{Name: "Agent CLI (fake-agent)", Status: DoctorPass, Detail: "found", Latency: time.Millisecond},
		{Name: "Platforms", Status: DoctorWarn, Detail: "none"},
		{Name: "Config File", Status: DoctorFail, Detail: "missing"},
	}, NewI18n(LangEnglish))
	for _, want := range []string{"System Diagnostic Report", "found", "none", "missing"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted doctor results missing %q: %s", want, formatted)
		}
	}
}

func TestDoctorChecks_PlatformsDependenciesAndCustom(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"git", "sqlite3", "ffmpeg", "fake-agent"} {
		makeExecutable(t, dir, name, "#!/bin/sh\necho "+name+"\n")
	}
	t.Setenv("PATH", dir)

	deps := checkDependencies()
	if len(deps) != 3 {
		t.Fatalf("dependencies = %d, want 3", len(deps))
	}
	for _, dep := range deps {
		if dep.Status != DoctorPass {
			t.Fatalf("dependency %s status = %v, want pass", dep.Name, dep.Status)
		}
	}

	noPlatforms := checkPlatforms(nil)
	if len(noPlatforms) != 1 || noPlatforms[0].Status != DoctorWarn {
		t.Fatalf("checkPlatforms(nil) = %#v", noPlatforms)
	}
	platforms := checkPlatforms([]Platform{&stubPlatformEngine{n: "feishu"}})
	if len(platforms) != 1 || platforms[0].Status != DoctorPass || !strings.Contains(platforms[0].Name, "feishu") {
		t.Fatalf("checkPlatforms(feishu) = %#v", platforms)
	}

	results := RunDoctorChecks(context.Background(), &doctorCheckerAgent{}, []Platform{&stubPlatformEngine{n: "feishu"}})
	foundCustom := false
	for _, result := range results {
		if result.Name == "custom" {
			foundCustom = true
			break
		}
	}
	if !foundCustom {
		t.Fatalf("RunDoctorChecks() did not include custom checker result: %#v", results)
	}
}

func TestDirHistory_PersistenceAndHelpers(t *testing.T) {
	dir := t.TempDir()
	h := NewDirHistory(dir)
	h.SetMaxSize(2)
	h.Add("project", "/work/one")
	h.Add("project", "/work/two")
	h.Add("project", "/work/three")

	if got := h.List("project"); len(got) != 2 || got[0] != "/work/three" || got[1] != "/work/two" {
		t.Fatalf("history = %#v", got)
	}
	if !h.Contains("project", "/work/two") || h.Contains("project", "/work/one") {
		t.Fatalf("contains returned unexpected result for %#v", h.List("project"))
	}
	if got := h.Previous("project"); got != "/work/two" {
		t.Fatalf("Previous() = %q", got)
	}

	reloaded := NewDirHistory(dir)
	if got := reloaded.Get("project", 1); got != "/work/three" {
		t.Fatalf("reloaded Get(1) = %q", got)
	}

	h.SetMaxSize(0)
	h.Add("project", "/work/four")
	h.Add("project", "/work/five")
	if got := h.List("project"); len(got) != 1 || got[0] != "/work/five" {
		t.Fatalf("history after minimum max size = %#v", got)
	}
}

func TestUpdaterHTTPAndArchiveHelpers(t *testing.T) {
	releasesServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "cc-connect-updater" {
			t.Fatalf("user agent = %q", r.Header.Get("User-Agent"))
		}
		_ = json.NewEncoder(w).Encode([]ReleaseInfo{{TagName: "v9.9.9", Name: "latest"}})
	}))
	defer releasesServer.Close()

	releases, err := fetchReleasesFrom(releasesServer.URL)
	if err != nil {
		t.Fatalf("fetchReleasesFrom() error = %v", err)
	}
	if len(releases) != 1 || releases[0].TagName != "v9.9.9" {
		t.Fatalf("releases = %#v", releases)
	}

	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer statusServer.Close()
	if _, err := fetchReleasesFrom(statusServer.URL); err == nil || !strings.Contains(err.Error(), "418") {
		t.Fatalf("fetchReleasesFrom status error = %v", err)
	}

	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer downloadServer.Close()
	data, err := downloadFile(downloadServer.URL)
	if err != nil || string(data) != "payload" {
		t.Fatalf("downloadFile() = %q, %v", data, err)
	}

	tarData := buildTarGz(t, "nested/cc-connect", []byte("binary"))
	binary, err := extractBinaryFromTarGz(tarData)
	if err != nil || string(binary) != "binary" {
		t.Fatalf("extractBinaryFromTarGz() = %q, %v", binary, err)
	}
	if _, err := extractBinaryFromTarGz([]byte("bad gzip")); err == nil {
		t.Fatal("extractBinaryFromTarGz() should reject invalid gzip")
	}

	zipData := buildZip(t, "cc-connect.exe", []byte("zip-binary"))
	binary, err = extractBinaryFromZip(zipData)
	if err != nil || string(binary) != "zip-binary" {
		t.Fatalf("extractBinaryFromZip() = %q, %v", binary, err)
	}
	if _, err := extractBinaryFromZip(buildZip(t, "readme.txt", []byte("none"))); err == nil {
		t.Fatal("extractBinaryFromZip() should fail when archive has no binary")
	}
}

func buildTarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestSkillPresetsFetchAndCache(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(SkillPresetsResponse{
			Version: 1,
			Skills: []SkillPreset{{
				Name:        "calendar",
				DisplayName: "Calendar",
				Featured:    true,
			}},
		})
	}))
	defer server.Close()

	presets, err := fetchSkillPresetsFromURL(server.URL, time.Second)
	if err != nil {
		t.Fatalf("fetchSkillPresetsFromURL() error = %v", err)
	}
	if presets.Version != 1 || len(presets.Skills) != 1 || presets.Skills[0].Name != "calendar" {
		t.Fatalf("presets = %#v", presets)
	}

	cache := &skillPresetsCache{url: server.URL}
	first, err := cache.fetch()
	if err != nil {
		t.Fatalf("cache first fetch: %v", err)
	}
	second, err := cache.fetch()
	if err != nil {
		t.Fatalf("cache second fetch: %v", err)
	}
	if first != second || calls != 2 {
		t.Fatalf("cache did not return cached pointer: first=%p second=%p calls=%d", first, second, calls)
	}

	badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badServer.Close()
	if _, err := fetchSkillPresetsFromURL(badServer.URL, time.Second); err == nil {
		t.Fatal("fetchSkillPresetsFromURL() should fail on non-200")
	}
}

type coverageAgent struct {
	stubAgent
	model     string
	reasoning string
	mode      string
	workDir   string
	providers []ProviderConfig
	allowed   []string
}

func (a *coverageAgent) SetModel(model string) { a.model = model }
func (a *coverageAgent) GetModel() string      { return a.model }
func (a *coverageAgent) Name() string          { return "stub" }
func (a *coverageAgent) AvailableModels(context.Context) []ModelOption {
	return []ModelOption{{Name: "gpt-main", Alias: "main"}, {Name: "gpt-fast", Desc: "fast"}}
}
func (a *coverageAgent) SetReasoningEffort(effort string) { a.reasoning = effort }
func (a *coverageAgent) GetReasoningEffort() string       { return a.reasoning }
func (a *coverageAgent) AvailableReasoningEfforts() []string {
	return []string{"low", "medium", "high"}
}
func (a *coverageAgent) SetMode(mode string) { a.mode = mode }
func (a *coverageAgent) GetMode() string     { return a.mode }
func (a *coverageAgent) PermissionModes() []PermissionModeInfo {
	return []PermissionModeInfo{
		{Key: "ask", Name: "Ask", Desc: "ask first", NameZh: "Ask", DescZh: "ask first"},
		{Key: "auto", Name: "Auto", Desc: "auto approve", NameZh: "Auto", DescZh: "auto approve"},
	}
}
func (a *coverageAgent) SetProviders(providers []ProviderConfig) { a.providers = providers }
func (a *coverageAgent) SetActiveProvider(name string) bool {
	for _, provider := range a.providers {
		if provider.Name == name {
			return true
		}
	}
	return false
}
func (a *coverageAgent) GetActiveProvider() *ProviderConfig {
	if len(a.providers) == 0 {
		return nil
	}
	return &a.providers[0]
}
func (a *coverageAgent) ListProviders() []ProviderConfig { return a.providers }
func (a *coverageAgent) SetWorkDir(dir string)           { a.workDir = dir }
func (a *coverageAgent) GetWorkDir() string              { return a.workDir }
func (a *coverageAgent) AddAllowedTools(tools ...string) error {
	a.allowed = append(a.allowed, tools...)
	return nil
}
func (a *coverageAgent) GetAllowedTools() []string { return append([]string(nil), a.allowed...) }

type coverageContextSession struct {
	stubAgentSession
	model     string
	reasoning string
	workDir   string
	usage     *ContextUsage
}

func (s *coverageContextSession) GetModel() string           { return s.model }
func (s *coverageContextSession) GetReasoningEffort() string { return s.reasoning }
func (s *coverageContextSession) GetWorkDir() string         { return s.workDir }
func (s *coverageContextSession) GetContextUsage() *ContextUsage {
	return s.usage
}

type coverageSpeechToText struct {
	data   []byte
	format string
	lang   string
}

func (s *coverageSpeechToText) Transcribe(_ context.Context, audio []byte, format string, lang string) (string, error) {
	s.data = append([]byte(nil), audio...)
	s.format = format
	s.lang = lang
	return "transcribed text", nil
}

func TestEngineRenderCards_CoveragePaths(t *testing.T) {
	agent := &coverageAgent{
		model:     "gpt-main",
		reasoning: "medium",
		mode:      "ask",
		workDir:   t.TempDir(),
		providers: []ProviderConfig{{Name: "openai", BaseURL: "https://example.invalid", Model: "gpt-main"}},
	}
	e := NewEngine("project", agent, []Platform{&stubPlatformEngine{n: "feishu"}}, "", LangEnglish)
	e.AddCommand("deploy", "Deploy app", "run deploy", "", "", "config")

	skillDir := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(filepath.Join(skillDir, "audit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "audit", "SKILL.md"), []byte("---\nname: audit\ndescription: Audit code\n---\nRun audit.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.skills.SetDirs([]string{skillDir})

	cards := []*Card{
		e.renderLangCard(),
		e.renderModelCard(""),
		e.renderReasoningCard(),
		e.renderModeCard(),
		e.renderProviderCard(),
		e.renderCommandsCard(),
		e.renderConfigCard(),
		e.renderSkillsCard(),
		e.renderCurrentCard("feishu:chat:user"),
		e.renderHistoryCard("feishu:chat:user"),
		e.renderVersionCard(),
	}
	for i, card := range cards {
		if card == nil || card.Header == nil || strings.TrimSpace(card.RenderText()) == "" {
			t.Fatalf("card %d is empty: %#v", i, card)
		}
	}
}

func TestEngineRenderCards_UnsupportedAndHeartbeatPaths(t *testing.T) {
	e := NewEngine("project", &stubAgent{}, []Platform{&stubPlatformEngine{n: "plain"}}, "", LangEnglish)

	for name, card := range map[string]*Card{
		"model":     e.renderModelCard(""),
		"reasoning": e.renderReasoningCard(),
		"mode":      e.renderModeCard(),
		"provider":  e.renderProviderCard(),
		"commands":  e.renderCommandsCard(),
		"skills":    e.renderSkillsCard(),
		"heartbeat": e.renderHeartbeatCard(),
	} {
		if card == nil || !strings.Contains(strings.ToLower(card.RenderText()), strings.ToLower(card.Header.Title[:1])) {
			t.Fatalf("%s card did not render useful text: %#v", name, card)
		}
	}

	hs := NewHeartbeatScheduler(t.TempDir())
	hs.Register("project", HeartbeatConfig{
		Enabled:      true,
		IntervalMins: 5,
		OnlyWhenIdle: true,
		SessionKey:   "feishu:chat:user",
		Silent:       true,
	}, e, "")
	e.SetHeartbeatScheduler(hs)
	card := e.renderHeartbeatCard()
	text := card.RenderText()
	for _, want := range []string{"Heartbeat", "5", "yes"} {
		if !strings.Contains(text, want) {
			t.Fatalf("heartbeat card missing %q: %s", want, text)
		}
	}
}

func TestEngineCronAndTimerCommandAddPaths(t *testing.T) {
	p := &stubPlatformEngine{n: "plain"}
	e := NewEngine("project", &stubAgent{}, []Platform{p}, "", LangEnglish)

	cronStore, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.SetCronScheduler(NewCronScheduler(cronStore))
	msg := &Message{SessionKey: "feishu:chat:user", ReplyCtx: "ctx", UserID: "admin"}
	e.cmdCronAdd(p, msg, []string{"0", "9", "*", "*", "*", "daily", "summary"})
	if len(cronStore.ListByProject("project")) != 1 {
		t.Fatalf("cron jobs = %#v", cronStore.ListByProject("project"))
	}
	e.cmdCronDel(p, msg, []string{cronStore.ListByProject("project")[0].ID})
	if len(cronStore.ListByProject("project")) != 0 {
		t.Fatalf("cron job was not deleted")
	}

	timerStore, err := NewTimerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.SetTimerScheduler(NewTimerScheduler(timerStore))
	e.cmdTimerAdd(p, msg, []string{"10m", "check", "status"})
	if len(timerStore.ListPending()) != 1 {
		t.Fatalf("timer jobs = %#v", timerStore.ListPending())
	}
	id := timerStore.ListPending()[0].ID
	e.cmdTimerMute(p, msg, []string{id}, true)
	if job := timerStore.Get(id); job == nil || !job.Mute {
		t.Fatalf("timer mute failed: %#v", job)
	}
	e.cmdTimerDel(p, msg, []string{id})
	if len(timerStore.ListPending()) != 0 {
		t.Fatalf("timer job was not deleted")
	}
}

func TestAPIServerCronTimerAndRelayHandlers(t *testing.T) {
	engine := NewEngine("project", &stubAgent{}, []Platform{&stubPlatformEngine{n: "feishu"}}, "", LangEnglish)
	defer engine.cancel()
	engine.interactiveStates["feishu:chat:user"] = &interactiveState{
		platform: &stubPlatformEngine{n: "feishu"},
		replyCtx: "ctx",
	}

	cronStore, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	timerStore, err := NewTimerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	relay := NewRelayManager(t.TempDir())
	api := &APIServer{
		engines: map[string]*Engine{"project": engine},
		cron:    NewCronScheduler(cronStore),
		timer:   NewTimerScheduler(timerStore),
		relay:   relay,
	}
	api.RegisterEngine("project", engine)

	cronBody := `{"cron_expr":"*/5 * * * *","prompt":"summarize","description":"Summary","session_key":"feishu:chat:user"}`
	rec := httptest.NewRecorder()
	api.handleCronAdd(rec, httptest.NewRequest(http.MethodPost, "/cron/add", strings.NewReader(cronBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("cron add status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var cronJob CronJob
	if err := json.NewDecoder(rec.Body).Decode(&cronJob); err != nil {
		t.Fatalf("decode cron job: %v", err)
	}
	if cronJob.Project != "project" || cronJob.SessionKey != "feishu:chat:user" {
		t.Fatalf("cron job project/session = %q/%q", cronJob.Project, cronJob.SessionKey)
	}

	rec = httptest.NewRecorder()
	api.handleCronInfo(rec, httptest.NewRequest(http.MethodGet, "/cron/info?id="+cronJob.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("cron info status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.handleCronEdit(rec, httptest.NewRequest(http.MethodPost, "/cron/edit", strings.NewReader(`{"id":"`+cronJob.ID+`","field":"description","value":"Updated"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("cron edit status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := cronStore.Get(cronJob.ID).Description; got != "Updated" {
		t.Fatalf("cron description = %q", got)
	}

	rec = httptest.NewRecorder()
	api.handleCronList(rec, httptest.NewRequest(http.MethodGet, "/cron/list?project=project", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), cronJob.ID) {
		t.Fatalf("cron list status/body = %d/%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.handleCronDel(rec, httptest.NewRequest(http.MethodPost, "/cron/del", strings.NewReader(`{"id":"`+cronJob.ID+`"}`)))
	if rec.Code != http.StatusOK || cronStore.Get(cronJob.ID) != nil {
		t.Fatalf("cron del status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.handleTimerAdd(rec, httptest.NewRequest(http.MethodPost, "/timer/add", strings.NewReader(`{"delay":"5m","prompt":"check","mute":true,"session_key":"feishu:chat:user"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("timer add status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var timerJob TimerJob
	if err := json.NewDecoder(rec.Body).Decode(&timerJob); err != nil {
		t.Fatalf("decode timer job: %v", err)
	}
	if timerJob.Project != "project" || timerJob.SessionKey != "feishu:chat:user" || !timerJob.Mute {
		t.Fatalf("timer job = %#v", timerJob)
	}

	rec = httptest.NewRecorder()
	api.handleTimerInfo(rec, httptest.NewRequest(http.MethodGet, "/timer/info?id="+timerJob.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("timer info status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.handleTimerList(rec, httptest.NewRequest(http.MethodGet, "/timer/list?project=project", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), timerJob.ID) {
		t.Fatalf("timer list status/body = %d/%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.handleTimerDel(rec, httptest.NewRequest(http.MethodPost, "/timer/del", strings.NewReader(`{"id":"`+timerJob.ID+`"}`)))
	if rec.Code != http.StatusOK || timerStore.Get(timerJob.ID) != nil {
		t.Fatalf("timer del status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.handleRelayBind(rec, httptest.NewRequest(http.MethodPost, "/relay/bind", strings.NewReader(`{"platform":"feishu","chat_id":"chat","bots":{"project":"Project","other":"Other"}}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("relay bind status = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	api.handleRelayBinding(rec, httptest.NewRequest(http.MethodGet, "/relay/binding?chat_id=chat", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Project") {
		t.Fatalf("relay binding status/body = %d/%s", rec.Code, rec.Body.String())
	}
}

func TestAPIServerHandlerErrorBranches(t *testing.T) {
	api := &APIServer{}

	for name, tc := range map[string]struct {
		handler func(http.ResponseWriter, *http.Request)
		req     *http.Request
		want    int
	}{
		"cron add method":        {api.handleCronAdd, httptest.NewRequest(http.MethodGet, "/cron/add", nil), http.StatusMethodNotAllowed},
		"cron add no scheduler":  {api.handleCronAdd, httptest.NewRequest(http.MethodPost, "/cron/add", strings.NewReader(`{}`)), http.StatusServiceUnavailable},
		"cron del method":        {api.handleCronDel, httptest.NewRequest(http.MethodGet, "/cron/del", nil), http.StatusMethodNotAllowed},
		"cron exec method":       {api.handleCronExec, httptest.NewRequest(http.MethodGet, "/cron/exec", nil), http.StatusMethodNotAllowed},
		"cron info method":       {api.handleCronInfo, httptest.NewRequest(http.MethodPost, "/cron/info", nil), http.StatusMethodNotAllowed},
		"cron edit method":       {api.handleCronEdit, httptest.NewRequest(http.MethodGet, "/cron/edit", nil), http.StatusMethodNotAllowed},
		"timer add method":       {api.handleTimerAdd, httptest.NewRequest(http.MethodGet, "/timer/add", nil), http.StatusMethodNotAllowed},
		"timer info method":      {api.handleTimerInfo, httptest.NewRequest(http.MethodPost, "/timer/info", nil), http.StatusMethodNotAllowed},
		"timer del method":       {api.handleTimerDel, httptest.NewRequest(http.MethodGet, "/timer/del", nil), http.StatusMethodNotAllowed},
		"relay send method":      {api.handleRelaySend, httptest.NewRequest(http.MethodGet, "/relay/send", nil), http.StatusMethodNotAllowed},
		"relay bind method":      {api.handleRelayBind, httptest.NewRequest(http.MethodGet, "/relay/bind", nil), http.StatusMethodNotAllowed},
		"relay binding no relay": {api.handleRelayBinding, httptest.NewRequest(http.MethodGet, "/relay/binding?chat_id=x", nil), http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.handler(rec, tc.req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	store, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api.cron = NewCronScheduler(store)
	rec := httptest.NewRecorder()
	api.handleCronAdd(rec, httptest.NewRequest(http.MethodPost, "/cron/add", strings.NewReader(`{"cron_expr":"* * * * *","prompt":"p"}`)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "project is required") {
		t.Fatalf("cron add missing project status/body = %d/%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	api.handleCronInfo(rec, httptest.NewRequest(http.MethodGet, "/cron/info?id=missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cron info missing status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.handleCronEdit(rec, httptest.NewRequest(http.MethodPost, "/cron/edit", strings.NewReader(`{"id":"missing","field":"description","value":"x"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cron edit missing status = %d", rec.Code)
	}

	timerStore, err := NewTimerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api.timer = NewTimerScheduler(timerStore)
	rec = httptest.NewRecorder()
	api.handleTimerAdd(rec, httptest.NewRequest(http.MethodPost, "/timer/add", strings.NewReader(`{"delay":"bad","prompt":"p","project":"p","session_key":"s"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("timer add invalid delay status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.handleTimerInfo(rec, httptest.NewRequest(http.MethodGet, "/timer/info?id=missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("timer info missing status = %d", rec.Code)
	}

	relay := NewRelayManager("")
	api.relay = relay
	rec = httptest.NewRecorder()
	api.handleRelayBind(rec, httptest.NewRequest(http.MethodPost, "/relay/bind", strings.NewReader(`{"chat_id":"chat","bots":{"one":"One"}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("relay bind invalid status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	api.handleRelayBinding(rec, httptest.NewRequest(http.MethodGet, "/relay/binding?chat_id=missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("relay binding missing status = %d", rec.Code)
	}
}

func TestRelayManagerBindingHelpersAndPersistence(t *testing.T) {
	dir := t.TempDir()
	rm := NewRelayManager(dir)
	e := NewEngine("alpha", &stubAgent{}, []Platform{&stubPlatformEngine{n: "feishu"}}, "", LangEnglish)
	defer e.cancel()
	rm.RegisterEngine("alpha", e)
	if !rm.HasEngine("alpha") {
		t.Fatal("expected registered engine")
	}
	if names := rm.ListEngineNames(); len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("engine names = %#v", names)
	}

	rm.SetTimeout(-time.Second)
	rm.SetVisibility("unknown")
	rm.AddToBind("feishu", "chat", "alpha")
	rm.AddToBind("feishu", "chat", "beta")
	if got := rm.ListBoundBots("chat", "alpha"); len(got) != 1 || got["beta"] != "beta" {
		t.Fatalf("bound bots = %#v", got)
	}
	if !rm.RemoveFromBind("chat", "alpha") {
		t.Fatal("RemoveFromBind(alpha) = false")
	}
	if rm.RemoveFromBind("chat", "missing") {
		t.Fatal("RemoveFromBind(missing) = true")
	}

	reloaded := NewRelayManager(dir)
	if binding := reloaded.GetBinding("chat"); binding == nil || binding.Bots["beta"] != "beta" {
		t.Fatalf("reloaded binding = %#v", binding)
	}
	reloaded.Unbind("chat")
	if binding := reloaded.GetBinding("chat"); binding != nil {
		t.Fatalf("binding after unbind = %#v", binding)
	}
}

func TestEngineCommandAndProviderCoveragePaths(t *testing.T) {
	p := &stubPlatformEngine{n: "plain"}
	agent := &coverageAgent{
		workDir:   t.TempDir(),
		providers: []ProviderConfig{{Name: "openai", APIKey: "sk-old", Model: "gpt-main"}},
	}
	e := NewEngine("project", agent, []Platform{p}, filepath.Join(t.TempDir(), "sessions.json"), LangEnglish)
	defer e.cancel()
	msg := &Message{SessionKey: "feishu:chat:user", ReplyCtx: "ctx", UserID: "admin", UserName: "Ada", Platform: "feishu"}

	e.SetProviderSaveFunc(func(providerName string) error { return nil })
	e.SetProviderAddSaveFunc(func(ProviderConfig) error { return nil })
	e.SetProviderRemoveSaveFunc(func(string) error { return nil })
	e.SetDisplaySaveFunc(func(*string, *bool, *int, *int, *bool) error { return nil })

	e.cmdAllow(p, msg, nil)
	e.cmdAllow(p, msg, []string{"Bash"})
	if len(agent.allowed) != 1 || agent.allowed[0] != "Bash" {
		t.Fatalf("allowed tools = %#v", agent.allowed)
	}

	e.cmdProvider(p, msg, nil)
	e.cmdProvider(p, msg, []string{"list"})
	e.cmdProvider(p, msg, []string{"current"})
	e.cmdProvider(p, msg, []string{"add", `{"name":"relay","api_key":"sk-new","base_url":"https://example.invalid","model":"m"}`})
	e.cmdProvider(p, msg, []string{"switch", "relay"})
	e.cmdProvider(p, msg, []string{"clear"})
	e.cmdProvider(p, msg, []string{"remove", "relay"})
	e.cmdProvider(p, msg, []string{"remove", "missing"})
	if len(agent.providers) != 1 || agent.providers[0].Name != "openai" {
		t.Fatalf("providers after add/remove = %#v", agent.providers)
	}

	e.cmdAlias(p, msg, []string{"add", "hi", "status"})
	e.cmdAlias(p, msg, []string{"list"})
	if e.aliases["hi"] != "/status" {
		t.Fatalf("alias hi = %q", e.aliases["hi"])
	}
	card := e.renderAliasCard()
	if card == nil || !strings.Contains(card.RenderText(), "hi") {
		t.Fatalf("alias card = %#v", card)
	}
	e.cmdAlias(p, msg, []string{"del", "hi"})
	e.cmdAlias(p, msg, []string{"unknown"})

	e.cmdConfig(p, msg, nil)
	e.cmdConfig(p, msg, []string{"get", "display_mode"})
	e.cmdConfig(p, msg, []string{"set", "display_mode", "quiet"})
	e.cmdConfig(p, msg, []string{"thinking_messages", "true"})
	e.cmdConfig(p, msg, []string{"set", "tool_max_len", "123"})
	e.cmdConfig(p, msg, []string{"set", "tool_max_len", "-1"})
	e.SetConfigReloadFunc(func() (*ConfigReloadResult, error) {
		return &ConfigReloadResult{DisplayUpdated: true, ProvidersUpdated: 1, CommandsUpdated: 2}, nil
	})
	e.cmdConfigReload(p, msg)
	e.SetConfigReloadFunc(nil)
	e.cmdConfigReload(p, msg)

	e.cmdWhoami(p, msg)
	e.cmdDoctor(p, msg)
	e.cmdUpgrade(p, msg, nil)
	e.cmdUpgradeConfirm(p, msg)

	select {
	case <-RestartCh:
	default:
	}
	e.cmdRestart(p, msg)
	select {
	case req := <-RestartCh:
		if req.SessionKey != msg.SessionKey || req.Platform != "plain" {
			t.Fatalf("restart request = %#v", req)
		}
	default:
		t.Fatal("expected restart request")
	}

	if len(p.getSent()) == 0 {
		t.Fatal("expected command replies")
	}
}

func TestEngineTimerHeartbeatAndFormattingCoveragePaths(t *testing.T) {
	p := &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "card"}}
	e := NewEngine("project", &stubAgent{}, []Platform{p}, "", LangEnglish)
	defer e.cancel()
	msg := &Message{SessionKey: "feishu:chat:user", ReplyCtx: "ctx"}

	timerStore, err := NewTimerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	timer := NewTimerScheduler(timerStore)
	e.SetTimerScheduler(timer)
	if err := timerStore.Add(&TimerJob{
		ID:          "timer-1",
		Project:     "project",
		SessionKey:  msg.SessionKey,
		ScheduledAt: time.Now().Add(10 * time.Minute),
		Description: "Check status",
		Mute:        true,
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if card := e.renderTimerCard(msg.SessionKey, "user"); card == nil || !strings.Contains(card.RenderText(), "Check status") {
		t.Fatalf("timer card = %#v", card)
	}
	e.cmdTimer(p, msg, nil)
	e.cmdTimer(p, msg, []string{"list"})
	e.cmdTimer(p, msg, []string{"mute", "timer-1"})
	e.cmdTimer(p, msg, []string{"unmute", "timer-1"})
	e.cmdTimer(p, msg, []string{"delete", "timer-1"})

	heartbeat := NewHeartbeatScheduler(t.TempDir())
	heartbeat.Register("project", HeartbeatConfig{
		Enabled:      true,
		IntervalMins: 3,
		SessionKey:   msg.SessionKey,
	}, e, "")
	e.SetHeartbeatScheduler(heartbeat)
	e.cmdHeartbeat(p, msg, []string{"status"})
	e.cmdHeartbeat(p, msg, []string{"pause"})
	e.cmdHeartbeat(p, msg, []string{"resume"})
	e.cmdHeartbeat(p, msg, []string{"interval"})
	e.cmdHeartbeat(p, msg, []string{"interval", "bad"})
	e.cmdHeartbeat(p, msg, []string{"interval", "7"})
	e.cmdHeartbeat(p, msg, []string{"run"})

	stateStr, yesNo := e.heartbeatLocalizedHelpers()
	if stateStr(true) == stateStr(false) || yesNo(true) == yesNo(false) {
		t.Fatal("heartbeat localized helpers should distinguish states")
	}
	for _, lang := range []Language{LangEnglish, LangChinese, LangTraditionalChinese, LangJapanese, LangSpanish} {
		if formatDurationI18n(49*time.Hour+3*time.Minute, lang) == "" ||
			usageUnavailableText(lang) == "" ||
			usageAccountLabel(lang) == "" ||
			usageWindowLabel(lang, 18000) == "" ||
			usageWindowLabel(lang, 123) == "" ||
			usageRemainingLabel(lang) == "" ||
			usageResetLabel(lang) == "" ||
			usageCardTitle(lang) == "" {
			t.Fatalf("empty localized helper for %q", lang)
		}
	}
	if formatUsageResetTime(LangEnglish, 0) != "-" || formatUsageResetTime(LangEnglish, 60) == "-" {
		t.Fatal("unexpected usage reset formatting")
	}

	report := &UsageReport{
		Email: "user@example.com",
		Plan:  "pro",
		Buckets: []UsageBucket{{
			Windows: []UsageWindow{
				{Name: "five", UsedPercent: 25, WindowSeconds: 18000, ResetAfterSeconds: 60},
				{Name: "week", UsedPercent: 125, WindowSeconds: 604800, ResetAfterSeconds: 120},
			},
		}},
	}
	text := formatUsageReport(report, LangEnglish)
	if !strings.Contains(text, "user@example.com") || !strings.Contains(text, "75%") || !strings.Contains(text, "0%") {
		t.Fatalf("usage report = %s", text)
	}
	if got := formatUsageReport(nil, LangEnglish); !strings.Contains(got, "unavailable") {
		t.Fatalf("nil usage report = %q", got)
	}
}

func TestEngineMiscHelpersAndSetterCoveragePaths(t *testing.T) {
	e := NewEngine("project", &stubAgent{}, []Platform{&stubPlatformEngine{n: "feishu"}}, "", LangEnglish)
	defer e.cancel()

	hooks := NewHookManager("project", nil, "", "", "")
	e.SetHooks(hooks)
	e.SetShell("sh", "-c", "profile")
	e.SetWebSetupFunc(func() (int, string, bool, error) { return 3000, "token", true, nil })
	e.SetWebStatusFunc(func() string { return "running" })
	e.SetSkipGit(true)
	e.SetProviderRefsSaveFunc(func([]string) error { return nil })
	e.SetListGlobalProvidersFunc(func(string) ([]ProviderConfig, error) {
		return []ProviderConfig{{Name: "global"}}, nil
	})
	e.SetRelayManager(NewRelayManager(""))
	e.SetDataDir(t.TempDir())
	if e.GetSessions() == nil || e.ProjectName() != "project" || e.RelayManager() == nil {
		t.Fatal("basic engine accessors returned nil/empty")
	}
	e.AddCommand("x", "", "prompt", "", "", "config")
	if !e.RemoveCommand("x") || e.RemoveCommand("x") {
		t.Fatal("RemoveCommand returned unexpected result")
	}
	e.interactiveStates["feishu:chat:user"] = &interactiveState{platform: &stubPlatformEngine{n: "feishu"}}
	if keys := e.ActiveSessionKeys(); len(keys) != 1 || keys[0] != "feishu:chat:user" {
		t.Fatalf("active session keys = %#v", keys)
	}

	if previewText("abcdef", 3) != "abc...(truncated)" || previewText("abc", 3) != "abc" || previewText("abc", 0) != "" {
		t.Fatal("previewText returned unexpected result")
	}
	if timerRunTitle(&TimerJob{Description: "desc"}) != "desc" || cronRunTitle(&CronJob{Description: "desc"}) != "desc" {
		t.Fatal("run title should prefer description")
	}
	if cronTimeFormat(time.Date(2027, 1, 2, 3, 4, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) != "2006-01-02 15:04" {
		t.Fatal("cronTimeFormat should include year for different year")
	}
	if !stringSliceContains([]string{"a", "b"}, "b") || stringSliceContains([]string{"a"}, "c") {
		t.Fatal("stringSliceContains returned unexpected result")
	}
	title, body := splitCardTitleBody("Title\n\nBody")
	if title != "Title" || body != "Body" {
		t.Fatalf("splitCardTitleBody = %q/%q", title, body)
	}
	if rows := splitHelpTabRows(false, []CardButton{{Text: "a"}, {Text: "b"}}); len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("single-row help tabs = %#v", rows)
	}
	if e.cardPrevButton("nav:/prev").Value != "nav:/prev" || e.cardNextButton("nav:/next").Value != "nav:/next" {
		t.Fatal("card navigation buttons lost action")
	}
}

func TestProviderProxyAndSpeechHelperCoveragePaths(t *testing.T) {
	body := `{"thinking":{"type":"adaptive","budget_tokens":1024},"messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	rewriteThinkingInRequest(req, "disabled")
	var rewritten map[string]any
	if err := json.NewDecoder(req.Body).Decode(&rewritten); err != nil {
		t.Fatal(err)
	}
	thinking := rewritten["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Fatalf("thinking after rewrite = %#v", thinking)
	}
	if _, ok := thinking["budget_tokens"]; ok {
		t.Fatalf("budget_tokens should be removed for disabled: %#v", thinking)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`not-json`))
	rewriteThinkingInRequest(req, "disabled")
	raw, _ := io.ReadAll(req.Body)
	if string(raw) != "not-json" {
		t.Fatalf("invalid JSON body changed to %q", raw)
	}

	if !NeedsConversion("amr") || NeedsConversion("mp3") {
		t.Fatal("NeedsConversion returned unexpected result")
	}
	for _, tc := range map[string]string{
		"amr":  "amr",
		"opus": "ogg",
		"mp4":  "m4a",
		"wav":  "wav",
		"silk": "silk",
	} {
		if got := formatToExt(tc); got == "" {
			t.Fatalf("formatToExt(%q) = %q", tc, got)
		}
	}
	for _, format := range []string{"mp3", "wav", "ogg", "m4a", "webm", "unknown"} {
		if got := formatToAudioMIME(format); !strings.HasPrefix(got, "audio/") {
			t.Fatalf("formatToAudioMIME(%q) = %q", format, got)
		}
	}
}

func TestCardBuilderAndAPIServerLifecycleCoveragePaths(t *testing.T) {
	card := NewCard().
		Title("Title", "blue").
		Markdown("hello").
		Markdownf("%s %d", "value", 7).
		Divider().
		Buttons(PrimaryBtn("Run", "act:/run"), DefaultBtn("Back", "nav:/back")).
		ButtonsEqual(DangerBtn("Delete", "act:/delete")).
		ListItem("row", "Open", "act:/open").
		ListItemBtn("danger row", "Remove", "danger", "act:/remove").
		ListItemBtnExtra("extra row", "More", "default", "act:/more", map[string]string{"k": "v"}).
		Select("Pick", []CardSelectOption{{Text: "One", Value: "1"}, {Text: "Two", Value: "2"}}, "2").
		Note("note").
		TaggedNote("tag", "tagged").
		Build()
	text := card.RenderText()
	for _, want := range []string{"Title", "hello", "value 7", "Run", "Pick", "tagged"} {
		if !strings.Contains(text, want) {
			t.Fatalf("card text missing %q: %s", want, text)
		}
	}
	if !card.HasButtons() {
		t.Fatal("expected card to report interactive elements")
	}
	if rows := card.CollectButtons(); len(rows) != 5 {
		t.Fatalf("button rows = %#v", rows)
	}
	empty := NewCard().Markdown("").Buttons().Select("", nil, "").Note("").Build()
	if empty.HasButtons() || empty.RenderText() != "" {
		t.Fatalf("empty card rendered unexpectedly: %#v text=%q", empty, empty.RenderText())
	}

	dataDir := filepath.Join("/tmp", "cc-connect-api-"+time.Now().Format("150405.000000000"))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	api, err := NewAPIServer(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(api.SocketPath(), filepath.Join(dataDir, "run")) {
		t.Fatalf("socket path = %q", api.SocketPath())
	}
	rm := NewRelayManager("")
	api.SetRelayManager(rm)
	if api.RelayManager() != rm {
		t.Fatal("relay manager accessor mismatch")
	}
	cronStore, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	timerStore, err := NewTimerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	api.SetCronScheduler(NewCronScheduler(cronStore))
	api.SetTimerScheduler(NewTimerScheduler(timerStore))
	api.SetMaxAttachmentSize(1024)
	if got, want := api.sendBodyLimit(), int64(1024*4/3)+sendBodyEnvelope; got != want {
		t.Fatalf("send body limit = %d, want %d", got, want)
	}
	api.SetMaxAttachmentSize(0)
	if got, want := api.sendBodyLimit(), int64(1024*4/3)+sendBodyEnvelope; got != want {
		t.Fatalf("non-positive size should be ignored: got %d want %d", got, want)
	}
	e := NewEngine("project", &stubAgent{}, []Platform{&stubPlatformEngine{n: "feishu"}}, "", LangEnglish)
	defer e.cancel()
	api.RegisterEngine("project", e)
	if !rm.HasEngine("project") {
		t.Fatal("RegisterEngine should also register relay engine when relay is configured")
	}
	api.Start()
	api.Stop()
	if _, err := os.Stat(api.SocketPath()); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed after Stop, err=%v", err)
	}
}

func TestSpeechHTTPClientsAndTranscribeCoveragePaths(t *testing.T) {
	whisperServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("unexpected whisper request path=%s auth=%s", r.URL.Path, r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if got := r.FormValue("model"); got != "whisper-test" {
			t.Fatalf("whisper model = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"text":" hello whisper "}`))
	}))
	defer whisperServer.Close()
	whisper := NewOpenAIWhisper("key", whisperServer.URL+"/", "whisper-test")
	got, err := whisper.Transcribe(context.Background(), []byte("audio"), "wav", "en")
	if err != nil || strings.TrimSpace(got) != "hello whisper" {
		t.Fatalf("whisper transcript=%q err=%v", got, err)
	}
	whisper.Client = whisperServer.Client()

	qwenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer qkey" {
			t.Fatalf("unexpected qwen request path=%s auth=%s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "qwen-test" {
			t.Fatalf("qwen payload = %#v", payload)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" qwen text "}}]}`))
	}))
	defer qwenServer.Close()
	qwen := NewQwenASR("qkey", qwenServer.URL+"/", "qwen-test")
	got, err = qwen.Transcribe(context.Background(), []byte("audio"), "mp3", "")
	if err != nil || got != "qwen text" {
		t.Fatalf("qwen transcript=%q err=%v", got, err)
	}

	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-test:generateContent" || r.Header.Get("x-goog-api-key") != "gkey" {
			t.Fatalf("unexpected gemini request path=%s key=%s", r.URL.Path, r.Header.Get("x-goog-api-key"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, ok := payload["contents"].([]any); !ok {
			t.Fatalf("gemini payload = %#v", payload)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":" gemini text "}]}}]}`))
	}))
	defer geminiServer.Close()
	gemini := NewGeminiSTT("gkey", "gemini-test")
	gemini.BaseURL = geminiServer.URL
	gemini.Client = geminiServer.Client()
	got, err = gemini.Transcribe(context.Background(), []byte("audio"), "ogg", "English")
	if err != nil || got != "gemini text" {
		t.Fatalf("gemini transcript=%q err=%v", got, err)
	}

	stt := &coverageSpeechToText{}
	got, err = TranscribeAudio(context.Background(), stt, &AudioAttachment{Data: []byte("audio"), Format: "mp3"}, "zh")
	if err != nil || got != "transcribed text" {
		t.Fatalf("TranscribeAudio transcript=%q err=%v", got, err)
	}
	if string(stt.data) != "audio" || stt.format != "mp3" || stt.lang != "zh" {
		t.Fatalf("stt call = data=%q format=%q lang=%q", stt.data, stt.format, stt.lang)
	}
}

func TestEngineFooterTTSProviderAndBindCoveragePaths(t *testing.T) {
	p := &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "card"}}
	agent := &coverageAgent{
		model:     "agent-model",
		reasoning: "medium",
		workDir:   t.TempDir(),
		providers: []ProviderConfig{{Name: "openai", Model: "gpt-main"}},
	}
	e := NewEngine("project", agent, []Platform{p}, "", LangEnglish)
	defer e.cancel()

	session := &coverageContextSession{
		model:     "session-model",
		reasoning: "high",
		workDir:   t.TempDir(),
		usage: &ContextUsage{
			UsedTokens:               160,
			ContextWindow:            200,
			OutputTokens:             1200,
			InputTokens:              34,
			CacheCreationInputTokens: 56,
			CachedInputTokens:        7890,
		},
	}
	e.SetReplyFooterEnabled(true)
	e.SetShowContextIndicator(true)
	e.SetShowWorkdirIndicator(true)
	footer := e.composeRichStatusFooter(false, time.Now().Add(-90*time.Second), agent, session, "")
	for _, want := range []string{"Elapsed", "session-model", "high", "out 1.2k", "ctx 80%", filepath.Base(session.workDir)} {
		if !strings.Contains(footer, want) {
			t.Fatalf("rich footer missing %q: %s", want, footer)
		}
	}
	if got := e.composeRichStatusFooter(true, time.Now(), agent, session, ""); got != "" {
		t.Fatalf("streaming footer should be empty, got %q", got)
	}
	if got := e.buildReplyFooter(agent, session, "", "[ctx: 20%]"); !strings.Contains(got, "[ctx: 20%]") || !strings.Contains(got, filepath.Base(session.workDir)) {
		t.Fatalf("reply footer = %q", got)
	}
	if appendFinalMetadataToSegment("partial", "full\n\n*metadata*") != "partial\n\n*metadata*" {
		t.Fatal("appendFinalMetadataToSegment should append final markdown metadata")
	}
	if e.formatShellProgress("ls", strings.Repeat("x", 20), 5) == e.formatShellTimeout("ls", strings.Repeat("x", 20), 5) {
		t.Fatal("shell progress and timeout formats should differ")
	}
	if toolCodeLang("write_file", "\n- old\n+ new") != "diff" || toolCodeLang("Bash", "ls") != "bash" {
		t.Fatal("toolCodeLang returned unexpected language")
	}
	exitCode := 2
	success := false
	if got := e.formatToolResultEventFallback("tool", "", "", &exitCode, &success); !strings.Contains(got, "2") || !strings.Contains(got, "No output") {
		t.Fatalf("tool result fallback = %q", got)
	}

	msg := &Message{SessionKey: "feishu:chat:user", ReplyCtx: "ctx", UserID: "admin", UserName: "Ada", Platform: "feishu"}
	e.SetTTSConfig(&TTSCfg{Enabled: true, Provider: "test", Voice: "voice", Speed: 1.2, TTS: &recordingTTS{}, MaxTextLen: 5})
	e.cmdTTS(p, msg, nil)
	e.cmdTTS(p, msg, []string{"always"})
	e.cmdTTS(p, msg, []string{"bad"})
	if err := e.synthesizeAndSendTTS(p, msg.ReplyCtx, "too long for max"); err == nil {
		t.Fatal("expected max text length error")
	}
	e.SetTTSConfig(&TTSCfg{Enabled: true, Provider: "test", Voice: "voice", Speed: 1.2, TTS: &recordingTTS{}})
	audioPlatform := &audioStubPlatform{stubPlatformEngine: stubPlatformEngine{n: "audio"}}
	if err := e.synthesizeAndSendTTS(audioPlatform, msg.ReplyCtx, "**hello**"); err != nil {
		t.Fatalf("synthesizeAndSendTTS: %v", err)
	}

	e.setPendingProviderAdd(msg.SessionKey, &pendingProviderAddState{phase: "other"})
	if card := e.renderProviderAddCard(msg.SessionKey); card == nil || !strings.Contains(card.RenderText(), "/provider add") {
		t.Fatalf("pending provider card = %#v", card)
	}
	if !e.handlePendingProviderAdd(p, msg, "newprov sk https://example.invalid model", msg.SessionKey) {
		t.Fatal("pending provider add should consume input")
	}
	if len(agent.providers) != 2 || agent.providers[1].Name != "newprov" {
		t.Fatalf("providers after pending add = %#v", agent.providers)
	}
	if e.handlePendingProviderAdd(p, msg, "/provider", msg.SessionKey) {
		t.Fatal("slash command should bypass pending provider add")
	}

	rm := NewRelayManager("")
	rm.RegisterEngine("project", e)
	rm.RegisterEngine("other", NewEngine("other", &stubAgent{}, []Platform{p}, "", LangEnglish))
	e.SetRelayManager(rm)
	e.cmdBind(p, msg, nil)
	e.cmdBind(p, msg, []string{"project"})
	e.cmdBind(p, msg, []string{"missing"})
	e.cmdBind(p, msg, []string{"other"})
	e.cmdBind(p, msg, []string{"-other"})
	e.cmdBind(p, msg, []string{"remove"})
	e.cmdBind(p, msg, []string{"help"})
	if len(p.getSent()) == 0 && len(p.repliedCards) == 0 {
		t.Fatal("expected replies from engine coverage paths")
	}
}

func TestProviderProxyBridgeSetupAndRunAsAuditCoveragePaths(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Host != strings.TrimPrefix(upstreamURL(t, r), "http://") {
			t.Fatalf("unexpected proxied request path=%s host=%s", r.URL.Path, r.Host)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		thinking := payload["thinking"].(map[string]any)
		if thinking["type"] != "disabled" {
			t.Fatalf("thinking payload = %#v", thinking)
		}
		if _, ok := thinking["budget_tokens"]; ok {
			t.Fatalf("budget_tokens should be stripped: %#v", thinking)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	proxy, localURL, err := NewProviderProxy(upstream.URL, "disabled")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	resp, err := http.Post(localURL+"/v1/messages", "application/json", strings.NewReader(`{"thinking":{"type":"adaptive","budget_tokens":128}}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d", resp.StatusCode)
	}
	proxy.Close()
	proxy.Close()

	regServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/v1/app/registration" {
			t.Fatalf("registration path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("action") != "poll" || r.Form.Get("device_code") != "dev" {
			t.Fatalf("form = %#v", r.Form)
		}
		_, _ = w.Write([]byte(`{"client_id":"id","client_secret":"secret","user_info":{"tenant_brand":"feishu","open_id":"ou"}}`))
	}))
	defer regServer.Close()
	reg, err := feishuRegistrationCall(regServer.Client(), regServer.URL, "poll", map[string]string{"device_code": "dev"})
	if err != nil || reg["client_id"] != "id" {
		t.Fatalf("registration response=%#v err=%v", reg, err)
	}

	mgmt := NewManagementServer(0, "", nil)
	var saved FeishuSetupSaveRequest
	mgmt.SetSetupFeishuSave(func(req FeishuSetupSaveRequest) error {
		saved = req
		return nil
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(`{"project":"p","app_id":"app","app_secret":"sec","platform_type":"feishu","owner_open_id":"ou"}`))
	mgmt.handleSetupFeishuSave(rec, req)
	if rec.Code != http.StatusOK || saved.ProjectName != "p" || saved.AppID != "app" {
		t.Fatalf("setup save code=%d saved=%#v body=%s", rec.Code, saved, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mgmt.handleSetupFeishuSave(rec, httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty setup save code=%d", rec.Code)
	}

	bs := NewBridgeServerInsecure(0, "", "bridge/ws", []string{"https://app.example"})
	if bs == nil {
		t.Fatal("expected insecure bridge server")
	}
	corsReq := httptest.NewRequest(http.MethodOptions, "/bridge/sessions", nil)
	corsReq.Header.Set("Origin", "https://app.example")
	corsRec := httptest.NewRecorder()
	bs.corsHTTP(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("OPTIONS should not call wrapped handler")
	})(corsRec, corsReq)
	if corsRec.Code != http.StatusNoContent || corsRec.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("cors response code=%d headers=%#v", corsRec.Code, corsRec.Header())
	}

	bp := bs.NewPlatform("project")
	msgCh := make(chan *Message, 3)
	if err := bp.Start(func(_ Platform, msg *Message) { msgCh <- msg }); err != nil {
		t.Fatal(err)
	}
	bs.enginesMu.Lock()
	bs.engines["project"] = &bridgeEngineRef{engine: nil, platform: bp}
	bs.enginesMu.Unlock()
	adapter := &bridgeAdapter{
		platform:        "web",
		capabilities:    map[string]bool{"card": false},
		metadata:        map[string]any{"progress_style": "compact"},
		server:          bs,
		previewRequests: make(map[string]chan string),
	}
	adapter.dispatchAsMessage(bs.engines["project"], "web:chat:user", "rctx", "/help")
	adapter.dispatchAsPermissionResponse(bs.engines["project"], "web:chat:user", "rctx", "allow")
	for i := 0; i < 2; i++ {
		select {
		case msg := <-msgCh:
			if msg.UserID != "web-admin" || msg.ReplyCtx == nil {
				t.Fatalf("bridge dispatched message = %#v", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for bridge dispatch")
		}
	}
	ackCh := make(chan string, 1)
	adapter.previewRequests["ref"] = ackCh
	adapter.handlePreviewAck(json.RawMessage(`{"type":"preview_ack","ref_id":"ref","preview_handle":"handle"}`))
	if got := <-ackCh; got != "handle" {
		t.Fatalf("preview ack = %q", got)
	}

	var report IsolationReport
	parseProbeOutput(&report, strings.Join([]string{
		"BEGIN probe-version=1",
		"ID uid=501(target)",
		"WHOAMI target",
		"GROUPS staff",
		"UMASK 0022",
		"PWD /tmp",
		"HOME /Users/target",
		"SHELL /bin/zsh",
		"WORKDIR_PATH /work",
		"WORKDIR_EXISTS yes",
		"WORKDIR_READABLE yes",
		"WORKDIR_WRITABLE no",
		"TARGET_HAS /Users/target/.config",
		"CROSS_LEAKED other /Users/other/.secret",
		"CROSS_UNKNOWN ghost",
		"SUPERVISOR_LEAKED /Users/super/.secret",
	}, "\n"))
	report.Project = "p"
	report.RunAsUser = "target"
	report.Fatal = computeAuditFatal(report)
	if !report.HasFatal() || len(report.CrossUser) != 2 || len(report.Supervisor) != 1 {
		t.Fatalf("audit report = %#v", report)
	}
	if pretty, err := report.PrettyJSON(); err != nil || !strings.Contains(string(pretty), `"fatal"`) {
		t.Fatalf("PrettyJSON=%s err=%v", pretty, err)
	}
	if got := shellQuote("a'b"); got != "'a'\\''b'" {
		t.Fatalf("shellQuote = %q", got)
	}
	if got := filterOtherUsers([]string{"", "target", "other"}, "target"); len(got) != 1 || got[0] != "other" {
		t.Fatalf("filterOtherUsers = %#v", got)
	}
	if _, err := RunIsolationProbe(context.Background(), AuditConfig{}); err == nil {
		t.Fatal("RunIsolationProbe should reject empty RunAsUser before sudo")
	}
}

func TestEngineShellAndWorkspaceInitCoveragePaths(t *testing.T) {
	p := &stubCompactProgressPlatform{stubPlatformEngine: stubPlatformEngine{n: "plain"}}
	agent := &coverageAgent{workDir: t.TempDir()}
	e := NewEngine("project", agent, []Platform{p}, "", LangEnglish)
	defer e.cancel()

	if err := e.runShellWithProgress(p, "ctx", "printf ok", agent.workDir, time.Second, 100); err != nil {
		t.Fatalf("runShellWithProgress success: %v", err)
	}
	if sent := strings.Join(p.getSent(), "\n"); !strings.Contains(sent, "ok") {
		t.Fatalf("shell success replies = %q", sent)
	}
	p.clearSent()
	if err := e.runShellWithProgress(p, "ctx", "printf fail; exit 3", agent.workDir, time.Second, 100); err == nil {
		t.Fatal("expected failing shell command error")
	}
	if sent := strings.Join(p.getSent(), "\n"); !strings.Contains(sent, "exit code 3") {
		t.Fatalf("shell failure replies = %q", sent)
	}
	p.clearSent()
	if err := e.runShellWithProgress(p, "ctx", "sleep 1", agent.workDir, 10*time.Millisecond, 100); err == nil {
		t.Fatal("expected shell timeout")
	}
	if sent := strings.Join(p.getSent(), "\n"); !strings.Contains(sent, "timeout") {
		t.Fatalf("shell timeout replies = %q", sent)
	}

	cronJob := &CronJob{Exec: "printf cron", WorkDir: agent.workDir}
	if err := e.executeCronShell(p, "ctx", cronJob); err != nil {
		t.Fatalf("executeCronShell: %v", err)
	}
	timerJob := &TimerJob{Exec: "printf timer", WorkDir: agent.workDir}
	if err := e.executeTimerShell(p, "ctx", timerJob); err != nil {
		t.Fatalf("executeTimerShell: %v", err)
	}
	e.executeShellCommand(p, &Message{ReplyCtx: "ctx", UserName: "Ada"}, &CustomCommand{Name: "echo", Exec: "printf {{arg1}}", WorkDir: agent.workDir}, []string{"cmd"})
	if sent := strings.Join(p.getSent(), "\n"); !strings.Contains(sent, "cron") || !strings.Contains(sent, "timer") || !strings.Contains(sent, "cmd") {
		t.Fatalf("shell command replies = %q", sent)
	}

	baseDir := t.TempDir()
	e.SetDataDir(t.TempDir())
	e.SetMultiWorkspace(baseDir, filepath.Join(t.TempDir(), "bindings.json"))
	e.SetWorkspaceIdleTimeout(0)
	e.skipGit = true
	e.workspaceInitAllowLocalPaths = true
	msg := &Message{SessionKey: "feishu:chat:user", Platform: "feishu", ChannelKey: "chat", Content: "/help", ReplyCtx: "ctx"}
	if e.handleWorkspaceInitFlow(p, msg, "team") {
		t.Fatal("slash command should bypass new workspace init flow")
	}
	msg.Content = "hello"
	if !e.handleWorkspaceInitFlow(p, msg, "team") {
		t.Fatal("skipGit init should consume first message")
	}
	msg.Content = "no"
	if !e.handleWorkspaceInitFlow(p, msg, "team") {
		t.Fatal("cancel confirmation should consume message")
	}
	msg.Content = "hello again"
	_ = e.handleWorkspaceInitFlow(p, msg, "team")
	msg.Content = "yes"
	if !e.handleWorkspaceInitFlow(p, msg, "team") {
		t.Fatal("yes confirmation should consume message")
	}
	if _, err := os.Stat(filepath.Join(baseDir, "team")); err != nil {
		t.Fatalf("workspace dir not created: %v", err)
	}

	e.skipGit = false
	e.workspaceInitAllowLocalPaths = true
	localDir := filepath.Join(baseDir, "existing")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	msg.ChannelKey = "chat2"
	msg.Content = "existing"
	if !e.handleWorkspaceInitFlow(p, msg, "existing") {
		t.Fatal("local dir init should consume message")
	}
	if got, err := resolveLocalDirPath("../escape", baseDir); err == nil || got != "" {
		t.Fatalf("expected escape rejection, got %q err=%v", got, err)
	}
	for _, tc := range []string{"https://host/org/repo.git", "git@host:org/repo.git", "ssh://host/org/repo"} {
		if got := extractRepoName(tc); got != "repo" {
			t.Fatalf("extractRepoName(%q) = %q", tc, got)
		}
	}
	if !looksLikeGitURL("git@host:org/repo") || !looksLikeLocalDir("./repo") || looksLikeLocalDir("/help") {
		t.Fatal("workspace path helpers returned unexpected values")
	}
}

func TestEngineRemainingCommandAndCardCoveragePaths(t *testing.T) {
	p := &stubCardPlatform{stubPlatformEngine: stubPlatformEngine{n: "feishu"}}
	agent := &coverageAgent{
		model:     "gpt-main",
		reasoning: "medium",
		workDir:   t.TempDir(),
		providers: []ProviderConfig{{Name: "openai", Model: "gpt-main"}},
	}
	e := NewEngine("project", agent, []Platform{p}, "", LangEnglish)
	defer e.cancel()
	e.SetAdminFrom("*")
	msg := &Message{SessionKey: "feishu:chat:user", ReplyCtx: "ctx", UserID: "admin"}

	e.SetMaxTurnTime(time.Second)
	e.cmdStart(p, msg)
	if got := p.getSent(); len(got) == 0 || !strings.Contains(got[len(got)-1], "project") {
		t.Fatalf("start replies = %#v", got)
	}

	e.SetWebSetupFunc(func() (int, string, bool, error) { return 8080, "tok", true, nil })
	e.SetWebStatusFunc(func() string { return "http://localhost:8080" })
	e.cmdWeb(p, msg, []string{"status"})
	e.cmdWeb(p, msg, []string{"setup"})
	e.SetWebStatusFunc(func() string { return "" })
	e.cmdWeb(p, msg, []string{"status"})

	if card := e.renderDoctorCard(); card == nil || !strings.Contains(card.RenderText(), "System Diagnostic") {
		t.Fatalf("doctor card = %#v", card)
	}
	prevVersion := CurrentVersion
	CurrentVersion = "dev"
	if card := e.renderUpgradeCard(); card == nil || !strings.Contains(strings.ToLower(card.RenderText()), "dev") {
		t.Fatalf("upgrade dev card = %#v", card)
	}
	CurrentVersion = prevVersion

	if card := e.renderModelSwitchingCard("gpt-fast"); card == nil || !strings.Contains(card.RenderText(), "gpt-fast") {
		t.Fatalf("switching card = %#v", card)
	}
	if card := e.renderModelSwitchResultCard("gpt-fast", nil); card == nil || !strings.Contains(card.RenderText(), "gpt-fast") {
		t.Fatalf("switch result card = %#v", card)
	}
	if card := e.renderModelSwitchResultCard("", os.ErrNotExist); card == nil || !strings.Contains(card.RenderText(), "Failed") {
		t.Fatalf("switch error card = %#v", card)
	}
	state := &interactiveState{modelSwitch: &modelSwitchState{phase: "switching", target: "gpt-fast"}}
	e.interactiveStates[msg.SessionKey] = state
	e.performModelSwitchAsync(msg.SessionKey, state, agent, e.sessions, "gpt-fast")
	if agent.GetModel() != "gpt-fast" {
		t.Fatalf("model after async switch = %q", agent.GetModel())
	}
	if len(p.refreshedCards) == 0 {
		t.Fatal("expected model switch result card refresh")
	}

	linkedRefs := []string{}
	e.SetListGlobalProvidersFunc(func(string) ([]ProviderConfig, error) {
		return []ProviderConfig{{Name: "linked", Model: "gpt-linked"}}, nil
	})
	e.SetProviderRefsSaveFunc(func(refs []string) error {
		linkedRefs = append([]string(nil), refs...)
		return nil
	})
	e.executeProviderLink(msg.SessionKey, "linked")
	if len(agent.providers) != 2 || agent.providers[1].Name != "linked" || len(linkedRefs) != 2 {
		t.Fatalf("provider link providers=%#v refs=%#v", agent.providers, linkedRefs)
	}
	e.executeProviderLink(msg.SessionKey, "")
	e.executeProviderLink(msg.SessionKey, "missing")

	cronStore, err := NewCronStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cronScheduler := NewCronScheduler(cronStore)
	e.SetCronScheduler(cronScheduler)
	e.cmdCronAddExec(p, msg, nil)
	e.cmdCronAddExec(p, &Message{SessionKey: msg.SessionKey, ReplyCtx: "ctx", UserID: "user"}, []string{"*", "*", "*", "*", "*", "printf no"})
	e.cmdCronAddExec(p, msg, []string{"*", "*", "*", "*", "*", "printf ok"})
	jobs := cronStore.ListByProject("project")
	if len(jobs) == 0 || !jobs[0].IsShellJob() {
		t.Fatalf("cron shell jobs = %#v", jobs)
	}
	e.cmdCronToggle(p, msg, []string{jobs[0].ID}, false)
	e.cmdCronToggle(p, msg, []string{jobs[0].ID}, true)
	e.cmdCronToggle(p, msg, []string{"missing"}, true)

	timerStore, err := NewTimerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.SetTimerScheduler(NewTimerScheduler(timerStore))
	e.cmdTimerAddExec(p, msg, nil)
	e.cmdTimerAddExec(p, &Message{SessionKey: msg.SessionKey, ReplyCtx: "ctx", UserID: "user"}, []string{"1m", "printf no"})
	e.cmdTimerAddExec(p, msg, []string{"1m", "printf ok"})
	if pending := timerStore.ListPending(); len(pending) == 0 || !pending[0].IsShellJob() {
		t.Fatalf("timer shell jobs = %#v", pending)
	}
	e.sendTTSReply(p, msg.ReplyCtx, "not configured")
}

func upstreamURL(t *testing.T, r *http.Request) string {
	t.Helper()
	if r.TLS != nil {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}
