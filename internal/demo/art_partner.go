package demo

// Papertrail Freight's files in #papertrail-freight (Slack Connect): the
// loading dock map for the week and the Q1 rate card PDF.

func init() {
	for _, f := range []struct {
		key  string
		w, h int
		art  func(c *cv, W, H float64)
	}{
		{"dock-map", 1600, 1000, artDockMap},
		{"rate-card", 612, 792, artRateCard},
	} {
		fileSizes[f.key] = [2]int{f.w, f.h}
		fileArt[f.key] = f.art
		fileKeys = append(fileKeys, f.key)
	}
}

// ---- dock-map ----

func artDockMap(c *cv, W, H float64) {
	c.bg(hex("#d9dcd6")) // concrete
	asphalt, yel, ink := hex("#4a4e52"), hex("#f2c300"), hex("#22262a")
	// the building along the top, three dock doors
	c.fill(hex("#a7b0b7"), sh().rect(0, 0, W, 230))
	c.fill(hex("#8b959d"), sh().rect(0, 210, W, 20))
	for i, x := range []float64{140, 420, 700} {
		col := hex("#6f7a83")
		if i == 1 {
			col = hex("#3c8d5a") // dock 2: open
		}
		if i == 2 {
			col = hex("#b5483a") // dock 3: closed for repair
		}
		c.fill(col, sh().rect(x, 70, 200, 150))
		for y := 90.0; y < 210; y += 22 {
			c.fill(alpha(white, 0.25), sh().rect(x+10, y, 180, 4))
		}
		c.text(x+100, 55, 30, ink, []string{"DOCK 1", "DOCK 2", "DOCK 3"}[i], alignCenter, true)
	}
	c.fill(white, sh().rrect(690, 120, 220, 60, 10))
	c.text(800, 160, 26, hex("#b5483a"), "CLOSED THU", alignCenter, true)
	// the yard
	c.fill(asphalt, sh().rect(0, 230, W, H-230))
	for x := 60.0; x < W; x += 180 {
		c.fill(alpha(white, 0.55), sh().rect(x, H-120, 90, 12))
	}
	// the yellow line from the gate to dock 2
	c.fill(yel, sh().polyline(18, 1500, H-60, 1300, H-260, 760, H-420, 520, 300))
	c.fill(yel, sh().poly(520, 240, 480, 320, 560, 320))
	// a truck backing in
	c.fill(white, sh().rrect(450, 330, 140, 260, 12))
	c.fill(hex("#2a9d8f"), sh().rrect(455, 590, 130, 110, 16))
	c.fill(hex("#cfe8e5"), sh().rrect(470, 650, 100, 36, 8))
	c.text(520, 470, 26, hex("#2a9d8f"), "PTF", alignCenter, true)
	// a sticky note
	c.fill(hex("#fff3a3"), rot(1260, 450, 420, 300, -0.05))
	c.text(1260, 400, 40, ink, "Use DOCK 2", alignCenter, true)
	c.text(1260, 460, 30, ink, "follow the yellow line", alignCenter, false)
	c.text(1260, 520, 28, hex("#7a6a00"), "— Wendell", alignCenter, false)
}

// ---- rate-card ----

func artRateCard(c *cv, W, H float64) {
	c.bg(white)
	teal, sand, ink := hex("#2a9d8f"), hex("#e9c46a"), hex("#23303f")
	c.fill(teal, sh().rect(0, 0, W, 210))
	c.fill(sand, sh().rect(0, 210, W, 14))
	// the logo: a paper plane over a road
	c.fill(white, sh().poly(70, 140, 170, 70, 130, 160))
	c.fill(alpha(white, 0.7), sh().poly(130, 160, 170, 70, 150, 150))
	c.text(200, 120, 50, white, "Papertrail Freight", alignLeft, true)
	c.text(200, 165, 24, alpha(white, 0.85), "Regional LTL · Ohio Valley & Great Lakes", alignLeft, false)
	c.text(70, 320, 56, ink, "Q1 Rate Card", alignLeft, true)
	c.text(70, 370, 26, hex("#7a8592"), "Prepared for Quillfeather Paper Co., Wexley Falls", alignLeft, false)
	// the table
	rows := [][3]string{{"Pallet (up to 1,500 lb)", "$84", "$96"}, {"Half pallet", "$52", "$58"}, {"Fragile crate", "$31", "$35"},
		{"Liftgate", "$18", "$18"}, {"Saturday pickup", "$45", "—"}}
	y := 450.0
	c.fill(hex("#eef6f5"), sh().rect(60, y-40, W-120, 56))
	c.text(80, y, 24, teal, "SERVICE", alignLeft, true)
	c.text(700, y, 24, teal, "Q1", alignRight, true)
	c.text(900, y, 24, teal, "HOLIDAY", alignRight, true)
	for i, r := range rows {
		ry := y + 70 + float64(i)*62
		if i%2 == 1 {
			c.fill(hex("#f6f8f9"), sh().rect(60, ry-40, W-120, 62))
		}
		c.text(80, ry, 26, ink, r[0], alignLeft, false)
		c.text(700, ry, 26, ink, r[1], alignRight, true)
		c.text(900, ry, 26, hex("#9aa3ad"), r[2], alignRight, false)
	}
	c.fill(sand, sh().rrect(60, 880, W-120, 110, 14))
	c.text(90, 945, 28, ink, "Holiday surcharge ends January 6", alignLeft, true)
	greyLines(c, 70, 1060, 860, 34, 4, hex("#e3e7ec"))
	c.fill(hex("#eef0f3"), sh().rect(0, H-70, W, 70))
	c.text(70, H-28, 20, hex("#8a94a0"), "papertrail-freight.example · rates in USD", alignLeft, false)
	c.text(W-70, H-28, 20, hex("#8a94a0"), "Page 1 of 3", alignRight, false)
}

// drawPartnerTeam is Papertrail Freight's workspace icon: a paper plane
// over a road on teal.
func drawPartnerTeam(px int) []byte {
	c := newCV(px, px, float64(px)/100)
	teal := hex("#2a9d8f")
	gfill(c, 0, 0, 100, 100, shade(teal, 0.12), shade(teal, -0.18), sh().rect(0, 0, 100, 100))
	c.fill(hex("#264653"), sh().poly(30, 100, 70, 100, 56, 62, 44, 62))
	c.fill(hex("#e9c46a"), sh().rect(48.5, 70, 3, 8).rect(48.5, 86, 3, 10))
	c.fill(white, sh().poly(18, 44, 82, 22, 52, 58))
	c.fill(hex("#d8efec"), sh().poly(52, 58, 82, 22, 46, 48))
	return c.png()
}
