package slackc

import (
	"strings"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// nativeCSS is Slack's own stylesheet, reduced to which native parts keep
// their own value for a property (css.NativeValue). In Slack's DOM a rule
// on a message row reaches the row's descendants two ways: directly, when
// a selector matches the descendant (".slick-ml-deleted *"), or by
// inheritance, where the descendant has no value of its own. A link, a
// mention, inline code or the timestamp have their own color in Slack's
// stylesheet, so a row's inherited color doesn't reach them, but a direct
// rule with enough specificity (or !important) does. These rules make the
// cascade decide that exactly as in the browser: natives answer
// Styled.Color & co. with CSS only where a plugin or user rule wins over
// them, and an inherited value stops at them.
//
// Selectors follow Slack's class names (docs/PLUGINS-components.md §2.3);
// the UA rules (b, i) are :where() so any author rule beats them, as the
// user-agent origin does. It's installed as the engine's base sheet, so it
// never makes the CSS engine non-empty (no cost while no CSS is set).
const nativeCSS = `
:where(b, strong) { font-weight: N }
:where(i, em) { font-style: N }
.c-message__sender_button { color: N; font-weight: N }
.c-timestamp { color: N }
.c-link { color: N }
.c-member_slug--link { color: N; background-color: N }
.c-inline_channel_entity { color: N; background-color: N }
.c-mrkdwn__subteam--link { color: N; background-color: N }
.c-mrkdwn__broadcast--mention { color: N; background-color: N; font-weight: N }
.c-mrkdwn__code { color: N; background-color: N }
.c-mrkdwn__pre { background-color: N }
.c-search__highlight { background-color: N }
.c-message__edited_label { color: N }
.c-message__body--tombstone { color: N }
.c-reaction { color: N; background-color: N }
.c-reaction_add { color: N; background-color: N }
.c-icon_button { color: N }
.c-message_attachment__title { font-weight: N }
.c-message_attachment__author_name, .c-message_attachment__service_name { font-weight: N }
.c-message_attachment__service_name, .c-message_attachment__footer { color: N }
.c-message__reply_count { color: N; font-weight: N }
.c-message__reply_bar_description { color: N }
.c-message_kit__labels__text { color: N }
.c-tooltip__tip { color: N; background-color: N }
.p-ia4_client .p-ia4_top_nav__right_container{flex:1 0 auto;justify-content:flex-end;align-items:center;margin-right:4px;display:flex}
`

// pluginElementCSS is Slack's own rules (client-boot-styles, 2026-11:
// cf726c4f2eaecc74) for the Slack classes plugins put on elements they
// render themselves, so a plugin's banner or button looks like Slack's:
// the context bar above the composer (HaikuWarning's and OwlMode's
// BaseContextBarV2), the reaction bar's add-reaction pill (CopyReacted's
// copy chip, button.c-button-unstyled.c-reaction_add), Slack Kit's icon
// button in the thread header (ThreadJump's Jump in thread,
// c-icon_button c-icon_button--size_medium c-icon_button--default
// p-flexpane_header__control) and the top nav's help button
// (StreamerMode's toggle, p-top_nav__button p-top_nav__help in a
// p-top_nav__windows_controls_container), and Slack's click-to-play over
// an unfurl's media with the legacy icon font's play glyph (ClickToLoad's
// placeholder: c-message_attachment__audio > __video_thumb >
// __video_buttons > button.c-message_attachment__video_play >
// i.c-deprecated-icon.c-icon--play; legacy_icons.go draws the glyph).
// It is the engine's host base
// sheet: its rules match only host-tree elements, never a native that
// carries the same class (the native add-reaction button, the thread
// pane's close button), which draws its own look. Rules are verbatim, in
// Slack's source order and cascade layers (sk.foundation, sk.components,
// sk.core, as the file first names them); only those whose subject is a
// class here.
const pluginElementCSS = `
@layer sk.foundation, sk.components, sk.core;
@layer sk.foundation{.c-button-unstyled{color:inherit;font-family:inherit;font-size:inherit;font-stretch:inherit;font-style:inherit;font-variant:inherit;font-weight:inherit;line-height:inherit;overflow:initial;text-align:initial;vertical-align:initial;cursor:pointer;background:0 0;border:0;margin:0;padding:0}.c-button-unstyled:hover,.c-button-unstyled:focus,.c-button-unstyled:active{outline:none}}
@layer sk.core{.c-icon_button{--sk_button-styles-radius:8px;border-radius:var(--sk_button-styles-radius);height:var(--sk_button-size);width:var(--sk_button-size);font-size:calc(.5 * var(--sk_button-size) + 2px);justify-content:center;align-items:center;transition-property:color,background-color,border-color;transition-duration:80ms;transition-timing-function:cubic-bezier(.36,.19,.29,1);display:inline-flex;position:relative}.c-icon_button:link{text-decoration:none}.c-icon_button--size_small{--sk_button-size:28px}.c-icon_button--size_smedium{--sk_button-size:32px}.c-icon_button--size_medium{--sk_button-size:36px}.c-icon_button--size_large{--sk_button-size:44px}.c-icon_button.c-button--disabled{opacity:.5}.c-icon_button--default,.c-icon_button--ghost,.c-icon_button--default:link,.c-icon_button--ghost:link{color:var(--dt_color-content-sec)}.c-icon_button--default:hover,.c-icon_button--ghost:hover,.c-icon_button--default:link:hover,.c-icon_button--ghost:link:hover{color:var(--dt_color-content-pry);background-color:var(--dt_color-base-ter)}.c-icon_button--default:active,.c-icon_button--ghost:active,.c-icon_button--default:link:active,.c-icon_button--ghost:link:active{background-blend-mode:var(--dt_blend-mode-interaction);background-image:linear-gradient(var(--dt_color-base-ter-pressed), var(--dt_color-base-ter-pressed))}}
.c-deprecated-icon:before{vertical-align:middle;font-family:Slack v2;font-size:20px;font-style:normal;font-weight:400;display:inline-block}
.c-deprecated-icon--inherit:before{font-size:inherit}
.c-icon--play:before{content:"\e134 "}
.c-icon_button--default.c-button--disabled,.c-icon_button--ghost.c-button--disabled,.c-icon_button--themeGhost.c-button--disabled,.c-icon_button--light.c-button--disabled,.c-icon_button--dark.c-button--disabled,.c-icon_button--outline.c-button--disabled{background-color:#0000}
.c-message_attachment__image,.c-message_attachment__thumb,.c-message_attachment__video_thumb{box-shadow:0 0 0 1px var(--dt_color-otl-ter) inset;background-position:50%;background-repeat:no-repeat;background-size:contain;border-radius:8px}
.c-message_attachment__video_thumb{justify-content:center;align-items:center;width:100%;height:100%;display:flex;overflow:hidden}
.c-message_attachment__video_buttons{text-align:center;background-color:#0006;border-radius:.7rem;width:100%;min-width:150px;max-width:225px}
.c-message_attachment__video_play{text-align:center;vertical-align:initial}
.c-message_attachment__video_play,.c-message_attachment__video_link{cursor:pointer;color:var(--dt_color-constants-white);opacity:.7;text-shadow:0 1px 1px #00000080;padding:0 10%;font-size:3.4rem;text-decoration:none;display:inline-block}
.c-message_attachment__video_play:is(:focus,:hover),.c-message_attachment__video_link:is(:focus,:hover){color:var(--dt_color-constants-white);opacity:1}
.c-message_attachment__audio{margin-top:5px}
.p-activity_ia4_page__item__reactions_bar--add_button_hidden .c-reaction_add{display:none}
.c-reaction_add,.c-reaction_expand{cursor:pointer;vertical-align:top;background-color:var(--dt_color-surf-pry);width:fit-content;height:24px;color:var(--dt_color-content-pry);border:0;border-radius:9999px;justify-content:center;align-items:center;gap:4px;margin-bottom:4px;margin-right:4px;padding-inline:8px;font-size:12px;line-height:1;display:inline-flex}
.c-reaction_add:hover,.c-reaction_expand:hover{box-shadow:0 0 0 1px var(--dt_color-otl-pry);background-color:#0000}
.c-reaction_add:focus,.c-reaction_expand:focus{border-radius:9999px}
.c-reaction_add:active,.c-reaction_expand:active{transform:scale(.94)}
.c-reaction_add{font-size:18px}
.c-message_kit__reaction_bar .c-reaction,.c-message_kit__reaction_bar .c-reaction_add{margin-bottom:4px;margin-right:4px}
.p-flexpane_header__control{cursor:pointer;flex-shrink:0}
.p-context_bar_v2{background:rgba(var(--sk_foreground_soft_solid,243, 243, 243), 1);border-top-left-radius:8px;border-top-right-radius:8px;padding:12px;display:flex;position:relative}
.p-context_bar_v2--horchata{background-color:rgb(var(--dt_color-plt-horchata-0))}
.p-context_bar_v2--teal{background:light-dark(#e7f6f3,#44ccbb1f)}
.p-context_bar_v2--yellow{background:light-dark(#e8912d1a,#e8912d1f)}
.p-context_bar_v2--highlight-1{background-color:var(--dt_color-base-hgl-1)}
.p-context_bar_v2__filler{background:inherit;width:100%;height:6px;position:absolute;bottom:-6px;left:0}
.p-context_bar_v2__icon{min-width:16px;height:16px;color:rgba(var(--sk_foreground_max_solid,97, 96, 97), 1);flex-shrink:0;justify-content:center;align-self:flex-start;align-items:center;margin-top:1px;margin-right:12px;font-size:16px;display:flex}
.p-context_bar_v2__content{font-size:13px;line-height:1.38463;font-weight:initial;letter-spacing:var(--custom-font-tracking-caption,inherit);flex:auto}
.p-ia4_client .p-top_nav__button.p-top_nav__help:hover{background-color:var(--dt_color-theme-surf-inv-pry)}
.p-ia4_client .p-top_nav__button.p-top_nav__help:active{background-color:var(--dt_color-theme-surf-inv-sec)}
.p-top_nav__button{width:26px;height:26px;color:var(--dt_color-theme-content-inv-pry);opacity:1;border-radius:4px;flex-shrink:0;justify-content:center;align-items:center;display:flex}
.p-top_nav__button:hover{color:var(--dt_color-theme-content-inv-pry);background-color:var(--dt_color-base-sec);opacity:1}
.p-top_nav__windows_controls_container{position:relative}
`

func init() {
	Styles.SetBase(strings.ReplaceAll(nativeCSS, ": N", ": "+css.NativeValue) + NativeTransitionsCSS)
	Styles.SetHostBase(pluginElementCSS)
}

// stripNative drops the base sheet's NativeValue winners from a host
// node's cascade: a plugin's own element with a Slack class has no native
// look to keep, so for it they are as if absent (the property inherits or
// takes its initial value, as before, and UA defaults still apply).
func stripNative(c *css.Cascaded) {
	n := 0
	for i := range c.Decls {
		if c.Decls[i].Value == css.NativeValue {
			n++
		}
	}
	if n == 0 {
		return
	}
	out := make(css.Decls, 0, len(c.Decls)-n)
	for _, d := range c.Decls {
		if d.Value != css.NativeValue {
			out = append(out, d)
		}
	}
	c.Decls = out
}
