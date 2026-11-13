package demo

import (
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/emoji"
)

// The standard emoji without a drawing of their own (stdArt has the
// common ones): drawn from their name and code point instead of one
// neutral face for all, so the picker reads as emoji. Smileys get a
// yellow face whose eyes and mouth follow the name (grin, wink, cry,
// open_mouth, heart_eyes…), hands a palm in their skin tone, hearts and
// shapes their colour, flags a flag, animals a face, food a plate,
// vehicles a vehicle, keycaps their digit, clocks a clock; anything else
// a coloured tile with the emoji's initial.

var (
	genOnce  sync.Once
	genNames map[string]string // image stem -> primary name
)

// stdName is the primary name of a standard emoji's image stem.
func stdName(stem string) string {
	genOnce.Do(func() {
		genNames = map[string]string{}
		for _, e := range emoji.StandardList() {
			if len(e.Names) > 0 {
				genNames[strings.ToLower(e.Unicode)] = e.Names[0]
			}
		}
	})
	if n, ok := genNames[stem]; ok {
		return n
	}
	return genNames[baseStem(stem)]
}

// cp is the stem's first code point (0 when unreadable).
func cp(stem string) int64 {
	first, _, _ := strings.Cut(stem, "-")
	v, _ := strconv.ParseInt(first, 16, 64)
	return v
}

func in(v, lo, hi int64) bool { return v >= lo && v <= hi }

func has(name string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(name, w) {
			return true
		}
	}
	return false
}

// skinOf is the stem's skin tone (Fitzpatrick modifiers); "" is the
// default yellow.
func skinOf(stem string) string {
	tones := map[string]string{"1f3fb": "#f7d7c4", "1f3fc": "#e8b48a", "1f3fd": "#c68642", "1f3fe": "#8d5524", "1f3ff": "#5c3a21"}
	for _, p := range strings.Split(stem, "-") {
		if t, ok := tones[p]; ok {
			return t
		}
	}
	return ""
}

var colourWords = map[string]string{"red": "#e53935", "orange": "#fb8c00", "yellow": "#fdd835", "green": "#43a047", "blue": "#1e88e5",
	"purple": "#8e24aa", "brown": "#795548", "black": "#263238", "white": "#eceff1", "pink": "#f06292", "grey": "#90a4ae", "gray": "#90a4ae"}

func colourIn(name string) (string, bool) {
	for _, w := range []string{"orange", "yellow", "green", "purple", "brown", "black", "white", "pink", "grey", "gray", "blue", "red"} {
		if strings.Contains(name, w) {
			return colourWords[w], true
		}
	}
	return "", false
}

func hashPick(s string, opts []string) string {
	h := fnv.New32a()
	h.Write([]byte(s))
	return opts[h.Sum32()%uint32(len(opts))]
}

