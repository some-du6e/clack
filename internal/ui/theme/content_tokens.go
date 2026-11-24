package theme

import "image/color"

// Content tokens the v2 palette lacks, read from slackvars.go (light,
// dark). Palette.Link and Mention are rgb(var(--sk_highlight)), #1d9bd1
// in dark mode; Slack Kit's link button (.c-link--button, KHV7
// ButtonLink: FileDetails' "Owned by" owner) is content-hgl-1, #2ba5ce.
var (
	v2ContentHgl1 = v2Tok{"var(--dt_color-content-hgl-1)", rgb(0x1264a3), rgb(0x2ba5ce)}
	v2OtlSec      = v2Tok{"var(--dt_color-otl-sec)", rgba(0x5e5d60, 0x73), rgba(0x797c81, 0x80)}
)

// ContentHgl1 is --dt_color-content-hgl-1 in t (.c-link--button's
// colour).
func ContentHgl1(t *Theme) color.NRGBA { return v2ContentHgl1.of(t) }

// OtlSec is --dt_color-otl-sec in t (.c-button--outline's border, and
// FileDetails' bordered Download, p-file_details__references_actions).
func OtlSec(t *Theme) color.NRGBA { return v2OtlSec.of(t) }

// ContentSec is --dt_color-content-sec in t (FileDetails' "Owned by" and
// "Created …" rows).
func ContentSec(t *Theme) color.NRGBA { return v2ContentSec.of(t) }

// ContentPry is --dt_color-content-pry in t.
func ContentPry(t *Theme) color.NRGBA { return v2ContentPry.of(t) }
