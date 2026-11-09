package acrank

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Weights is a LinearModel's weights (94aK), by feature name.
type Weights map[string]float64

// weight is LinearModel._weightForFeature: the feature's weight plus its
// READ_ (or, for the composer page destination and share sources,
// WRITE_) variant; USER_IS_EXTERNAL always carries an extra -1.
func (w Weights) weight(name, source string) float64 {
	v := w[name]
	if name == "USER_IS_EXTERNAL" {
		v--
	}
	pre := "READ_"
	if source == "composer_page__destination" || strings.HasPrefix(source, "share") {
		pre = "WRITE_"
	}
	return v + w[pre+name]
}

// ParseModel reads search.autocomplete.model's answer ({"model":
// {name: weight}}) the way createLinearModel does: names uppercased,
// values through Number(). ok is false when there is no model (Slack
// keeps the bundled one then).
func ParseModel(raw []byte) (Weights, bool) {
	var r struct {
		Model map[string]json.RawMessage `json:"model"`
	}
	if json.Unmarshal(raw, &r) != nil || len(r.Model) == 0 {
		return nil, false
	}
	w := make(Weights, len(r.Model))
	for k, v := range r.Model {
		s := strings.Trim(string(v), "\" ")
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			continue
		}
		w[strings.ToUpper(k)] = f
	}
	return w, len(w) > 0
}