// genEmoji draws a standard emoji from its name and stem.
func genEmoji(c *cv, name, stem string) {
	v := cp(stem)
	switch {
	case name == "":
		fallbackEmoji(c, stem)
	case strings.HasPrefix(name, "flag-") || in(v, 0x1f1e6, 0x1f1ff) || name == "checkered_flag" || name == "triangular_flag_on_post":
		genFlag(c, name)
	case strings.HasPrefix(name, "keycap") || in(v, 0x30, 0x39) || name == "hash" || name == "asterisk" || name == "keycap_ten":
		genKeycap(c, name, stem)
	case has(name, "clock") && !has(name, "alarm"):
		genClock(c, name)
	case has(name, "heart") && !has(name, "eyes", "face", "hands", "cat", "decoration", "_on_fire", "hand"):
		col, ok := colourIn(name)
		if !ok {
			col = "#e53935"
		}
		c.fill(shade(hex(col), -0.3), sh().heart(50, 54, 46))
		c.fill(hex(col), sh().heart(50, 54, 43))
		c.fill(alpha(white, 0.45), sh().ellipseRot(28, 34, 9, 5, -0.7))
	case (has(name, "circle", "square", "diamond") && !has(name, "face")) || in(v, 0x1f7e0, 0x1f7eb):
		genShape(c, name)
	case in(v, 0x1f600, 0x1f64f) && !in(v, 0x1f645, 0x1f64f) || in(v, 0x1f910, 0x1f92f) || in(v, 0x1f970, 0x1f97a) ||
		in(v, 0x1fae0, 0x1fae8) || (has(name, "face") && !has(name, "clock", "moon", "sun", "cat", "dog", "monkey", "pig", "cow", "mouse", "rabbit", "horse", "tiger", "dragon", "wolf", "fox", "bear", "panda", "frog", "unicorn")):
		genFace(c, name)
	case in(v, 0x1f466, 0x1f487) || in(v, 0x1f645, 0x1f64e) || in(v, 0x1f9d1, 0x1f9df) || in(v, 0x1f934, 0x1f93e) ||
		has(name, "man", "woman", "person", "boy", "girl", "baby", "people", "family", "couple"):
		genPerson(c, name, stem)
	case in(v, 0x1f446, 0x1f450) || in(v, 0x1f590, 0x1f596) || in(v, 0x1f918, 0x1f91f) || in(v, 0x1faf0, 0x1faf8) || in(v, 0x270a, 0x270d) ||
		has(name, "hand", "fist", "finger", "palm", "thumbs", "point_", "nail_care", "writing", "selfie", "handshake"):
		genHand(c, name, stem)
	case in(v, 0x1f400, 0x1f43f) || in(v, 0x1f980, 0x1f9ae) || in(v, 0x1f98a, 0x1f9a4) || has(name, "dog", "monkey", "pig", "cow", "mouse", "rabbit", "horse", "tiger", "wolf", "fox", "bear", "panda", "frog", "lion", "koala", "hamster", "owl", "penguin", "bird", "chicken", "duck", "eagle"):
		genAnimal(c, name)
	case in(v, 0x1f32d, 0x1f37f) || in(v, 0x1f950, 0x1f96f) || in(v, 0x1f9c0, 0x1f9cb) || in(v, 0x1fad0, 0x1fadb):
		genFood(c, name)
	case in(v, 0x1f330, 0x1f344) || has(name, "leaf", "tree", "plant", "flower", "rose", "tulip", "blossom", "cactus", "herb", "seedling", "clover"):
		genPlant(c, name)
	case in(v, 0x1f680, 0x1f6c5) || in(v, 0x1f6e0, 0x1f6fc) || has(name, "car", "bus", "train", "truck", "plane", "ship", "boat", "bike", "bicycle", "taxi", "rocket", "tram", "metro", "ambulance", "police"):
		genVehicle(c, name)
	case in(v, 0x2600, 0x2604) || in(v, 0x1f300, 0x1f32c) || has(name, "sun", "cloud", "rain", "moon", "star", "comet", "rainbow", "lightning", "tornado", "fog", "umbrella"):
		genSky(c, name)
	case has(name, "arrow") || in(v, 0x2190, 0x21ff) || in(v, 0x27a1, 0x27bf) || in(v, 0x2b05, 0x2b07):
		genArrow(c, name)
	default:
		genTile(c, name)
	}
}

// ---- smileys ----

