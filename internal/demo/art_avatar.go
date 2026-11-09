package demo

// Avatars and the team icon. A person is drawn from their Avatar spec in a
// 100x100 unit space: background, shoulders, head, hair by style, a face
// and an extra (tie, glasses, lanyard, headset, hardhat…). Bots draw a
// glyph instead. Shapes are bold so a 24px avatar still reads as that
// person; details (eyebrows, blush) just fade out when small.

import (
	"image/color"
	"math"
)

// drawAvatar renders a at px x px.
func drawAvatar(a Avatar, px int) []byte {
	c := newCV(px, px, float64(px)/100)
	bg := hex(a.Bg)
	gfill(c, 0, 0, 0, 100, shade(bg, 0.12), shade(bg, -0.12), sh().rect(0, 0, 100, 100))
	switch a.Glyph {
	case "quill":
		drawQuillGlyph(c, hex(a.Accent))
		return c.png()
	case "pulse":
		drawPulseGlyph(c, hex(a.Accent))
		return c.png()
	}
	skin, hair, acc := hex(a.Skin), hex(a.Hair), hex(a.Accent)
	if a.Skin == "" {
		skin = hex("#e0ac69")
	}
	shirt := shade(mix(bg, hex("#3d4a5c"), 0.6), -0.15)
	switch a.Extra {
	case "tie":
		shirt = hex("#2f3b52") // a jacket
	case "hardhat":
		shirt = hex("#ff8c1a") // hi-vis
	}

	// hair that hangs behind the head
	switch a.Style {
	case "long":
		c.fill(hair, sh().blob(28, 30, 50, 16, 72, 30, 76, 62, 74, 86, 26, 86, 24, 62))
	case "afro":
		c.fill(hair, sh().circle(50, 36, 31))
	case "bob":
		c.fill(hair, sh().blob(28, 30, 50, 18, 72, 30, 74, 58, 68, 66, 32, 66, 26, 58))
	}

	// shoulders and neck
	c.fill(shirt, sh().ellipse(50, 104, 42, 28))
	c.fill(shade(skin, -0.12), sh().rrect(43, 58, 14, 18, 5))
	switch a.Extra {
	case "tie":
		c.fill(white, sh().poly(40, 77, 50, 92, 60, 77, 56, 74, 44, 74))
		c.fill(acc, sh().poly(47.5, 78, 52.5, 78, 54, 82, 51.5, 99, 48.5, 99, 46, 82))
		c.fill(shade(acc, -0.25), sh().poly(47.5, 76, 52.5, 76, 53, 80, 47, 80))
	case "hardhat":
		c.fill(hex("#e8e8e8"), sh().rect(30, 84, 40, 3).rect(30, 92, 40, 3)) // reflective stripes
	case "hoodie":
		c.fill(shade(shirt, -0.2), sh().ellipse(50, 78, 18, 6))
		c.fill(acc, sh().line(45, 80, 44, 95, 1.6).line(55, 80, 56, 95, 1.6))
	default:
		c.fill(shade(shirt, 0.15), sh().poly(42, 76, 50, 84, 58, 76, 54, 74, 46, 74)) // a collar
	}

	// ears and head
	c.fill(shade(skin, -0.05), sh().ellipse(28.5, 47, 4.5, 6.5).ellipse(71.5, 47, 4.5, 6.5))
	gfill(c, 0, 22, 0, 70, shade(skin, 0.06), shade(skin, -0.06), sh().ellipse(50, 45, 21.5, 24.5))

	// hair on top of the head
	switch a.Style {
	case "short":
		c.fill(hair, sh().blob(28, 40, 30, 27, 42, 19, 58, 19, 70, 27, 72, 40, 64, 31, 50, 28, 36, 31))
	case "side":
		c.fill(hair, sh().blob(28, 42, 30, 26, 44, 18, 62, 20, 72, 30, 72, 42, 66, 33, 46, 31, 34, 36))
	case "long", "bob":
		c.fill(hair, sh().blob(28, 44, 32, 26, 50, 19, 68, 26, 72, 44, 66, 34, 52, 30, 40, 31, 34, 36))
	case "bun":
		c.fill(hair, sh().circle(50, 15, 9.5))
		c.fill(hair, sh().blob(28, 42, 31, 27, 50, 20, 69, 27, 72, 42, 64, 32, 50, 29, 36, 32))
	case "curly":
		s := sh()
		for i := 0; i < 9; i++ {
			t := math.Pi + math.Pi*float64(i)/8
			s.circle(50+23*math.Cos(t), 40+19*math.Sin(t), 7.5)
		}
		s.ellipse(50, 30, 18, 9)
		c.fill(hair, s)
	case "afro":
		c.fill(shade(hair, 0.08), sh().blob(30, 38, 36, 26, 50, 22, 64, 26, 70, 38, 60, 32, 40, 32))
	case "messy":
		c.fill(hair, sh().poly(28, 42, 27, 30, 33, 26, 32, 18, 40, 22, 44, 14, 50, 20, 57, 13, 60, 21, 68, 18, 67, 26, 73, 30, 72, 42, 64, 32, 50, 30, 36, 32))
	case "balding":
		c.fill(hair, sh().blob(27, 50, 28, 36, 33, 33, 34, 44).blob(73, 50, 72, 36, 67, 33, 66, 44))
		c.fill(alpha(white, 0.25), sh().ellipse(44, 27, 6, 2.5)) // the shine
	case "mohawk":
		c.fill(shade(hair, -0.45), sh().blob(30, 38, 36, 27, 50, 23, 64, 27, 70, 38, 60, 32, 40, 32)) // shaved sides
		c.fill(hair, sh().poly(44, 30, 42, 20, 46, 12, 47, 4, 52, 10, 54, 2, 57, 12, 58, 22, 56, 30))
	}

	// face
	brow := shade(hair, -0.1)
	if a.Style == "bald" || a.Style == "balding" {
		brow = shade(skin, -0.45)
	}
	c.fill(brow, sh().line(38, 39, 45, 38, 2).line(55, 38, 62, 39, 2))
	c.fill(hex("#2b2522"), sh().ellipse(41.5, 46, 2.6, 3.1).ellipse(58.5, 46, 2.6, 3.1))
	c.fill(alpha(white, 0.9), sh().circle(42.3, 45, 0.9).circle(59.3, 45, 0.9))
	c.fill(shade(skin, -0.22), sh().arc(50, 51, 3, math.Pi*0.15, math.Pi*0.85, 1.6))
	c.fill(alpha(hex("#ff6b6b"), 0.18), sh().circle(36, 54, 4).circle(64, 54, 4))
	c.fill(hex("#8a3b32"), sh().arc(50, 53.5, 8, math.Pi*0.2, math.Pi*0.8, 2.2))

	// extras on top
	switch a.Extra {
	case "glasses":
		g := shade(acc, -0.1)
		c.fill(g, sh().rrect(34, 41, 15, 11, 4).rrect(36, 43, 11, 7, 2.5).hole())
		c.fill(g, sh().rrect(51, 41, 15, 11, 4).rrect(53, 43, 11, 7, 2.5).hole())
		c.fill(g, sh().line(49, 45, 51, 45, 1.6).line(34, 44, 29, 43, 1.6).line(66, 44, 71, 43, 1.6))
		c.fill(alpha(white, 0.25), sh().rrect(36, 43, 11, 7, 2.5).rrect(53, 43, 11, 7, 2.5))
	case "earrings":
		c.fill(acc, sh().circle(28, 55, 2.2).circle(72, 55, 2.2))
	case "lanyard":
		c.fill(acc, sh().polyline(2.4, 41, 72, 50, 90, 59, 72))
		c.fill(white, sh().rrect(43, 89, 14, 11, 1.5))
		c.fill(hex("#2a9df4"), sh().rect(43, 89, 14, 3))
		c.fill(hex("#ff9f1c"), sh().rrect(57, 84, 10, 6, 3).circle(66, 87, 4)) // the whistle
		c.fill(shade(hex("#ff9f1c"), -0.4), sh().circle(66, 87, 1.5))
	case "headset":
		band := hex("#2b2d42")
		c.fill(band, sh().arc(50, 45, 25, math.Pi*1.08, math.Pi*1.92, 3.5))
		c.fill(band, sh().rrect(23, 40, 8, 14, 3).rrect(69, 40, 8, 14, 3))
		c.fill(band, sh().quad(2, 27, 52, 28, 64, 40, 61))
		c.fill(acc, sh().circle(41, 61, 2.6))
	case "hardhat":
		hat := acc
		gfill(c, 0, 10, 0, 34, shade(hat, 0.2), shade(hat, -0.1), sh().blob(27, 33, 30, 18, 50, 9, 70, 18, 73, 33))
		c.fill(shade(hat, -0.15), sh().rrect(22, 30, 56, 6, 3))
		c.fill(shade(hat, 0.3), sh().rrect(47, 11, 6, 19, 3))
	case "bow":
		c.fill(acc, sh().poly(64, 24, 76, 16, 77, 32).poly(64, 24, 54, 14, 52, 30))
		c.fill(shade(acc, -0.2), sh().circle(64, 24, 3.5))
	case "beanie":
		c.fill(acc, sh().blob(27, 34, 30, 18, 50, 10, 70, 18, 73, 34))
		c.fill(shade(acc, -0.2), sh().rrect(26, 29, 48, 8, 4))
		c.fill(shade(acc, 0.35), sh().circle(50, 9, 5))
	}
	return c.png()
}

