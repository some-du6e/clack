package i18n

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Slack formats dates with dayjs (client-boot Ub4j) in the locale's dayjs
// locale: dayjs's own (vendors: de YhBJ, en-gb 9xm0, en njAx, es 4HKe, fr
// 5bfw, it 5Mw5, ja WZkP, ko 8gvz, pt-br 1+rt, zh-cn pHGr, zh-tw UIeI) with
// Slack's overrides on top (de-DE ygkG, en-GB +fZ8, en-US XTdU, es-ES
// PP8D/RHSz, es-LA Cu/i, fr-FR PUFr, it-IT H+Q/, ja-JP DIhm, ko-KR Cxbz,
// pt-BR dzVM, zh-CN awR0, zh-TW CiHg), Slack's tables in Azem (Sq, VQ, nI)
// and the time formats of toTime (xiXI). Only the parts Slack's date
// helpers read are here.

type dayjsLocale struct {
	weekdays, weekdaysShort [7]string
	months, monthsShort     [12]string
	lt, lts                 string // formats LT, LTS
	l, ll                   string // formats L, LL
	ordinal                 func(n int, unit byte) string
	meridiem                func(h, m int, lower bool) string
	rel                     [3]string // the first word of calendar() for yesterday, today, tomorrow (bRXt)
	// Azem
	sqFormat         string // Sq: "%1, %2"
	sqShortenDay     bool
	vqFormat         string // VQ: the day divider "%1, %2"
	vqCapitalize     bool
	calendar         func(firstOfMonth, noComma bool) calendarFormats // nI
	time             timeFormats                                      // xiXI
	default12h       bool                                             // wrON: !do24hrTime by default
	relFormatPretty  string                                           // Nb relativeFormatPretty
	relFormatPrettyS string                                           // zh: with a space
}

type calendarFormats struct {
	MMMD, MMMDo, MMMMD, MMMMDo, MMMDYYYY, MMMYYYY, MMMMYYYY, MMMDoYYYY, MMMMDoYYYY string
}

type timeVariant struct {
	def, seconds, hourOnly string
	omit0                  bool
}

type timeFormats struct {
	h24             timeVariant
	h12             *timeVariant
	h12WithMeridiem *timeVariant
}

func split7(s string) (out [7]string)   { copy(out[:], strings.Split(s, "_")); return }
func split12(s string) (out [12]string) { copy(out[:], strings.Split(s, "_")); return }

func enOrdinal(n int, _ byte) string {
	suf := [...]string{"th", "st", "nd", "rd"}
	g := n % 100
	s := ""
	if i := (g - 20) % 10; i >= 1 && i <= 3 {
		s = suf[i]
	} else if g >= 1 && g <= 3 {
		s = suf[g]
	} else {
		s = suf[0]
	}
	return "[" + strconv.Itoa(n) + s + "]"
}

func suffixOrdinal(suf string) func(int, byte) string {
	return func(n int, _ byte) string { return strconv.Itoa(n) + suf }
}

func zhOrdinal(week string) func(int, byte) string {
	return func(n int, unit byte) string {
		switch unit {
		case 'M':
			return strconv.Itoa(n) + " 月"
		case 'w', 'W':
			return strconv.Itoa(n) + week
		}
		return strconv.Itoa(n) + " 日"
	}
}

func ampm(h, _ int, lower bool) string {
	if lower {
		if h < 12 {
			return "am"
		}
		return "pm"
	}
	if h < 12 {
		return "AM"
	}
	return "PM"
}

func zhMeridiem(h, m int, _ bool) string {
	switch n := 100*h + m; {
	case n < 600:
		return "凌晨"
	case n < 900:
		return "早上"
	case n < 1130:
		return "上午"
	case n < 1230:
		return "中午"
	case n < 1800:
		return "下午"
	}
	return "晚上"
}