func genFace(c *cv, n string) {
	switch {
	case has(n, "rage", "angry", "pouting", "imp", "cursing"):
		c.fill(hex("#b71c1c"), sh().circle(50, 51, 46.5))
		c.fillSrc(c.grad(50, 6, 50, 96, hex("#ff8a65"), hex("#e53935")), sh().circle(50, 51, 44.5))
	case has(n, "nauseated", "vomiting", "sick", "green"):
		c.fill(hex("#2e7d32"), sh().circle(50, 51, 46.5))
		c.fillSrc(c.grad(50, 6, 50, 96, hex("#c5e1a5"), hex("#7cb342")), sh().circle(50, 51, 44.5))
	case has(n, "cold"):
		c.fill(hex("#1565c0"), sh().circle(50, 51, 46.5))
		c.fillSrc(c.grad(50, 6, 50, 96, hex("#bbdefb"), hex("#42a5f5")), sh().circle(50, 51, 44.5))
	default:
		yface(c)
	}
	if has(n, "blush", "relaxed", "smiling_face_with_3_hearts", "kissing_closed", "innocent", "hugging", "pleading") {
		c.fill(alpha(hex("#ff6f91"), 0.45), sh().ellipse(24, 60, 9, 6).ellipse(76, 60, 9, 6))
	}
	// eyes
	switch {
	case has(n, "heart_eyes", "heart"):
		c.fill(redC, sh().heart(34, 42, 11).heart(66, 42, 11))
	case has(n, "star_struck"):
		c.fill(hex("#ff9800"), sh().star(34, 40, 12, 5, 5, 0).star(66, 40, 12, 5, 5, 0))
	case has(n, "dizzy", "dead", "knocked", "x_eyes"):
		c.fill(featC, sh().polyline(5, 28, 33, 40, 45).polyline(5, 40, 33, 28, 45).polyline(5, 60, 33, 72, 45).polyline(5, 72, 33, 60, 45))
	case has(n, "wink", "stuck_out_tongue_winking"):
		c.fill(featC, sh().ellipse(35, 40, 5, 7.5))
		c.fill(featC, sh().arc(66, 44, 8, math.Pi*1.12, math.Pi*1.88, 5))
	case has(n, "sleep", "pensive", "relieved", "relaxed", "innocent", "blush", "smirk", "kissing_closed", "lying", "yawning", "zipper"):
		c.fill(featC, sh().arc(34, 38, 8, math.Pi*0.15, math.Pi*0.85, 4.5).arc(66, 38, 8, math.Pi*0.15, math.Pi*0.85, 4.5))
	case has(n, "grin", "laugh", "joy", "smile", "happy", "satisfied"):
		eyesHappy(c, 38)
	case has(n, "cry", "sob", "disappointed", "weary", "tired", "persevere", "confounded"):
		eyesSad(c, 40)
	case has(n, "rolling_eyes", "face_with_rolling"):
		c.fill(white, sh().circle(35, 40, 9).circle(65, 40, 9))
		c.fill(featC, sh().circle(35, 34, 4.5).circle(65, 34, 4.5))
	case has(n, "astonished", "flushed", "scream", "fearful", "anguished", "hushed", "open_mouth", "exploding", "cold_sweat"):
		c.fill(white, sh().circle(35, 40, 9).circle(65, 40, 9))
		c.fill(featC, sh().circle(35, 40, 4.5).circle(65, 40, 4.5))
	case has(n, "sunglasses", "cool"):
		c.fill(hex("#212121"), sh().rrect(16, 32, 30, 16, 6).rrect(54, 32, 30, 16, 6).rect(40, 36, 20, 4))
	default:
		eyesDot(c, 40)
	}
	if has(n, "rage", "angry", "pouting", "triumph", "cursing", "imp") {
		c.fill(featC, sh().line(22, 26, 42, 32, 5).line(78, 26, 58, 32, 5))
	}
	if has(n, "thinking", "raised_eyebrow", "monocle") {
		c.fill(featC, sh().line(56, 28, 76, 24, 4.5))
	}
	// mouth
	switch {
	case has(n, "tongue", "yum", "zany", "savoring"):
		smileArc(c, 70, 18, 5)
		c.fill(tongueC, sh().rrect(48, 66, 14, 16, 7))
	case has(n, "kiss"):
		c.fill(featC, sh().arc(50, 66, 5, -math.Pi*0.5, math.Pi*0.5, 4).arc(50, 76, 5, -math.Pi*0.5, math.Pi*0.5, 4))
	case has(n, "astonished", "scream", "open_mouth", "hushed", "anguished", "fearful", "yawning", "exploding", "flushed", "cold_sweat"):
		c.fill(mouthC, sh().ellipse(50, 70, 9, 12))
	case has(n, "grimacing"):
		c.fill(white, sh().rrect(28, 62, 44, 16, 6))
		c.fill(featC, sh().rect(28, 69, 44, 2).rect(39, 62, 2, 16).rect(50, 62, 2, 16).rect(61, 62, 2, 16))
	case has(n, "grin", "laugh", "joy", "smiley", "satisfied", "sweat_smile", "beaming", "rofl"):
		openMouth(c, 58, 26, 22, true, true)
	case has(n, "neutral", "expressionless", "no_mouth", "zipper", "unamused", "straight"):
		if has(n, "zipper") {
			c.fill(featC, sh().line(30, 68, 70, 68, 4))
			for x := 32.0; x <= 68; x += 6 {
				c.fill(featC, sh().line(x, 64, x, 72, 2))
			}
		} else if !has(n, "no_mouth") {
			c.fill(featC, sh().line(34, 70, 66, 70, 5))
		}
	case has(n, "frown", "sad", "disappointed", "worried", "confused", "pensive", "cry", "sob", "weary", "tired", "persevere", "confounded",
		"angry", "rage", "pouting", "slightly_frowning", "triumph", "cursing", "nauseated", "sick"):
		c.fill(featC, sh().arc(50, 82, 16, math.Pi*1.2, math.Pi*1.8, 5))
	case has(n, "smirk"):
		c.fill(featC, sh().arc(58, 58, 14, math.Pi*0.15, math.Pi*0.6, 5))
	default:
		smileArc(c, 74, 18, 5)
	}
	// extras
	switch {
	case has(n, "sob"):
		c.fill(alpha(tearC, 0.85), sh().rect(26, 46, 9, 46).rect(65, 46, 9, 46))
	case has(n, "cry", "tear", "joy"):
		c.fill(tearC, sh().drop(24, 52, 7))
	case has(n, "sweat"):
		c.fill(tearC, sh().drop(80, 26, 8))
	}
	if has(n, "mask") {
		c.fill(white, sh().rrect(24, 54, 52, 28, 10))
		c.fill(hex("#cfd8dc"), sh().line(24, 60, 6, 52, 2).line(76, 60, 94, 52, 2))
	}
	if has(n, "zzz", "sleeping") {
		c.text(84, 22, 18, hex("#1e88e5"), "z", alignCenter, true)
	}
	if has(n, "party", "partying") {
		c.fill(hex("#8e24aa"), sh().poly(50, 0, 30, 14, 70, 14))
	}
}

