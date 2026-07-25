package main

// Starter layouts. A new screen is never a blank rectangle, and loading one is
// a single click. Coordinates are laid out for the screen's actual size rather
// than a fixed 800x480, so a 400x300 badge gets a sane version of the same idea.
type starter struct {
	ID    string
	Name  string
	About string
}

var starters = []starter{
	{"clock-rows", "Clock And Rows", "A black title bar, a large clock, the date, and a label/value table."},
	{"photo", "Photo Frame", "One dithered image filling the screen with a caption bar across the bottom."},
	{"status", "Status Board", "Boxed panels, dividers and two live data blocks."},
}

func starterByID(id string) (starter, bool) {
	for _, s := range starters {
		if s.ID == id {
			return s, true
		}
	}
	return starter{}, false
}

// starterBlocks builds one of the starters at the screen's dimensions.
func starterBlocks(id string, sc Screen) []Block {
	w, h := sc.W, sc.H
	pad := scaled(w, h, 16, 6)

	switch id {
	case "photo":
		capH := scaled(w, h, 44, 18)
		capFont, capSize := fontForHeight(capH-8, "go-bold", "pixel8x16")
		return []Block{
			{Type: BlockImage, X: 0, Y: 0, W: w, H: h - capH, Anchor: "c", Align: "left",
				Dither: "floyd", Fit: "cover"},
			{Type: BlockText, X: 0, Y: h - capH, W: w, H: capH, Anchor: "c", Align: "center",
				Text: "Kodak Gold 200 · Melbourne", Font: capFont, Size: capSize, Invert: true},
		}

	case "status":
		barH := scaled(w, h, 48, 20)
		barFont, barSize := fontForHeight(barH-10, "go-bold", "pixel8x16")
		lblFont, lblSize := fontForHeight(scaled(w, h, 20, 14), "go", "pixel7x13")
		clockH := scaled(w, h, 40, 18)
		progH := scaled(w, h, 44, 22)
		cardW := (w - 3*pad) / 2

		// Measured from both ends so the cards grow into whatever is left,
		// rather than leaving a hole in the middle of the panel.
		clockTop := h - pad - clockH
		progTop := clockTop - pad - progH
		divTop := progTop - pad
		cardTop := barH + pad
		cardH := divTop - pad - cardTop
		if cardH < 40 {
			cardH = 40
		}
		valFont, valSize := fontForHeight(cardH/2, "go-bold", "pixel8x16-bold")
		return []Block{
			{Type: BlockBox, X: 0, Y: 0, W: w, H: barH, Fill: true, Anchor: "nw", Align: "left"},
			{Type: BlockText, X: pad, Y: 0, W: w - 2*pad, H: barH, Anchor: "w", Align: "left",
				Text: "Status Board", Font: barFont, Size: barSize, Invert: true},

			{Type: BlockBox, X: pad, Y: cardTop, W: cardW, H: cardH, Thickness: 2, Anchor: "nw", Align: "left"},
			{Type: BlockText, X: pad + 8, Y: cardTop + 6, W: cardW - 16, H: lblSize + 4, Anchor: "nw", Align: "left",
				Text: "Outside", Font: lblFont, Size: lblSize},
			{Type: BlockData, X: pad + 8, Y: cardTop + 6 + lblSize + 4, W: cardW - 16, H: cardH - 18 - lblSize,
				Anchor: "w", Align: "left", URL: "https://wttr.in/Melbourne?format=j1",
				Path: "current_condition.0.temp_C", Text: "{{v}}°C", Fallback: "—",
				Interval: defaultInterval, Timeout: defaultTimeout, Font: valFont, Size: valSize},

			{Type: BlockBox, X: 2*pad + cardW, Y: cardTop, W: cardW, H: cardH, Thickness: 2, Anchor: "nw", Align: "left"},
			{Type: BlockText, X: 2*pad + cardW + 8, Y: cardTop + 6, W: cardW - 16, H: lblSize + 4, Anchor: "nw", Align: "left",
				Text: "Humidity", Font: lblFont, Size: lblSize},
			{Type: BlockData, X: 2*pad + cardW + 8, Y: cardTop + 6 + lblSize + 4, W: cardW - 16, H: cardH - 18 - lblSize,
				Anchor: "w", Align: "left", URL: "https://wttr.in/Melbourne?format=j1",
				Path: "current_condition.0.humidity", Text: "{{v}}%", Fallback: "—",
				Interval: defaultInterval, Timeout: defaultTimeout, Font: valFont, Size: valSize},

			{Type: BlockDivider, X: pad, Y: divTop, W: w - 2*pad, H: 2,
				Orient: "horizontal", Thickness: 2, Anchor: "nw", Align: "left"},
			{Type: BlockProgress, X: pad, Y: progTop, W: w - 2*pad, H: progH,
				Anchor: "nw", Align: "left", Label: "Battery", Value: 78, Font: lblFont, Size: lblSize},
			{Type: BlockDate, X: pad, Y: clockTop, W: (w - 2*pad) / 2, H: clockH,
				Anchor: "w", Align: "left", Format: "medium", Font: lblFont, Size: lblSize},
			{Type: BlockClock, X: w / 2, Y: clockTop, W: w/2 - pad, H: clockH,
				Anchor: "e", Align: "right", Format: "24h", Font: lblFont, Size: lblSize},
		}

	default: // clock-rows
		barH := scaled(w, h, 52, 20)
		barFont, barSize := fontForHeight(barH-12, "go-bold", "pixel8x16-bold")
		clockH := scaled(w, h, 124, 46)
		clockFont, clockSize := fontForHeight(clockH-16, "go-bold", "pixel8x16-bold")
		dateFont, dateSize := fontForHeight(scaled(w, h, 28, 16), "go", "pixel7x13")
		rowsTop := barH + clockH + 12
		rowsH := h - rowsTop - pad
		if rowsH < 24 {
			rowsH = 24
		}
		rowFont, rowSize := fontForHeight(scaled(w, h, 22, 16), "go", "pixel8x16")
		return []Block{
			{Type: BlockBox, X: 0, Y: 0, W: w, H: barH, Fill: true, Anchor: "nw", Align: "left"},
			{Type: BlockText, X: pad, Y: 0, W: w - 2*pad, H: barH, Anchor: "w", Align: "left",
				Text: "panel", Font: barFont, Size: barSize, Invert: true},
			{Type: BlockClock, X: pad, Y: barH, W: w/2 - pad, H: clockH, Anchor: "w", Align: "left",
				Format: "24h", Font: clockFont, Size: clockSize},
			{Type: BlockDate, X: w / 2, Y: barH, W: w/2 - pad, H: clockH, Anchor: "e", Align: "right",
				Format: "long", Font: dateFont, Size: dateSize},
			{Type: BlockDivider, X: 0, Y: barH + clockH, W: w, H: 3,
				Orient: "horizontal", Thickness: 3, Anchor: "nw", Align: "left"},
			{Type: BlockRows, X: pad, Y: rowsTop, W: w - 2*pad, H: rowsH, Anchor: "nw", Align: "left",
				Font: rowFont, Size: rowSize, Rules: true, Rows: []Row{
					{"Bin Night", "Tuesday"},
					{"Next Tram 96", "6 min"},
					{"Melbourne", "14C, rain"},
					{"Battery", "3.94 V"},
				}},
		}
	}
}

// scaled maps a reference measurement on an 800x480 panel onto this screen,
// never going below a floor.
func scaled(w, h, ref, min int) int {
	sx := float64(w) / 800.0
	sy := float64(h) / 480.0
	s := sx
	if sy < s {
		s = sy
	}
	v := int(float64(ref)*s + 0.5)
	if v < min {
		v = min
	}
	return v
}

// fontForHeight picks the largest legal size of the preferred outline face
// that fits the available height, and falls back to the named bitmap face at
// its native size when that would go under the outline face's floor. This is
// the whole font/size rule applied automatically for generated layouts.
func fontForHeight(avail int, outline, bitmap string) (string, int) {
	ot, ok := lookupFace(outline)
	if !ok {
		return defaultFontID, faceByID[defaultFontID].Native
	}
	if avail >= ot.Min {
		size := avail
		if size > ot.Max {
			size = ot.Max
		}
		return ot.ID, size
	}
	bt, ok := lookupFace(bitmap)
	if !ok {
		bt = faceByID[defaultFontID]
	}
	// Largest whole multiple that fits, else the native size.
	best := bt.Native
	for _, s := range bt.LegalSizes() {
		if s <= avail && s > best {
			best = s
		}
	}
	return bt.ID, best
}