var (
	enWeekdays      = split7("Sunday_Monday_Tuesday_Wednesday_Thursday_Friday_Saturday")
	enWeekdaysShort = split7("Sun_Mon_Tue_Wed_Thu_Fri_Sat")
	enMonths        = split12("January_February_March_April_May_June_July_August_September_October_November_December")
	enMonthsShort   = split12("Jan_Feb_Mar_Apr_May_Jun_Jul_Aug_Sep_Oct_Nov_Dec")
	zhWeekdays      = split7("星期日_星期一_星期二_星期三_星期四_星期五_星期六")
	zhMonths        = split12("一月_二月_三月_四月_五月_六月_七月_八月_九月_十月_十一月_十二月")
	cjkMonthsShort  = split12("1月_2月_3月_4月_5月_6月_7月_8月_9月_10月_11月_12月")
)

func plainCalendar(c calendarFormats) func(bool, bool) calendarFormats {
	return func(bool, bool) calendarFormats { return c }
}

var zhCalendar = plainCalendar(calendarFormats{"Mo Do", "Mo Do", "Mo Do", "Mo Do", "YYYY [年] Mo Do", "YYYY [年] Mo", "YYYY [年] Mo", "YYYY [年] Mo Do", "YYYY [年] Mo Do"})

var h12Plain = &timeVariant{def: "h:mm", seconds: "h:mm:ss"}