// FallbackWeights is the bundled autocompleteFallback model (X46X).
var FallbackWeights = Weights{
	"ACTIVE_TOKEN_FILTER_AFTER":            -1.3342286966503,
	"ACTIVE_TOKEN_FILTER_BEFORE":           -1.5840743442206,
	"ACTIVE_TOKEN_FILTER_DURING":           -1.5067377681356,
	"ACTIVE_TOKEN_FILTER_FROM":             -0.32097975590927,
	"ACTIVE_TOKEN_FILTER_HAS":              0.12307720392367,
	"ACTIVE_TOKEN_FILTER_IN":               -0.20437151727999,
	"ACTIVE_TOKEN_FILTER_ON":               -0.2331890332635,
	"ACTIVE_TOKEN_FILTER_TO":               -0.39179992363572,
	"ACTIVE_TOKEN_FILTER_WITH":             -2.6491467875516,
	"ACTIVE_TOKEN_MEMBER":                  3.3152339724625,
	"ACTIVE_TOKEN_PHRASE":                  1.6227243782418,
	"ACTIVE_TOKEN_STRING":                  1.2377431959723,
	"APP_NAME_PREFIX_MATCH":                0.41310546603391,
	"CHANNEL_ACTION_TEAM_CTR":              5.3683432201332,
	"CHANNEL_ACTION_USER_CTR":              -3.0316210543763,
	"CHANNEL_IS_ARCHIVED":                  -0.87858649301233,
	"CHANNEL_IS_MEMBER":                    0.77730174627333,
	"CHANNEL_IS_NOT_MEMBER":                -0.42,
	"CHANNEL_IS_OTHER_TEAM":                -0.61919041941926,
	"CHANNEL_IS_STARRED":                   0.42983549335324,
	"CHANNEL_IS_UNREAD":                    0.32493583671244,
	"CHANNEL_READ_AGE_DECAY_7D":            1.7862684517169,
	"CURRENT_USER_TEXTY":                   -2.0962311707595,
	"EMOJI_COMBO_BREAK_0":                  0.034181103626295,
	"EMOJI_COMBO_BREAK_1":                  -1.5404314842404,
	"EMOJI_COMBO_BREAK_2":                  -2.2796470967609,
	"EMOJI_EXACT_MATCH":                    0.183283071987,
	"EMOJI_QUERY_LENGTH_RATIO":             1.0690358111219,
	"EMOJI_USER_CTR_30DD":                  5.1931307714537,
	"FILE_AGE_DECAY_7D":                    0.094311459141267,
	"FILE_AGE_DECAY_90D":                   -0.068414601197763,
	"FILE_FROM_CURRENT_USER":               -0.053638049001005,
	"FILE_HAS_DMS_WITH_CREATOR":            -0.0061128361931085,
	"FILE_IS_CANVAS":                       0.3,
	"FILE_LATEST_ACTION_AGE_DECAY_90D":     0.22470956185502,
	"FILE_LATEST_VIEW_BY_USER_OLDER_90D":   -1.5,
	"FILE_LOG_ACTIONS":                     0.31755825349717,
	"FILE_LOG_PRIVATE_SHARES":              0.63647050316634,
	"FILE_LOG_PUBLIC_SHARES":               -0.4105759438314,
	"FILE_NAME_CONTAINS_QUERY_NORMALIZED":  1,
	"FILTER_USER_DM_READ_AGE_DECAY_2H":     -0.39943623618561,
	"FILTER_USER_DM_READ_AGE_DECAY_7D":     0.8400841724556,
	"FILTER_USER_IS_ACTIVE":                0.42798763518113,
	"FILTER_USER_PRIORITY":                 0.71416734958181,
	"FRECENCY_BONUS":                       0.0083562726103486,
	"FRECENCY_LOG_HIT_COUNT_SCALED":        1.0221320483578,
	"FRECENCY_LOG_ITEM_COUNT_SCALED":       0.93494415431914,
	"FRECENCY_WEIGHT_HIT_VISITS":           0.010783602348951,
	"FRECENCY_WEIGHT_ITEM_VISITS":          0.019341183576389,
	"FUZZY_SCORE_50":                       0.95744512772969,
	"FUZZY_SCORE_ALL":                      -2.1179514334905,
	"GLOBAL_ACTION_USER_CTR":               6.0311738732755,
	"HERE_KEYWORD_EXACT_MATCH":             2.8669041266612,
	"HISTORY_AGE_DECAY_7D":                 0.66414153833523,
	"IS_BROADCAST_KEYWORD":                 3.4759283393419,
	"MEMBER_OF_CURRENT_CHANNEL":            3.3022467998568,
	"MPIM_MATCHER_SCORE_0":                 -0.02289922354073,
	"MPIM_MATCHER_SCORE_1":                 -1.2162030026989,
	"MPIM_MATCHER_SCORE_2":                 -2.3109629676003,
	"MPIM_MATCHER_SCORE_3":                 -2.545746172366,
	"MPIM_MEMBER_ALL_MATCHES":              0.040175375087294,
	"MPIM_MEMBER_NAME_MATCHES":             0.071667207602181,
	"MPIM_READ_AGE_DECAY_7D":               2.0911081748312,
	"MPIM_USER_CTR_30DD":                   10.134770283044,
	"MPIM_WITH_DEACTIVATED_USER":           -1.1657978882602,
	"NAV_NORMAL_MATCH_CHANNEL_LOG_RESULTS": 0.49306597435127,
	"NAV_NORMAL_MATCH_USER":                0.83230837935037,
	"NAV_NORMAL_PREFIX_MATCH_NO_USER":      0.4852904832645,
	"NAV_NORMAL_PREFIX_MATCH_USER":         -0.12081821144909,
	"ONLY_MATCHES_PREVIOUS_NAME":           -1.248003231081,
	"OS_APP_ACTION":                        -1.381807580371,
	"QUERY_FRECENCY_BONUS":                 0.011725693517502,
	"QUERY_FRECENCY_RAW":                   0.0071433931825349,
	"QUERY_FRECENCY_WEIGHT_HIT_VISITS":     0.0047962209104509,
	"QUERY_TOKEN_NORMAL_PREFIX_MATCH":      0.38593901851901,
	"SLASH_COMMAND_USER_CTR":               3.2204170381454,
	"STRING_EXACT_PREFIX_MATCH":            -0.53245135333594,
	"STRING_NORMAL_PREFIX_MATCH":           -0.4692069451129,
	"TYPE_APP":                             -2.0995766663586,
	"TYPE_APP_ACTION":                      -0.15299090921826,
	"TYPE_CHANNEL_ACTION":                  -0.97398280475431,
	"TYPE_GLOBAL_ACTION":                   0.24313716972704,
	"TYPE_NAVIGATION_CHANNEL":              -0.99949500941624,
	"TYPE_NAVIGATION_FILE":                 -0.60870055140391,
	"TYPE_NAVIGATION_MODAL":                -1.2540608783032,
	"TYPE_NAVIGATION_MPIM":                 -0.13640397661212,
	"TYPE_NAVIGATION_TEAM":                 -0.74505579518249,
	"TYPE_NAVIGATION_USER":                 0.19845685040217,
	"TYPE_NAVIGATION_VIEW":                 -0.06828592658814,
	"TYPE_QUERY":                           -0.34666651828357,
	"TYPE_QUERY_HISTORY":                   -0.89886675989568,
	"TYPE_SLASH_COMMAND":                   -0.18074491193454,
	"TYPE_USER_QUERY":                      1.2730815188112,
	"TYPE_WORKFLOW_TRIGGER":                -1.2028127578536,
	"USER_DM_EMPTY_V2":                     1.4226370781581,
	"USER_DM_LATEST_MSG_AGE_DECAY_7D_V2":   0.40074999941377,
	"USER_DM_LATEST_OLDER_90D_V2":          -1.2419114598606,
	"USER_EMAIL_ONLY_MATCH":                -1.2756812486684,
	"USER_ENTITY_TYPE_CTR_30DD":            3.7407446891213,
	"USER_FIRST_NAME_NORMAL_MATCH":         0.36402719522123,
	"USER_IS_ACTIVE":                       0.58279556397075,
	"USER_IS_DEACTIVATED":                  -0.58279556397075,
	"USER_IS_BOT":                          -0.76450720727465,
	"USER_IS_EXTERNAL":                     0.27225738168756,
	"USER_LAST_NAME_NORMAL_MATCH":          0.19972040917946,
	"USER_MPIM_READ_AGE_DECAY_7D":          0.56565249217787,
	"USER_PRIORITY":                        0.29699660378898,
	"USER_SAME_TEAM":                       0.17696933968657,
	"USER_AFFINITY_SCORE":                  0,
	"CHANNEL_PRIORITY_90D_SCORE":           0,
	"FILE_IS_TOP_ENGAGED":                  0,
}
