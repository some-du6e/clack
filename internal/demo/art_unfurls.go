package demo

// The link previews' pictures (history "unfurls"): the Quillies
// nomination page's card and a trade paper's vending machine story.

func init() {
	for _, f := range []struct {
		key  string
		w, h int
		art  func(c *cv, W, H float64)
	}{
		{"quillies-card", 1200, 630, artQuilliesCard},
		{"vending-news", 1200, 630, artVendingNews},
	} {
		fileSizes[f.key] = [2]int{f.w, f.h}
		fileArt[f.key] = f.art
		fileKeys = append(fileKeys, f.key)
	}
}

func artQuilliesCard(c *cv, W, H float64) {
	teal, gold, cream := hex("#0f4c5c"), hex("#f2b134"), hex("#f6efd9")
	gfill(c, 0, 0, W, H, shade(teal, 0.1), shade(teal, -0.25), sh().rect(0, 0, W, H))
	for _, d := range [][3]float64{{120, 90, 10}, {880, 70, 14}, {940, 400, 8}, {70, 420, 12}, {600, 60, 6}, {300, 460, 7}} {
		c.fill(alpha(gold, 0.7), sh().star(d[0], d[1], d[2]*2, d[2], 5, 0))
	}
	// the trophy
	cx := 760.0
	c.fill(gold, sh().rrect(cx-110, 120, 220, 170, 60))
	c.fill(gold, sh().ring(cx-120, 190, 45, 18).ring(cx+120, 190, 45, 18))
	c.fill(shade(gold, -0.2), sh().rect(cx-22, 280, 44, 70))
	c.fill(shade(gold, -0.3), sh().rrect(cx-90, 340, 180, 46, 10))
	c.fill(cream, sh().rrect(cx-60, 395, 120, 26, 6))
	quill(c, cx-40, 260, cx+50, 140, 10, cream)
	c.text(70, 220, 76, cream, "The Quillies", alignLeft, true)
	c.text(70, 280, 30, gold, "4th annual Excellence in Paper Awards", alignLeft, true)
	c.text(70, 340, 26, alpha(cream, 0.85), "Nominations close Thursday, 5pm", alignLeft, false)
	c.text(70, 380, 26, alpha(cream, 0.85), "Ceremony Friday, 3pm · Break room", alignLeft, false)
}

func artVendingNews(c *cv, W, H float64) {
	c.bg(hex("#f4efe6"))
	ink, red := hex("#2b2b2b"), hex("#c8442f")
	// the machine
	x, y := 640.0, 40.0
	c.fill(hex("#2f3b45"), sh().rrect(x, y, 300, 400, 18))
	c.fill(hex("#cfe3ea"), sh().rrect(x+20, y+20, 190, 300, 8))
	for r := 0; r < 4; r++ {
		for k := 0; k < 3; k++ {
			col := []string{"#f2b134", "#c8442f", "#2a9d8f"}[(r+k)%3]
			c.fill(hex(col), sh().rrect(x+32+float64(k)*60, y+36+float64(r)*72, 44, 52, 6))
		}
	}
	c.fill(hex("#11171c"), sh().rrect(x+225, y+40, 55, 90, 6))
	c.fill(red, sh().circle(x+252, y+70, 12)) // the "eye"
	c.fill(alpha(white, 0.8), sh().circle(x+256, y+66, 4))
	c.fill(hex("#11171c"), sh().rrect(x+30, y+340, 170, 40, 6))
	// the headline block
	c.fill(red, sh().rect(0, 0, 18, H))
	c.text(60, 80, 24, red, "PULP & PAPER WEEKLY · OFFICE LIFE", alignLeft, true)
	c.text(60, 160, 44, ink, "Vending machines", alignLeft, true)
	c.text(60, 215, 44, ink, "now \"learn\" your", alignLeft, true)
	c.text(60, 270, 44, ink, "snack habits", alignLeft, true)
	greyLines(c, 60, 330, 520, 34, 5, hex("#d9d2c5"))
}