// ---- people and hands ----

func genHand(c *cv, n, stem string) {
	col := skinOf(stem)
	if col == "" {
		drawHand(c, openHand, 52, 56, 1.1, 0, false)
		return
	}
	skin := hex(col)
	c.fill(shade(skin, -0.3), xf(openHand(2), 52, 56, 1.1, 0))
	c.fill(skin, xf(openHand(0), 52, 56, 1.1, 0))
}

func genPerson(c *cv, n, stem string) {
	skin := hex("#ffc83d")
	if col := skinOf(stem); col != "" {
		skin = hex(col)
	}
	shirt := hex(hashPick(n, []string{"#1e88e5", "#43a047", "#8e24aa", "#fb8c00", "#e53935", "#00897b"}))
	c.fill(shade(shirt, -0.25), sh().rrect(16, 62, 68, 40, 26))
	c.fill(shirt, sh().rrect(18, 64, 64, 38, 24))
	c.fill(shade(skin, -0.25), sh().circle(50, 38, 24))
	c.fill(skin, sh().circle(50, 38, 22))
	hair := hex(hashPick(n+"h", []string{"#3b2a20", "#6d4c41", "#212121", "#f2c14e", "#a1887f"}))
	if has(n, "woman", "girl") {
		c.fill(hair, sh().pie(50, 36, 25, math.Pi, 2*math.Pi).rect(25, 34, 8, 24).rect(67, 34, 8, 24))
	} else if !has(n, "bald", "baby") {
		c.fill(hair, sh().pie(50, 34, 23, math.Pi*1.05, math.Pi*1.95))
	}
	c.fill(featC, sh().circle(42, 40, 2.8).circle(58, 40, 2.8))
	c.fill(featC, sh().arc(50, 44, 8, math.Pi*0.25, math.Pi*0.75, 2.5))
}

// ---- symbols ----