var dayjsLocales = map[string]*dayjsLocale{
	"en-US": {
		weekdays: enWeekdays, weekdaysShort: enWeekdaysShort, months: enMonths, monthsShort: enMonthsShort,
		lt: "h:mm A", lts: "h:mm:ss A", l: "MM/DD/YYYY", ll: "MMMM D, YYYY",
		ordinal: enOrdinal, meridiem: ampm, rel: [3]string{"Yesterday", "Today", "Tomorrow"},
		sqFormat: "%1, %2", vqFormat: "%1, %2",
		calendar: func(_, noComma bool) calendarFormats {
			c := calendarFormats{"MMM D", "MMM Do", "MMMM D", "MMMM Do", "MMM D, YYYY", "MMM YYYY", "MMMM YYYY", "MMM Do, YYYY", "MMMM Do, YYYY"}
			if noComma {
				c.MMMDYYYY, c.MMMDoYYYY, c.MMMMDoYYYY = "MMM D YYYY", "MMM Do YYYY", "MMMM Do YYYY"
			}
			return c
		},
		time:       timeFormats{h24: timeVariant{def: "HH:mm", seconds: "HH:mm:ss"}, h12: h12Plain, h12WithMeridiem: &timeVariant{def: "LT", seconds: "LTS"}},
		default12h: true, relFormatPretty: "%1 at %2",
	},
	"en-GB": {
		weekdays: enWeekdays, weekdaysShort: enWeekdaysShort, months: enMonths, monthsShort: enMonthsShort,
		lt: "HH:mm", lts: "HH:mm:ss", l: "DD/MM/YYYY", ll: "D MMMM YYYY",
		ordinal: enOrdinal, meridiem: ampm, rel: [3]string{"Yesterday", "Today", "Tomorrow"},
		sqFormat: "%1, %2", vqFormat: "%1, %2",
		calendar:   plainCalendar(calendarFormats{"D MMM", "D MMM", "D MMMM", "D MMMM", "D MMM YYYY", "MMM YYYY", "MMMM YYYY", "D MMM YYYY", "D MMMM YYYY"}),
		time:       timeFormats{h24: timeVariant{def: "HH:mm", seconds: "HH:mm:ss"}, h12: h12Plain, h12WithMeridiem: &timeVariant{def: "h:mm A", seconds: "h:mm:ss A"}},
		default12h: true, relFormatPretty: "%1 at %2",
	},
	"de-DE": {
		weekdays: split7("Sonntag_Montag_Dienstag_Mittwoch_Donnerstag_Freitag_Samstag"), weekdaysShort: split7("So._Mo._Di._Mi._Do._Fr._Sa."),
		months: split12("Januar_Februar_März_April_Mai_Juni_Juli_August_September_Oktober_November_Dezember"), monthsShort: split12("Jan._Feb._März_Apr._Mai_Juni_Juli_Aug._Sept._Okt._Nov._Dez."),
		lt: "HH:mm [Uhr]", lts: "HH:mm:ss [Uhr]", l: "DD.MM.YYYY", ll: "D. MMMM YYYY",
		ordinal: suffixOrdinal("."), meridiem: ampm, rel: [3]string{"gestern", "heute", "morgen"},
		sqFormat: "%1, %2", vqFormat: "%1, %2",
		calendar:        plainCalendar(calendarFormats{"Do MMM", "Do MMM", "D MMMM", "Do MMMM", "Do MMM YYYY", "MMM YYYY", "MMMM YYYY", "Do MMM YYYY", "Do MMMM YYYY"}),
		time:            timeFormats{h24: timeVariant{def: "LT", seconds: "LTS"}},
		relFormatPretty: "%1 um %2",
	},
	"es-ES": {
		weekdays: split7("domingo_lunes_martes_miércoles_jueves_viernes_sábado"), weekdaysShort: split7("dom._lun._mar._mié._jue._vie._sáb."),
		months: split12("enero_febrero_marzo_abril_mayo_junio_julio_agosto_septiembre_octubre_noviembre_diciembre"), monthsShort: split12("ene._feb._mar._abr._may._jun._jul._ago._sep._oct._nov._dic."),
		lt: "HH:mm", lts: "HH:mm:ss", l: "DD/MM/YYYY", ll: "D [de] MMMM [de] YYYY",
		ordinal: suffixOrdinal("º"), meridiem: ampm, rel: [3]string{"ayer", "hoy", "mañana"},
		sqFormat: "%1, %2", vqFormat: "%1, %2", vqCapitalize: true,
		calendar: func(first, _ bool) calendarFormats {
			c := calendarFormats{"D MMM", "D MMM", "D MMMM", "D [de] MMMM", "D MMM YYYY", "MMM YYYY", "MMMM [de] YYYY", "D MMM YYYY", "D [de] MMMM [de] YYYY"}
			if first {
				c.MMMMDo = "[uno de] MMMM"
			}
			return c
		},
		time:            timeFormats{h24: timeVariant{def: "LT", seconds: "LTS"}},
		relFormatPretty: "%1 a las %2",
	},
	"es-LA": {
		weekdays: split7("domingo_lunes_martes_miércoles_jueves_viernes_sábado"), weekdaysShort: split7("dom._lun._mar._mié._jue._vie._sáb."),
		months: split12("enero_febrero_marzo_abril_mayo_junio_julio_agosto_septiembre_octubre_noviembre_diciembre"), monthsShort: split12("ene._feb._mar._abr._may._jun._jul._ago._sep._oct._nov._dic."),
		lt: "H:mm", lts: "H:mm:ss", l: "DD/MM/YYYY", ll: "D [de] MMMM [de] YYYY",
		ordinal: suffixOrdinal("º"), meridiem: ampm, rel: [3]string{"ayer", "hoy", "mañana"},
		sqFormat: "%1, %2", vqFormat: "%1, %2",
		calendar:        plainCalendar(calendarFormats{"D [de] MMM", "D [de] MMM", "D [de] MMMM", "D [de] MMMM", "D [de] MMM [de] YYYY", "MMM YYYY", "MMMM [de] YYYY", "D [de] MMM [de] YYYY", "D [de] MMMM [de] YYYY"}),
		time:            timeFormats{h24: timeVariant{def: "LT", seconds: "LTS"}},
		relFormatPretty: "%1 a la(s) %2",
	},
	"fr-FR": {
		weekdays: split7("dimanche_lundi_mardi_mercredi_jeudi_vendredi_samedi"), weekdaysShort: split7("dim._lun._mar._mer._jeu._ven._sam."),
		months: split12("janvier_février_mars_avril_mai_juin_juillet_août_septembre_octobre_novembre_décembre"), monthsShort: split12("janv._févr._mars_avr._mai_juin_juil._août_sept._oct._nov._déc."),
		lt: "H [h] mm", lts: "H [h] mm [min] ss [s]", l: "DD/MM/YYYY", ll: "D MMMM YYYY",
		ordinal: func(n int, _ byte) string {
			if n == 1 {
				return "1er"
			}
			return strconv.Itoa(n)
		},
		meridiem: ampm, rel: [3]string{"Hier", "Aujourd’hui", "Demain"},
		sqFormat: "%1, %2", vqFormat: "%1 %2", vqCapitalize: true,
		calendar:        plainCalendar(calendarFormats{"D MMM", "Do MMM", "D MMMM", "Do MMMM", "D MMM YYYY", "MMM YYYY", "MMMM YYYY", "Do MMM YYYY", "Do MMMM YYYY"}),
		time:            timeFormats{h24: timeVariant{def: "LT", seconds: "LTS", hourOnly: "H [h]", omit0: true}},
		relFormatPretty: "%1 à %2",
	},
	"it-IT": {
		weekdays: split7("domenica_lunedì_martedì_mercoledì_giovedì_venerdì_sabato"), weekdaysShort: split7("dom_lun_mar_mer_gio_ven_sab"),
		months: split12("gennaio_febbraio_marzo_aprile_maggio_giugno_luglio_agosto_settembre_ottobre_novembre_dicembre"), monthsShort: split12("gen_feb_mar_apr_mag_giu_lug_ago_set_ott_nov_dic"),
		lt: "H:mm", lts: "H:mm:ss", l: "DD/MM/YYYY", ll: "D MMMM YYYY",
		ordinal: suffixOrdinal("º"), meridiem: ampm, rel: [3]string{"Ieri", "Oggi", "Domani"},
		sqFormat: "%1 %2", vqFormat: "%1 %2", vqCapitalize: true,
		calendar: func(first, _ bool) calendarFormats {
			if first {
				return calendarFormats{"Do MMM", "Do MMM", "Do MMMM", "Do MMMM", "Do MMM YYYY", "MMM YYYY", "MMMM YYYY", "Do MMM YYYY", "Do MMMM YYYY"}
			}
			return calendarFormats{"D MMM", "D MMM", "D MMMM", "D MMMM", "D MMM YYYY", "MMM YYYY", "MMMM YYYY", "D MMM YYYY", "D MMMM YYYY"}
		},
		time:            timeFormats{h24: timeVariant{def: "LT", seconds: "LTS"}},
		relFormatPretty: "%1 alle %2",
	},
	"ja-JP": {
		weekdays: split7("日曜日_月曜日_火曜日_水曜日_木曜日_金曜日_土曜日"), weekdaysShort: split7("日_月_火_水_木_金_土"),
		months: cjkMonthsShort, monthsShort: cjkMonthsShort,
		lt: "HH:mm", lts: "HH:mm:ss", l: "YYYY/MM/DD", ll: "YYYY年M月D日",
		ordinal: suffixOrdinal("日"),
		meridiem: func(h, _ int, _ bool) string {
			if h < 12 {
				return "午前"
			}
			return "午後"
		},
		rel:      [3]string{"昨日", "今日", "明日"},
		sqFormat: "%2 (%1)", sqShortenDay: true, vqFormat: "%2 (%1)",
		calendar:        plainCalendar(calendarFormats{"MMMD[日]", "MMMDo", "MMMMD", "MMMMDo", "YYYY[年]MMMDo", "YYYY[年]MMM", "YYYY[年]MMMM", "YYYY[年]MMMMDo", "YYYY[年]MMMMDo"}),
		time:            timeFormats{h24: timeVariant{def: "LT", seconds: "LTS"}},
		relFormatPretty: "%1の%2",
	},
	"ko-KR": {
		weekdays: split7("일요일_월요일_화요일_수요일_목요일_금요일_토요일"), weekdaysShort: split7("일_월_화_수_목_금_토"),
		months: split12("1월_2월_3월_4월_5월_6월_7월_8월_9월_10월_11월_12월"), monthsShort: split12("1월_2월_3월_4월_5월_6월_7월_8월_9월_10월_11월_12월"),
		lt: "A h:mm", lts: "A h:mm:ss", l: "YYYY/MM/DD", ll: "YYYY년 MMMM D일",
		ordinal: suffixOrdinal("일"),
		meridiem: func(h, _ int, _ bool) string {
			if h < 12 {
				return "오전"
			}
			return "오후"
		},
		rel:      [3]string{"어제", "오늘", "내일"},
		sqFormat: "%2 %1", vqFormat: "%2 %1",
		calendar:   plainCalendar(calendarFormats{"MMM D[일]", "MMM Do", "MMMM D", "MMMM Do", "YYYY[년] MMM Do", "YYYY[년] MMM", "YYYY[년] MMMM", "YYYY[년] MMMM Do", "YYYY[년] MMMM Do"}),
		time:       timeFormats{h24: timeVariant{def: "HH:mm", seconds: "HH:mm:ss"}, h12: h12Plain, h12WithMeridiem: &timeVariant{def: "LT", seconds: "LTS"}},
		default12h: true, relFormatPretty: "%1 %2",
	},
	"pt-BR": {
		weekdays: split7("Domingo_Segunda-feira_Terça-feira_Quarta-feira_Quinta-feira_Sexta-feira_Sábado"), weekdaysShort: split7("Dom_Seg_Ter_Qua_Qui_Sex_Sáb"),
		months: split12("janeiro_fevereiro_março_abril_maio_junho_julho_agosto_setembro_outubro_novembro_dezembro"), monthsShort: split12("jan_fev_mar_abr_mai_jun_jul_ago_set_out_nov_dez"),
		lt: "H[h]mm", lts: "H[h]mm[min]ss[s]", l: "DD/MM/YYYY", ll: "D [de] MMMM [de] YYYY",
		ordinal: suffixOrdinal("º"), meridiem: ampm, rel: [3]string{"Ontem", "Hoje", "Amanhã"},
		sqFormat: "%1, %2", vqFormat: "%1, %2", vqCapitalize: true,
		calendar: func(first, _ bool) calendarFormats {
			if first {
				return calendarFormats{"Do MMM", "Do MMM", "Do MMMM", "Do [de] MMMM", "Do MMM, YYYY", "MMM YYYY", "MMMM [de] YYYY", "Do MMM, YYYY", "Do [de] MMMM [de] YYYY"}
			}
			return calendarFormats{"D MMM", "D MMM", "D MMMM", "D [de] MMMM", "D MMM, YYYY", "MMM YYYY", "MMMM [de] YYYY", "D MMM, YYYY", "D [de] MMMM [de] YYYY"}
		},
		time:            timeFormats{h24: timeVariant{def: "LT", seconds: "LTS", hourOnly: "H[h]", omit0: true}},
		relFormatPretty: "%1 às %2",
	},
	"zh-CN": {
		weekdays: zhWeekdays, weekdaysShort: split7("周日_周一_周二_周三_周四_周五_周六"),
		months: zhMonths, monthsShort: cjkMonthsShort,
		lt: "A h:mm", lts: "A h:mm:ss", l: "YYYY/MM/DD", ll: "YYYY年M月D日",
		ordinal: zhOrdinal("周"), meridiem: zhMeridiem, rel: [3]string{"昨天", "今天", "明天"},
		sqFormat: "%2%1", vqFormat: "%2%1",
		calendar:   zhCalendar,
		time:       timeFormats{h24: timeVariant{def: "HH:mm", seconds: "HH:mm:ss"}, h12: h12Plain, h12WithMeridiem: &timeVariant{def: "LT", seconds: "LTS"}},
		default12h: true, relFormatPretty: "%1%2", relFormatPrettyS: "%1 %2",
	},
	"zh-TW": {
		weekdays: zhWeekdays, weekdaysShort: split7("週日_週一_週二_週三_週四_週五_週六"),
		months: zhMonths, monthsShort: cjkMonthsShort,
		lt: "A h:mm", lts: "A h:mm:ss", l: "YYYY/MM/DD", ll: "YYYY年M月D日",
		ordinal: zhOrdinal("週"), meridiem: zhMeridiem, rel: [3]string{"昨天", "今天", "明天"},
		sqFormat: "%2%1", sqShortenDay: true, vqFormat: "%2%1",
		calendar:   zhCalendar,
		time:       timeFormats{h24: timeVariant{def: "HH:mm", seconds: "HH:mm:ss"}, h12: h12Plain, h12WithMeridiem: &timeVariant{def: "LT", seconds: "LTS"}},
		default12h: true, relFormatPretty: "%1%2", relFormatPrettyS: "%1 %2",
	},
}