// drawQuillGlyph is Inkwell's mark: a quill over an ink drop.
func drawQuillGlyph(c *cv, acc color.RGBA) {
	c.fill(alpha(black, 0.18), sh().ellipse(36, 80, 18, 5))
	c.fill(hex("#0b1626"), sh().rrect(22, 66, 28, 14, 5).rect(26, 62, 20, 6))
	c.fill(acc, sh().ellipse(36, 66, 10, 2.5))
	quill(c, 36, 66, 80, 14, 13, mix(acc, white, 0.55))
}

// drawPulseGlyph is Pulse's mark: a heartbeat line in a ring.
func drawPulseGlyph(c *cv, fg color.RGBA) {
	c.fill(alpha(fg, 0.25), sh().ring(50, 50, 33, 5))
	c.fill(fg, sh().polyline(6, 14, 52, 32, 52, 40, 36, 50, 70, 58, 26, 66, 52, 86, 52))
	c.fill(fg, sh().circle(86, 52, 5))
}

// drawTeam is Quillfeather's icon: a cream quill on deep teal with a "Q".
func drawTeam(px int) []byte {
	c := newCV(px, px, float64(px)/100)
	teal := hex("#0f4c5c")
	gfill(c, 0, 0, 100, 100, shade(teal, 0.15), shade(teal, -0.2), sh().rect(0, 0, 100, 100))
	cream := hex("#f6efd9")
	c.fill(cream, sh().ring(44, 50, 24, 9))
	c.fill(cream, sh().line(56, 62, 72, 80, 9))
	quill(c, 30, 86, 86, 12, 12, hex("#f2b134"))
	return c.png()
}