func genFlag(c *cv, n string) {
	pole := hex("#78909c")
	c.fill(pole, sh().rect(14, 8, 6, 86))
	h := fnv.New32a()
	h.Write([]byte(n))
	x := h.Sum32()
	pal := []string{"#e53935", "#ffffff", "#1e88e5", "#fdd835", "#43a047", "#212121", "#fb8c00"}
	cols := []string{pal[x%7], pal[(x/7)%7], pal[(x/49)%7]}
	if cols[1] == cols[0] {
		cols[1] = pal[(x/7+1)%7]
	}
	if n == "checkered_flag" {
		for i := 0; i < 6; i++ {
			for j := 0; j < 4; j++ {
				col := white
				if (i+j)%2 == 0 {
					col = hex("#212121")
				}
				c.fill(col, sh().rect(20+float64(i)*12, 12+float64(j)*11, 12, 11))
			}
		}
		return
	}
	c.fill(hex("#90a4ae"), sh().rect(19, 11, 74, 52))
	vertical := x%2 == 0
	for i, col := range cols {
		if vertical {
			c.fill(hex(col), sh().rect(20+float64(i)*24, 12, 24, 50))
		} else {
			c.fill(hex(col), sh().rect(20, 12+float64(i)*16.7, 72, 16.7))
		}
	}
}

func genKeycap(c *cv, n, stem string) {
	c.fill(hex("#1565c0"), sh().rrect(6, 6, 88, 88, 16))
	c.fill(hex("#42a5f5"), sh().rrect(10, 10, 80, 80, 13))
	label := map[string]string{"zero": "0", "one": "1", "two": "2", "three": "3", "four": "4", "five": "5", "six": "6", "seven": "7",
		"eight": "8", "nine": "9", "keycap_ten": "10", "hash": "#", "asterisk": "*", "keycap_star": "*"}[n]
	if label == "" {
		if v := cp(stem); in(v, 0x30, 0x39) {
			label = string(rune(v))
		} else {
			label = "#"
		}
	}
	c.text(50, 70, 56, white, label, alignCenter, true)
}

func genClock(c *cv, n string) {
	c.fill(hex("#78909c"), sh().circle(50, 50, 45))
	c.fill(white, sh().circle(50, 50, 40))
	hour := 12
	for i, w := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"} {
		if strings.HasPrefix(strings.TrimPrefix(n, "clock"), w) || strings.Contains(n, "clock"+strconv.Itoa(i+1)) {
			hour = i + 1
		}
	}
	a := float64(hour)/12*2*math.Pi - math.Pi/2
	c.fill(hex("#263238"), sh().line(50, 50, 50+22*math.Cos(a), 50+22*math.Sin(a), 6))
	m := -math.Pi / 2
	if has(n, "30") {
		m = math.Pi / 2
	}
	c.fill(hex("#263238"), sh().line(50, 50, 50+32*math.Cos(m), 50+32*math.Sin(m), 4))
	c.fill(hex("#e53935"), sh().circle(50, 50, 4))
}

func genShape(c *cv, n string) {
	col, ok := colourIn(n)
	if !ok {
		col = "#1e88e5"
	}
	k := hex(col)
	switch {
	case has(n, "square"):
		sz := 76.0
		if has(n, "small") {
			sz = 44
		} else if has(n, "medium") {
			sz = 60
		}
		c.fill(shade(k, -0.25), sh().rrect(50-sz/2-2, 50-sz/2-2, sz+4, sz+4, 8))
		c.fill(k, sh().rrect(50-sz/2, 50-sz/2, sz, sz, 6))
	case has(n, "diamond"):
		r := 44.0
		if has(n, "small") {
			r = 26
		}
		c.fill(shade(k, -0.25), sh().poly(50, 50-r-2, 50+r+2, 50, 50, 50+r+2, 50-r-2, 50))
		c.fill(k, sh().poly(50, 50-r, 50+r, 50, 50, 50+r, 50-r, 50))
	default:
		c.fill(shade(k, -0.25), sh().circle(50, 50, 44))
		c.fill(k, sh().circle(50, 50, 41))
		c.fill(alpha(white, 0.35), sh().ellipseRot(34, 32, 11, 6, -0.7))
	}
}

func genArrow(c *cv, n string) {
	c.fill(hex("#1565c0"), sh().rrect(6, 6, 88, 88, 16))
	c.fill(hex("#42a5f5"), sh().rrect(10, 10, 80, 80, 13))
	a := 0.0 // right
	switch {
	case has(n, "arrow_up_down", "arrows_counterclockwise", "arrows_clockwise", "repeat", "twisted"):
		c.fill(white, sh().arc(50, 50, 22, 0.3, 2*math.Pi-0.3, 8))
		c.fill(white, sh().poly(72, 34, 80, 52, 62, 50))
		return
	case has(n, "left"):
		a = math.Pi
	case has(n, "up") || has(n, "top"):
		a = -math.Pi / 2
	case has(n, "down"):
		a = math.Pi / 2
	}
	if has(n, "upper_right", "_ne") {
		a = -math.Pi / 4
	} else if has(n, "lower_right", "_se") {
		a = math.Pi / 4
	}
	s := sh().rect(-26, -7, 34, 14).poly(4, -22, 28, 0, 4, 22)
	c.fill(white, xf(s, 50, 50, 1, a))
}

