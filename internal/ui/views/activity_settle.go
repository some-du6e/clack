package views

import (
	"reflect"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// unbuildChanged is what settle describes again after a load: the rows
// whose item the answer changed (or brought back), whose conversation
// reads differently now, or whose timestamp text moved on (a day turned
// over: "Yesterday"). A refresh that brought the same items keeps every
// other row's text, so a peek's or a realtime refresh doesn't describe all
// of them again in one frame. The rest of what a row's text reads marks
// it itself as it changes (namesIn, membersIn, the lists' and files'
// titles, live entries). old is the page as the load found it, page its
// answer.
func (v *Activity) unbuildChanged(old, page core.ActivityPage) {
	was := make(map[string]*core.ActivityItem, len(old.Items))
	for i := range old.Items {
		was[actKey(&old.Items[i])] = &old.Items[i]
	}
	now := time.Now()
	for i := range page.Items {
		it := &page.Items[i]
		k := actKey(it)
		r := v.rows[k]
		if r == nil || !r.built {
			continue
		}
		o := was[k]
		if o == nil || !reflect.DeepEqual(o, it) || old.Convs[it.Channel] != page.Convs[it.Channel] ||
			actWhen(now, r.d.at, false) != r.d.when || actWhen(now, r.d.at, true) != r.d.whenNarrow {
			r.built = false
		}
	}
}
