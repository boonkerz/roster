package main

// Farbthema. Die Farben bleiben Paketvariablen (Zeichencode liest sie direkt);
// applyTheme setzt sie komplett um. Dunkel ist die Vorgabe, Hell folgt denselben
// Rollen (Hintergrund, Fläche, Zeile, Text …), nur mit umgekehrter Helligkeit.

var (
	colBg, colPanel, colRow, colHover, colSelected, colLine   [3]uint8
	colText, colMuted, colStrong, colOnAccent, colAccentText  [3]uint8
	colGreen, colRed, colAmber, colAccent                     [3]uint8
	colBtn, colBtnHover, colBtnAccentHover, colBtnDangerHover [3]uint8
	colGuestRow, colFieldFocus                                [3]uint8
	colErrText, colWarnText, colOkText                        [3]uint8
)

type palette struct {
	bg, panel, row, hover, selected, line         [3]uint8
	text, muted, strong, onAccent, accentText     [3]uint8
	green, red, amber, accent                     [3]uint8
	btn, btnHover, btnAccentHover, btnDangerHover [3]uint8
	guestRow, fieldFocus                          [3]uint8
	errText, warnText, okText                     [3]uint8
}

// themeDark: dieselbe Sprache wie Web-UI und Viewer (dunkel, ruhig, ein Akzent).
var themeDark = palette{
	bg: [3]uint8{0x0b, 0x0e, 0x14}, panel: [3]uint8{0x14, 0x1a, 0x22}, row: [3]uint8{0x11, 0x17, 0x20},
	hover: [3]uint8{0x1b, 0x24, 0x30}, selected: [3]uint8{0x1a, 0x2a, 0x3d}, line: [3]uint8{0x23, 0x2b, 0x38},
	text: [3]uint8{0xd7, 0xdc, 0xe5}, muted: [3]uint8{0x84, 0x8e, 0x9f}, strong: [3]uint8{0xff, 0xff, 0xff},
	onAccent: [3]uint8{0xff, 0xff, 0xff}, accentText: [3]uint8{0x7f, 0xb0, 0xf0},
	green: [3]uint8{0x2e, 0x7d, 0x32}, red: [3]uint8{0x9c, 0x2b, 0x2b}, amber: [3]uint8{0x9a, 0x6b, 0x1f},
	accent: [3]uint8{0x2f, 0x5a, 0x8f},
	btn:    [3]uint8{0x1e, 0x26, 0x31}, btnHover: [3]uint8{0x2b, 0x36, 0x45},
	btnAccentHover: [3]uint8{0x3b, 0x6f, 0xad}, btnDangerHover: [3]uint8{0xb8, 0x35, 0x35},
	guestRow: [3]uint8{0x0e, 0x13, 0x1b}, fieldFocus: [3]uint8{0x18, 0x22, 0x2f},
	errText: [3]uint8{0xff, 0x8f, 0x8f}, warnText: [3]uint8{0xf0, 0xc0, 0x60}, okText: [3]uint8{0x8f, 0xd3, 0x8f},
}

// themeLight: helle Flächen, dunkle Schrift; Plaketten behalten ihre Signalfarben
// (sie werden halbtransparent gefüllt und bleiben so auch auf Weiß blass genug).
var themeLight = palette{
	bg: [3]uint8{0xf2, 0xf4, 0xf7}, panel: [3]uint8{0xff, 0xff, 0xff}, row: [3]uint8{0xff, 0xff, 0xff},
	hover: [3]uint8{0xe6, 0xec, 0xf4}, selected: [3]uint8{0xdb, 0xe6, 0xf5}, line: [3]uint8{0xd5, 0xdb, 0xe4},
	text: [3]uint8{0x1f, 0x27, 0x33}, muted: [3]uint8{0x62, 0x6d, 0x7d}, strong: [3]uint8{0x0f, 0x14, 0x1b},
	onAccent: [3]uint8{0xff, 0xff, 0xff}, accentText: [3]uint8{0x24, 0x56, 0xa0},
	green: [3]uint8{0x2e, 0x7d, 0x32}, red: [3]uint8{0xc2, 0x3b, 0x3b}, amber: [3]uint8{0xb4, 0x76, 0x1a},
	accent: [3]uint8{0x2f, 0x5a, 0x8f},
	btn:    [3]uint8{0xe1, 0xe6, 0xee}, btnHover: [3]uint8{0xd0, 0xd8, 0xe3},
	btnAccentHover: [3]uint8{0x3b, 0x6f, 0xad}, btnDangerHover: [3]uint8{0xb8, 0x35, 0x35},
	guestRow: [3]uint8{0xf7, 0xf9, 0xfc}, fieldFocus: [3]uint8{0xee, 0xf3, 0xfa},
	errText: [3]uint8{0xb4, 0x23, 0x18}, warnText: [3]uint8{0x9a, 0x6b, 0x00}, okText: [3]uint8{0x1f, 0x7a, 0x3a},
}

// applyTheme schaltet die Palette um ("light", sonst dunkel).
func applyTheme(name string) {
	p := themeDark
	if name == "light" {
		p = themeLight
	}
	colBg, colPanel, colRow, colHover, colSelected, colLine = p.bg, p.panel, p.row, p.hover, p.selected, p.line
	colText, colMuted, colStrong, colOnAccent, colAccentText = p.text, p.muted, p.strong, p.onAccent, p.accentText
	colGreen, colRed, colAmber, colAccent = p.green, p.red, p.amber, p.accent
	colBtn, colBtnHover, colBtnAccentHover, colBtnDangerHover = p.btn, p.btnHover, p.btnAccentHover, p.btnDangerHover
	colGuestRow, colFieldFocus = p.guestRow, p.fieldFocus
	colErrText, colWarnText, colOkText = p.errText, p.warnText, p.okText
}

func init() { applyTheme("dark") }