// ---- things ----

func genAnimal(c *cv, n string) {
	fur := hex(hashPick(n, []string{"#a1887f", "#ffb74d", "#bcaaa4", "#90a4ae", "#ffcc80", "#8d6e63", "#f8bbd0"}))
	switch {
	case has(n, "frog"):
		fur = hex("#7cb342")
	case has(n, "panda", "penguin"):
		fur = hex("#eceff1")
	case has(n, "pig"):
		fur = hex("#f8bbd0")
	case has(n, "fox", "tiger", "lion"):
		fur = hex("#fb8c00")
	}
	dark := shade(fur, -0.35)
	c.fill(dark, sh().circle(22, 26, 14).circle(78, 26, 14))
	c.fill(fur, sh().circle(22, 26, 10).circle(78, 26, 10))
	c.fill(dark, sh().circle(50, 54, 42))
	c.fill(fur, sh().circle(50, 54, 39))
	c.fill(shade(fur, 0.35), sh().ellipse(50, 70, 20, 14))
	c.fill(hex("#263238"), sh().circle(36, 48, 5).circle(64, 48, 5))
	c.fill(hex("#263238"), sh().ellipse(50, 64, 6, 4.5))
}

func genFood(c *cv, n string) {
	food := hex(hashPick(n, []string{"#ef5350", "#ffb300", "#8d6e63", "#66bb6a", "#ff7043", "#fdd835", "#ab47bc"}))
	if col, ok := colourIn(n); ok {
		food = hex(col)
	}
	switch {
	case has(n, "cup", "tea", "beer", "wine", "cocktail", "drink", "juice", "milk", "sake", "champagne", "bottle", "mate", "boba", "bubble_tea"):
		c.fill(shade(food, -0.3), sh().rrect(26, 18, 48, 70, 10))
		c.fill(food, sh().rrect(29, 30, 42, 55, 8))
		c.fill(alpha(white, 0.7), sh().rrect(26, 18, 48, 12, 6))
	default:
		c.fill(hex("#cfd8dc"), sh().ellipse(50, 74, 44, 16))
		c.fill(white, sh().ellipse(50, 72, 40, 13))
		c.fill(shade(food, -0.3), sh().pie(50, 70, 32, math.Pi, 2*math.Pi))
		c.fill(food, sh().pie(50, 70, 29, math.Pi, 2*math.Pi))
		c.fill(alpha(white, 0.4), sh().ellipseRot(38, 52, 8, 4, -0.5))
	}
}

func genPlant(c *cv, n string) {
	if has(n, "flower", "rose", "tulip", "blossom", "hibiscus", "sunflower", "bouquet") {
		petal := hex(hashPick(n, []string{"#f06292", "#ec407a", "#ffb300", "#ba68c8", "#ef5350"}))
		c.fill(hex("#43a047"), sh().rect(47, 50, 6, 46))
		for i := 0; i < 6; i++ {
			a := float64(i) / 6 * 2 * math.Pi
			c.fill(petal, sh().circle(50+18*math.Cos(a), 36+18*math.Sin(a), 13))
		}
		c.fill(hex("#fdd835"), sh().circle(50, 36, 10))
		return
	}
	g := hex(hashPick(n, []string{"#43a047", "#66bb6a", "#2e7d32", "#9ccc65"}))
	c.fill(hex("#6d4c41"), sh().rect(46, 56, 8, 38))
	c.fill(shade(g, -0.25), sh().circle(50, 38, 34))
	c.fill(g, sh().circle(50, 38, 31))
	c.fill(shade(g, 0.25), sh().circle(38, 28, 10))
}