func localeData(loc string) *dayjsLocale {
	if d := dayjsLocales[loc]; d != nil {
		return d
	}
	return dayjsLocales[EnUS]
}

// FormatDayjs formats t with a dayjs format string in loc (Slack's dayjs
// with advancedFormat, localizedFormat and its "Mo" extension).
func FormatDayjs(loc string, t time.Time, f string) string {
	d := localeData(loc)
	// localizedFormat and advancedFormat rewrite the format first; their
	// output is format text again ([…] literals survive).
	f = expandTokens(f, func(tok string) (string, bool) {
		switch tok {
		case "LTS":
			return d.lts, true
		case "LT":
			return d.lt, true
		case "LL":
			return d.ll, true
		case "L":
			return d.l, true
		}
		return "", false
	})
	f = expandTokens(f, func(tok string) (string, bool) {
		switch tok {
		case "Do":
			return d.ordinal(t.Day(), 'D'), true
		case "Mo":
			return d.ordinal(int(t.Month()), 'M'), true
		}
		return "", false
	})
	var b strings.Builder
	for i := 0; i < len(f); {
		if f[i] == '[' {
			if j := strings.IndexByte(f[i:], ']'); j > 0 {
				b.WriteString(f[i+1 : i+j])
				i += j + 1
				continue
			}
		}
		tok := matchToken(f[i:])
		if tok == "" {
			_, n := utf8.DecodeRuneInString(f[i:])
			b.WriteString(f[i : i+n])
			i += n
			continue
		}
		b.WriteString(formatToken(d, t, tok))
		i += len(tok)
	}
	return b.String()
}

