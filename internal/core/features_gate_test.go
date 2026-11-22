package core

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The featureAccess store is persisted and answers merge into it: a failed
// first features.access.policies.list (offline launch, a 5xx) leaves last
// launch's answer in place, and the read is tried again soon, not in an
// hour.
func TestFeatureAccessFailedFirstReadKeepsCache(t *testing.T) {
	a, f := testApp(t)
	f.reply["features.access.policies.list"] = `{"ok":false,"error":"internal_error"}`
	a.S.SetFeatureAccess(map[string]bool{FeatureUserGroupsManage: true})
	a.installCachedFeatures()
	a.loadFeatures(context.Background())
	on, known := a.FeatureState(FeatureUserGroupsManage)
	if !on || !known {
		t.Errorf("after a failed first read: on %v, known %v; want the cached answer", on, known)
	}
	wait := time.Until(a.featureRetryAt())
	if wait <= 0 || wait > time.Minute {
		t.Errorf("the next read is %v away, want about 30 s", wait)
	}
	if c, ok := a.S.CachedFeatureAccess(FeatureUserGroupsManage); !ok || !c {
		t.Errorf("the failed read changed the persisted answer: %v %v", c, ok)
	}
}

// feature_access_policies_updated before the first read merges into the
// persisted answer: the policies it leaves out keep their cached values,
// for this launch and the next.
func TestFeatureEventBeforeFirstReadMerges(t *testing.T) {
	a, _ := testApp(t)
	a.S.SetFeatureAccess(map[string]bool{FeatureUserGroupsManage: true, "agent_tasks__write": true})
	a.installCachedFeatures()
	b, _ := json.Marshal(map[string]any{"type": "feature_access_policies_updated",
		"feature_access_policies": map[string]any{"agent_tasks__write": map[string]any{"is_enabled": false}}})
	a.onFeatureAccessUpdated(b)
	_ = slack.Event{}
	if !a.FeatureAccess(FeatureUserGroupsManage) || a.FeatureAccess("agent_tasks__write") {
		t.Errorf("after the event: user_groups__manage %v, agent_tasks__write %v", a.FeatureAccess(FeatureUserGroupsManage), a.FeatureAccess("agent_tasks__write"))
	}
	if c, ok := a.S.CachedFeatureAccess(FeatureUserGroupsManage); !ok || !c {
		t.Errorf("the event dropped user_groups__manage from the persisted answer: %v %v", c, ok)
	}
}