func genVehicle(c *cv, n string) {
	body := hex(hashPick(n, []string{"#e53935", "#1e88e5", "#fdd835", "#43a047", "#fb8c00", "#eceff1"}))
	if col, ok := colourIn(n); ok {
		body = hex(col)
	}
	if has(n, "plane", "airplane") {
		c.fill(hex("#90a4ae"), xf(sh().rrect(-40, -8, 80, 16, 8).poly(-6, -8, 10, -8, -8, -40, -18, -40).poly(-6, 8, 10, 8, -8, 40, -18, 40), 50, 50, 1, -0.5))
		return
	}
	c.fill(shade(body, -0.3), sh().rrect(8, 36, 84, 38, 10))
	c.fill(body, sh().rrect(10, 38, 80, 34, 9))
	c.fill(hex("#bbdefb"), sh().rrect(22, 44, 22, 14, 3).rrect(52, 44, 22, 14, 3))
	c.fill(hex("#263238"), sh().circle(28, 76, 10).circle(72, 76, 10))
	c.fill(hex("#b0bec5"), sh().circle(28, 76, 4).circle(72, 76, 4))
}

func genSky(c *cv, n string) {
	switch {
	case has(n, "moon"):
		// a crescent: the outer circle's left side, back along an inner one
		var xy []float64
		for i := 0; i <= 24; i++ {
			a := math.Pi*0.35 + math.Pi*1.3*float64(i)/24
			xy = append(xy, 50+40*math.Cos(a), 50+40*math.Sin(a))
		}
		for i := 24; i >= 0; i-- {
			a := math.Pi*0.42 + math.Pi*1.16*float64(i)/24
			xy = append(xy, 64+30*math.Cos(a), 46+30*math.Sin(a))
		}
		c.fill(hex("#fdd835"), sh().poly(xy...))
	case has(n, "star", "comet"):
		c.fill(hex("#f9a825"), sh().star(50, 52, 44, 19, 5, -math.Pi/2))
		c.fill(hex("#fdd835"), sh().star(50, 52, 39, 16, 5, -math.Pi/2))
	case has(n, "rain", "cloud", "fog", "lightning", "tornado", "umbrella"):
		cl := hex("#cfd8dc")
		c.fill(shade(cl, -0.2), sh().circle(34, 50, 20).circle(56, 40, 24).circle(74, 54, 17).rrect(16, 50, 74, 22, 11))
		c.fill(cl, sh().circle(34, 50, 18).circle(56, 40, 22).circle(74, 54, 15).rrect(18, 52, 70, 18, 9))
		if has(n, "rain", "umbrella") {
			c.fill(tearC, sh().drop(34, 84, 5).drop(54, 88, 5).drop(72, 84, 5))
		}
		if has(n, "lightning") {
			c.fill(hex("#fdd835"), sh().poly(52, 64, 42, 84, 52, 84, 46, 98, 62, 78, 52, 78, 58, 64))
		}
	default: // the sun
		for i := 0; i < 8; i++ {
			a := float64(i) / 8 * 2 * math.Pi
			c.fill(hex("#ffb300"), sh().line(50+30*math.Cos(a), 50+30*math.Sin(a), 50+44*math.Cos(a), 50+44*math.Sin(a), 7))
		}
		c.fill(hex("#ffb300"), sh().circle(50, 50, 26))
		c.fill(hex("#fdd835"), sh().circle(50, 50, 23))
	}
}

// genTile is any other emoji: a rounded tile in a colour of its name
// with its first letter.
func genTile(c *cv, n string) {
	col := hex(hashPick(n, []string{"#5c6bc0", "#26a69a", "#ef6c00", "#8e24aa", "#00897b", "#6d4c41", "#3949ab", "#c2185b"}))
	c.fill(shade(col, -0.3), sh().rrect(8, 8, 84, 84, 18))
	c.fill(col, sh().rrect(11, 11, 78, 78, 16))
	c.fill(alpha(white, 0.18), sh().rrect(11, 11, 78, 30, 16))
	r := []rune(strings.TrimLeft(n, "_-0123456789"))
	l := "?"
	if len(r) > 0 {
		l = strings.ToUpper(string(r[0]))
	}
	c.text(50, 68, 46, white, l, alignCenter, true)
}
