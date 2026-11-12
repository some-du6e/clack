package fakeslack

import (
	"encoding/json"
	"net/http"
	"sync"
)

// The token page (app.slack.com/auth?app=client…, slack.authPage): with
// the fake's d cookie it answers as Slack's does, a script setting
// sessionStorage.incomingConfig to the session's teams map (the workspace,
// a second workspace and the Grid org); with any other cookie, the sign-in
// page and no tokens. Every page load the fake gets outside /api/ is
// logged (PageLoads), so tests can check what a sign-in loaded, and that
// it never asked for /ssb/redirect.

// AuthCookie is the d cookie the fake's token page accepts.
const AuthCookie = "xoxd-fake"

// AuthTeamURL and AuthOrgURL are the addresses the token page gives the
// fake workspace and org; AuthOtherURL is the session's second workspace.
const (
	AuthTeamURL  = "https://fakeclub.slack.com/"
	AuthOrgURL   = "https://fakeclub.enterprise.slack.com/"
	AuthOtherURL = "https://otherclub.slack.com/"
)

// pageLogs is each server's page loads (outside /api/), "path?query".
var pageLogs sync.Map // *Server → *pageLog

type pageLog struct {
	mu    sync.Mutex
	loads []string
}

func (s *Server) logPage(r *http.Request) {
	v, _ := pageLogs.LoadOrStore(s, &pageLog{})
	pl := v.(*pageLog)
	pl.mu.Lock()
	pl.loads = append(pl.loads, r.URL.RequestURI())
	pl.mu.Unlock()
}

// PageLoads lists the pages loaded outside /api/ so far, in order.
func (s *Server) PageLoads() []string {
	v, ok := pageLogs.Load(s)
	if !ok {
		return nil
	}
	pl := v.(*pageLog)
	pl.mu.Lock()
	defer pl.mu.Unlock()
	return append([]string(nil), pl.loads...)
}

func (s *Server) authPage(w http.ResponseWriter, r *http.Request) {
	s.logPage(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if c, err := r.Cookie("d"); err != nil || c.Value != AuthCookie {
		w.Write([]byte(`<html><body>Sign in to Slack</body></html>`))
		return
	}
	cfg, _ := json.Marshal(map[string]any{"teams": map[string]any{
		OtherTeamID:  map[string]any{"id": OtherTeamID, "name": "Other Club", "url": AuthOtherURL, "domain": "otherclub", "token": "xoxc-fake-other"},
		TeamID:       map[string]any{"id": TeamID, "name": "Fake Club", "url": AuthTeamURL, "domain": "fakeclub", "token": "xoxc-fake", "enterprise_api_token": "xoxc-fake-org"},
		EnterpriseID: map[string]any{"id": EnterpriseID, "name": "Fake Org", "url": AuthOrgURL, "token": "xoxc-fake-org"},
	}})
	w.Write([]byte(`<html><script>(function(){ if (!window.parent) return; sessionStorage.incomingConfig = JSON.stringify(` + string(cfg) + `); window.parent.postMessage('gantry_auth_notify', "https:\/\/app.slack.com"); })();</script></html>`))
}

// otherPage answers every page the fake doesn't serve: logged, not found.
func (s *Server) otherPage(w http.ResponseWriter, r *http.Request) {
	s.logPage(r)
	http.NotFound(w, r)
}