var tokens = []string{"YYYY", "MMMM", "dddd", "MMM", "ddd", "LTS", "YY", "MM", "DD", "Do", "Mo", "dd", "HH", "hh", "mm", "ss", "LL", "LT", "M", "D", "d", "H", "h", "m", "s", "A", "a", "L"}

func matchToken(s string) string {
	for _, t := range tokens {
		if strings.HasPrefix(s, t) {
			return t
		}
	}
	return ""
}

// expandTokens replaces the tokens repl knows outside [...] literals.
func expandTokens(f string, repl func(string) (string, bool)) string {
	if !strings.ContainsAny(f, "LDM") {
		return f
	}
	var b strings.Builder
	for i := 0; i < len(f); {
		if f[i] == '[' {
			if j := strings.IndexByte(f[i:], ']'); j > 0 {
				b.WriteString(f[i : i+j+1])
				i += j + 1
				continue
			}
		}
		if tok := matchToken(f[i:]); tok != "" {
			if r, ok := repl(tok); ok {
				b.WriteString(r)
			} else {
				b.WriteString(tok)
			}
			i += len(tok)
			continue
		}
		_, n := utf8.DecodeRuneInString(f[i:])
		b.WriteString(f[i : i+n])
		i += n
	}
	return b.String()
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func formatToken(d *dayjsLocale, t time.Time, tok string) string {
	h := t.Hour()
	h12 := h % 12
	if h12 == 0 {
		h12 = 12
	}
	switch tok {
	case "YYYY":
		return strconv.Itoa(t.Year())
	case "YY":
		return pad2(t.Year() % 100)
	case "MMMM":
		return d.months[t.Month()-1]
	case "MMM":
		return d.monthsShort[t.Month()-1]
	case "MM":
		return pad2(int(t.Month()))
	case "M":
		return strconv.Itoa(int(t.Month()))
	case "DD":
		return pad2(t.Day())
	case "D":
		return strconv.Itoa(t.Day())
	case "dddd":
		return d.weekdays[t.Weekday()]
	case "ddd", "dd":
		return d.weekdaysShort[t.Weekday()]
	case "d":
		return strconv.Itoa(int(t.Weekday()))
	case "HH":
		return pad2(h)
	case "H":
		return strconv.Itoa(h)
	case "hh":
		return pad2(h12)
	case "h":
		return strconv.Itoa(h12)
	case "mm":
		return pad2(t.Minute())
	case "m":
		return strconv.Itoa(t.Minute())
	case "ss":
		return pad2(t.Second())
	case "s":
		return strconv.Itoa(t.Second())
	case "A":
		return d.meridiem(h, t.Minute(), false)
	case "a":
		return d.meridiem(h, t.Minute(), true)
	}
	return tok
}

// Default24h is toTime's default: 24-hour clock unless the locale is one
// of en-US, en-GB, ko-KR, zh-CN, zh-TW (wrON).
func Default24h(loc string) bool { return !localeData(loc).default12h }

// Time is toTime (xiXI): "3:04 PM", "15:04 Uhr", "15 h 04", "下午 3:04".
// Locales without a 12-hour format (de, es, fr, it, ja, pt) always use
// 24 hours.
func Time(loc string, t time.Time, do24, seconds bool) string {
	return timeWith(loc, t, do24, !do24, seconds)
}

func timeWith(loc string, t time.Time, do24, meridiem, seconds bool) string {
	d := localeData(loc)
	w := &d.time.h24
	if !do24 && d.time.h12 != nil {
		w = d.time.h12
		if meridiem && d.time.h12WithMeridiem != nil {
			w = d.time.h12WithMeridiem
		}
	}
	f := w.def
	switch {
	case w.omit0 && seconds:
		if t.Second() == 0 {
			if t.Minute() == 0 && w.hourOnly != "" {
				f = w.hourOnly
			}
		} else {
			f = w.seconds
		}
	case w.omit0 || !seconds:
		if w.hourOnly != "" && t.Minute() == 0 {
			f = w.hourOnly
		}
	default:
		f = w.seconds
	}
	return FormatDayjs(loc, t, f)
}

// ShortTime is toTime without the meridiem ("3:04"): the hover time of
// grouped rows.
func ShortTime(loc string, t time.Time, do24 bool) string {
	return timeWith(loc, t, do24, false, false)
}

// Weekday is toDayOfTheWeek (Dlvk): dddd, or ddd when short.
func Weekday(loc string, t time.Time, short bool) string {
	if short {
		return FormatDayjs(loc, t, "ddd")
	}
	return FormatDayjs(loc, t, "dddd")
}

// CalendarOpts are toCalendarDate's options (2Ie3).
type CalendarOpts struct {
	ShortenMonth, ShortenDay, ExcludeYear, ExcludeDay, ExcludeComma bool
}

// CalendarDate is toCalendarDate: "November 3rd, 2025", "3 de noviembre
// de 2025", "2025年11月3日".
func CalendarDate(loc string, t time.Time, o CalendarOpts) string {
	c := localeData(loc).calendar(t.Day() == 1, o.ExcludeComma)
	var f string
	switch {
	case o.ShortenMonth && o.ShortenDay && o.ExcludeYear:
		f = c.MMMD
	case o.ShortenMonth && o.ShortenDay:
		f = c.MMMDYYYY
	case !o.ShortenMonth && o.ShortenDay && o.ExcludeYear:
		f = c.MMMMD
	case o.ShortenMonth && o.ExcludeYear:
		f = c.MMMDo
	case o.ShortenMonth && o.ExcludeDay:
		f = c.MMMYYYY
	case o.ShortenMonth:
		f = c.MMMDoYYYY
	case o.ExcludeYear:
		f = c.MMMMDo
	case o.ExcludeDay:
		f = c.MMMMYYYY
	default:
		f = c.MMMMDoYYYY
	}
	return FormatDayjs(loc, t, f)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// RelativeDay is maybeGetRelativeDay (bRXt): the first word of dayjs's
// calendar() for yesterday, today and tomorrow ("Yesterday", "Hoy",
// "今日"), capitalized, or all lower case; "" for other days.
func RelativeDay(loc string, t, now time.Time, lower bool) string {
	d := localeData(loc)
	var s string
	switch {
	case sameDay(t, now):
		s = d.rel[1]
	case sameDay(t, now.AddDate(0, 0, -1)):
		s = d.rel[0]
	case sameDay(t, now.AddDate(0, 0, 1)):
		s = d.rel[2]
	default:
		return ""
	}
	if lower {
		return strings.ToLower(s)
	}
	return capitalize(s)
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// DayDivider is formatDateDayDivider (YvgU), the date between days in a
// conversation: another year's "November 3rd, 2025"; "Today",
// "Yesterday"; else "Monday, November 3rd" ("Lunes, 3 de noviembre",
// "11月3日 (月)").
func DayDivider(loc string, t, now time.Time, lower bool) string {
	if t.Year() != now.Year() {
		return CalendarDate(loc, t, CalendarOpts{})
	}
	if r := RelativeDay(loc, t, now, lower); r != "" {
		return r
	}
	d := localeData(loc)
	day := Weekday(loc, t, d.sqShortenDay)
	if d.vqCapitalize {
		day = capitalize(day)
	}
	return strings.NewReplacer("%1", day, "%2", CalendarDate(loc, t, CalendarOpts{ExcludeYear: true})).Replace(d.vqFormat)
}

// LongDate is formatDate's {date_long}: Sq's "%1, %2" of the weekday and
// the month and day, with the year outside ±182 days ("Monday, November
// 3rd"; "Monday, November 3rd, 2025").
func LongDate(loc string, t, now time.Time) string {
	d := localeData(loc)
	excl := math.Abs(now.Sub(t).Minutes()) < 262080
	return strings.NewReplacer("%1", Weekday(loc, t, d.sqShortenDay), "%2",
		CalendarDate(loc, t, CalendarOpts{ExcludeYear: excl})).Replace(d.sqFormat)
}

// LongDatePretty is formatDate's {date_long_pretty}: "Today",
// "Yesterday", else Sq's "%1, %2" of the weekday and the month and day
// (with the year outside ±182 days).
func LongDatePretty(loc string, t, now time.Time, lower bool) string {
	if r := RelativeDay(loc, t, now, lower); r != "" {
		return r
	}
	d := localeData(loc)
	excl := math.Abs(now.Sub(t).Minutes()) < 262080
	return strings.NewReplacer("%1", Weekday(loc, t, d.sqShortenDay), "%2",
		CalendarDate(loc, t, CalendarOpts{ExcludeYear: excl})).Replace(d.sqFormat)
}

// TimeAgo is toTimeAgo (zZAT): "< 1 minute ago" … "N years ago",
// "in 3 hours" for a future time unless alwaysAgo; compact gives "3h".
func TimeAgo(loc string, t, now time.Time, compact, alwaysAgo bool) string {
	tab, ok := timeAgoTables[loc]
	if !ok {
		tab = timeAgoTables[EnUS]
	}
	secs := now.Sub(t).Seconds()
	future := !alwaysAgo && secs < 0
	h := math.Abs(secs)
	mins := h / 60
	hrs := mins / 60
	days := hrs / 24
	months := days / (365.0 / 12)
	years := days / 365
	row := tab[0]
	if future {
		row = tab[1]
	}
	if compact {
		row = tab[3]
	}
	n := func(s string, v float64) string {
		return strings.Replace(s, "%d", strconv.Itoa(int(math.Floor(v+0.5))), 1)
	}
	switch {
	case h < 45:
		return row[0]
	case h < 90:
		return row[1]
	case mins < 45:
		return n(row[2], mins)
	case mins < 90:
		return row[3]
	case hrs < 24:
		return n(row[4], hrs)
	case days < 2:
		return row[5]
	case days < 30:
		return n(row[6], days)
	case days < 365:
		if compact && !future {
			return CalendarDate(loc, t, CalendarOpts{ShortenDay: true, ShortenMonth: true, ExcludeYear: true})
		}
		if months < 1.5 {
			return n(tab[map[bool]int{false: 0, true: 1}[future]][7], months)
		}
		return n(tab[map[bool]int{false: 0, true: 1}[future]][8], months)
	case years < 1.5:
		return row[9]
	}
	return n(row[10], years)
}

// intlDate is {d, date|time, style} (Intl.DateTimeFormat). Slack's
// strings use only {…, date, long}: dayjs's LL is the same text.
func intlDate(loc string, t time.Time, timeOnly bool, style string) string {
	if timeOnly {
		return Time(loc, t, Default24h(loc), false)
	}
	switch style {
	case "short":
		return FormatDayjs(loc, t, "L")
	}
	return FormatDayjs(loc, t, "LL")
}
