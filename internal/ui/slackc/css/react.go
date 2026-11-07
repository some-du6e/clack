package css

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// React's unitless properties (react-dom isUnitlessNumber), without
// vendor prefixes: numbers stay bare for these and get "px" otherwise.
var unitless = map[string]bool{
	"animationIterationCount": true, "aspectRatio": true, "borderImageOutset": true, "borderImageSlice": true,
	"borderImageWidth": true, "boxFlex": true, "boxFlexGroup": true, "boxOrdinalGroup": true, "columnCount": true,
	"columns": true, "flex": true, "flexGrow": true, "flexPositive": true, "flexShrink": true, "flexNegative": true,
	"flexOrder": true, "gridArea": true, "gridRow": true, "gridRowEnd": true, "gridRowSpan": true, "gridRowStart": true,
	"gridColumn": true, "gridColumnEnd": true, "gridColumnSpan": true, "gridColumnStart": true, "fontWeight": true,
	"lineClamp": true, "lineHeight": true, "opacity": true, "order": true, "orphans": true, "scale": true,
	"tabSize": true, "widows": true, "zIndex": true, "zoom": true, "fillOpacity": true, "floodOpacity": true,
	"stopOpacity": true, "strokeDasharray": true, "strokeDashoffset": true, "strokeMiterlimit": true,
	"strokeOpacity": true, "strokeWidth": true,
}

// reactProp converts a React style key to a CSS property name and
// reports whether numbers stay unitless.
func reactProp(k string) (string, bool) {
	if strings.HasPrefix(k, "--") {
		return k, false
	}
	base := k
	prefix := ""
	switch {
	case strings.HasPrefix(k, "Webkit"):
		prefix, base = "-webkit-", k[6:]
	case strings.HasPrefix(k, "Moz"):
		prefix, base = "-moz-", k[3:]
	case strings.HasPrefix(k, "ms") && len(k) > 2 && k[2] >= 'A' && k[2] <= 'Z':
		prefix, base = "-ms-", k[2:]
	case strings.HasPrefix(k, "O") && len(k) > 1 && k[1] >= 'A' && k[1] <= 'Z':
		prefix, base = "-o-", k[1:]
	}
	if base == "" {
		return "", false
	}
	lowerFirst := strings.ToLower(base[:1]) + base[1:]
	var b strings.Builder
	b.WriteString(prefix)
	for i := 0; i < len(lowerFirst); i++ {
		c := lowerFirst[i]
		if c >= 'A' && c <= 'Z' {
			b.WriteByte('-')
			b.WriteByte(c + 32)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String(), unitless[lowerFirst]
}

func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// FromReactStyle converts a React style object into declarations:
// camelCase keys become kebab-case (vendor prefixes too), numbers get
// "px" except React's unitless properties, null/false/"" are dropped and
// "--custom" keys are kept as they are. Shorthands are applied before
// longhands (a JS object's key order isn't known here).
func FromReactStyle(m map[string]any) Decls {
	if len(m) == 0 {
		return nil
	}
	type kv struct {
		prop  string
		val   string
		short bool
		key   string
	}
	items := make([]kv, 0, len(m))
	for k, v := range m {
		prop, unitlessNum := reactProp(k)
		if prop == "" {
			continue
		}
		var s string
		num := func(f float64) string {
			if f == 0 || unitlessNum || strings.HasPrefix(prop, "--") {
				return fmtNum(f)
			}
			return fmtNum(f) + "px"
		}
		switch x := v.(type) {
		case nil:
			continue
		case bool:
			continue
		case string:
			s = strings.TrimSpace(x)
		case float64:
			s = num(x)
		case float32:
			s = num(float64(x))
		case int:
			s = num(float64(x))
		case int64:
			s = num(float64(x))
		case int32:
			s = num(float64(x))
		case uint64:
			s = num(float64(x))
		case json.Number:
			f, err := x.Float64()
			if err != nil {
				continue
			}
			s = num(f)
		default:
			continue
		}
		if s == "" {
			continue
		}
		// React assigns style[prop] = value: "!important" makes it invalid
		if !strings.HasPrefix(prop, "--") && strings.Contains(strings.ToLower(s), "!important") {
			continue
		}
		name := prop
		if a, ok := propAlias[name]; ok {
			name = a
		}
		_, short := shorthands[name]
		items = append(items, kv{prop: prop, val: s, short: short || name == "all", key: k})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].short != items[j].short {
			return items[i].short
		}
		return items[i].key < items[j].key
	})
	var out Decls
	for _, it := range items {
		out = expandDecl(it.prop, it.val, false, out)
	}
	return out
}
