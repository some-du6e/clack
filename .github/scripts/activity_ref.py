#!/usr/bin/env python3
"""Side-by-sides of Clack's Activity rows and the same rows in Slack's markup.

The Slack side is built from Slack's shipping CSS (client-boot-styles, the
public CDN file), the light and dark design tokens Clack already carries
(internal/ui/slackc/css/slackvars.go, read from the web client) and the
markup of the gantry build's ActivityRowContentMultiLine (Detailed) and
ActivityRowContentSingleLine (Dense) with their hashed class names
(client-boot-render U5s4 / 6gcG / tPE3 / q1Zr). Nothing here talks to
Slack beyond fetching that CSS and Slack's icon set; the rows are the
synthetic feed's (activity-ref-rows.json, written by
TestActivitySideBySide), and a second set on internal/fakeslack's feed
(activity-ref-rows-actions.json, clack-rows-*.png: the read rows' marks,
the Slack Connect invite buttons and the inviter's team icon, Mark
Complete, the Slackbot and file footers) saved as side-rows-*.png.

usage: activity_ref.py <shots dir> <slack css> <slackvars.go> <icons.json>
"""
import html
import json
import os
import re
import subprocess
import sys

from PIL import Image, ImageDraw

shots, css_path, vars_path, icons_path = sys.argv[1:5]
rows = []
icons = json.load(open(icons_path))["icons"]


def tokens(name):
    src = open(vars_path).read()
    m = re.search(r"var %s = map\[string\]string\{(.*?)\n\}" % name, src, re.S)
    return dict(re.findall(r'"(--[^"]+)":\s*"([^"]*)"', m.group(1)))


def svg(name, size):
    body = icons.get(name, {}).get("body", "")
    return ('<svg viewBox="0 0 20 20" width="%d" height="%d" style="display:block">%s</svg>' % (size, size, body))


C = "activity_row_content_container__E078d"
GROUPED = "activity_row_content_container--grouped__HEFXq"
UNREAD = "activity_row_content_container--unread__JB7Ek"
READ_FILL = "activity_row_content_container--read-fill__y1Gcy"


def badge(r):
    if not r["Unread"]:
        if r.get("Mark"):
            # StatusIcon: status_icon --circle --compact (--neutral), the
            # engagement badges alike
            return ('<div class="activity_row_content__status_indicators__aLkYD activity_row_content__inline_status__Nu_EY '
                    'activity_row_content__inline_statusStandalone__CfEER"><div class="activity_row_content__status_icon__aom2I '
                    'activity_row_content__status_icon--circle__iH3ok activity_row_content__status_icon--compact__VBEFh '
                    'activity_row_content__status_icon--neutral__eCITz">%s</div></div>' % svg(r["Mark"], 12))
        return ""
    return ('<div class="activity_row_content__status_indicators__aLkYD activity_row_content__inline_status__Nu_EY '
            'activity_row_content__inline_statusStandalone__CfEER"><span class="c-badge c-badge--themed c-badge--circle '
            'activity_row_content__unread_badge__uU3VT activity_row_content__unread_badgeStandalone__NOwgY">%d</span></div>'
            % max(1, r["Count"]))


def avatar(size, radius):
    return ('<span style="display:block;width:%dpx;height:%dpx;border-radius:%dpx;background:var(--dt_color-base-ter)"></span>'
            % (size, size, radius))


def emoji_img(alt):
    # vbPY Emoji, emojiSize small, renderEmojiFallback: an <img
    # class="c-emoji c-emoji__small"> (16px); no image here, so Chrome paints
    # the alt text clipped to the box, as Slack does for an unknown emoji
    return '<img class="c-emoji c-emoji__small" alt="%s">' % html.escape(alt)


def body(r, dense):
    sub = ""
    if r["React"]:
        # QTZf ReactionContent hands its subtext to /ad9 ActivityMessage
        # with truncate 1: h08e Truncate (span.c-truncate,
        # --sk_truncate-lines 1, word-break break-all), then
        # span.p-activity_ia4_page__item__activity_subtext around the
        # subtext and the message text right after it (no space between).
        # Dense "Reacted to:" in span.margin_right_25; Detailed (layout
        # revisions) span.reaction_prefix (700, margin-right 4px) around
        # "Reacted {emoji} to:", the emoji in span.reaction_emoji (relative,
        # top 3px). With activity_inbox on, ActivityMessage's span has no
        # read/unread colour class: the text takes the row's colour.
        if dense:
            pre = '<span class="margin_right_25">Reacted to:</span>'
        else:
            pre = ('<span class="reaction_prefix__V6YJG">Reacted <span class="reaction_emoji__XTXNb">%s</span> to:</span>'
                   % emoji_img(r["Emoji"]))
        return ('<span data-qa="activity-item-message"><span class="c-truncate" style="--sk_truncate-lines:1;word-break:break-all">'
                '<span class="p-activity_ia4_page__item__activity_subtext">%s</span>%s</span></span>'
                % (pre, html.escape(r["Body"])))
    elif r["Sub"]:
        sub = html.escape(r["Sub"])
    out = sub + html.escape(r["Body"])
    if r.get("Complete") and not dense:
        # xSv9 ActivityItemMarkCompleteButton (Detailed): a small button
        # under the reminder, the check then "Mark Complete"
        out = ('<div>%s</div><button class="c-button c-button--outline c-button--small markCompleteButton__ob4yw" '
               'style="display:inline-flex;align-items:center"><span class="margin_right_25">%s</span>Mark Complete</button>'
               % (out, svg("check", 16)))
    return out


def sender(r, dense):
    name = html.escape(r["Name"])
    if r.get("TeamIcon"):
        # BaseInviteSenderName: the name, TeamIcon (16px, margin_right_25),
        # the team in Detailed
        name += ('<span style="display:inline-block;width:16px;height:16px;border-radius:4px;vertical-align:-3px;'
                 'margin:0 4px;background:var(--dt_color-base-ter)"></span>')
        if not dense and r.get("Team"):
            name += html.escape(r["Team"])
    return name


def extras(r):
    if not r.get("Invite"):
        return ""
    btn = {"Ignore": "c-button--outline", "Accept": "c-button--primary"}
    li = "".join('<li class="p-slack_connect_invite_actions__text_action"><button class="c-button %s c-button--small">%s</button></li>'
                 % (btn[b], b) for b in r["Invite"])
    return ('<div class="activity_multi_line_row_content__entry_extras___Ee3R"><ul class="p-slack_connect_invite_actions__text_actions">'
            '%s</ul></div>' % li)


def footer(r):
    if not (r["FootText"] or r["FootIcon"]):
        return ""
    icon = ""
    cls = "activity_multi_line_row_content__entry_footer__gEf6l"
    if r["FootIcon"]:
        cls += " activity_multi_line_row_content__entry_footerWithTypeIcon__PwghK"
        icon = '<span class="activity_multi_line_row_content__entry_footer_type_icon__EJAWK">%s</span>' % svg(r["FootIcon"], 16)
    text = ""
    if r["FootText"]:
        name = html.escape(r["FootName"])
        if r["FootNameIcon"]:
            name = '<span style="display:inline-block;vertical-align:-3px">%s</span> %s' % (svg(r["FootNameIcon"], 13), name)
        if name:
            name = '<span class="activity_row_content__footer_name__eOgiR">%s</span>' % name
        text = "<div>%s%s%s</div>" % (html.escape(r["FootPre"]), name, html.escape(r["FootPost"]))
    return '<div class="%s">%s%s</div>' % (cls, icon, text)


def detailed(r):
    state = UNREAD if r["Unread"] else READ_FILL
    inner = "activity_multi_line_row_content__f02c1" + (" activity_multi_line_row_contentUnread__bFP4I" if r["Unread"] else "")
    return (
        '<div class="%s activity_row_content_container--multi-line__QNKz5 activity_row_content_container--detailed__aCWfz %s %s">'
        '<div class="%s"><div class="activity_multi_line_row_content__entry_grid__gbj2P">'
        '<div class="activity_multi_line_row_content__entry_avatar__xDrPX">%s</div>'
        '<div class="activity_multi_line_row_content__entry_stack__kQwdx">'
        '<div class="activity_multi_line_row_content__entry_name__tnAap"><span class="activity_multi_line_row_content__entry_sender__wEdr_">%s</span>'
        '<span class="activity_multi_line_row_content__entry_name_timestamp__KEPR0">%s</span></div>'
        '<div class="activity_multi_line_row_content__entry_body__ei5MV">%s</div>%s%s</div>'
        '<div class="activity_multi_line_row_content__status__anisk">%s</div>'
        "</div></div></div>"
        % (C, GROUPED, state, inner, avatar(28, 6), sender(r, False), html.escape(r["Time"]), body(r, False), footer(r), extras(r), badge(r)))


def dense(r):
    state = UNREAD if r["Unread"] else READ_FILL
    inner = "activity_single_line_row_content__PA7hI" + (" activity_single_line_row_contentUnread__dkeKz" if r["Unread"] else "")
    icon = ""
    if r["TypeIcon"]:
        glyph = emoji_img(r["Emoji"]) if r["TypeIcon"] == "reaction" else svg(r["TypeIcon"], 20)
        icon = '<span class="activity_single_line_row_content__type_icon__RB9K6">%s</span>' % glyph
    loc = ""
    if r["LocOn"]:
        li = ('<span style="display:inline-block;vertical-align:-2px">%s</span> ' % svg(r["LocIcon"], 13)) if r["LocIcon"] else ""
        loc = '<div class="activity_single_line_row_content__location__zRcTb">%s%s</div>' % (li, html.escape(r["Loc"]))
    return (
        '<div class="%s activity_row_content_container--dense__zFXm8 activity_row_content_container--design-v3__ITXYB %s %s">'
        '<div class="%s"><div class="activity_single_line_row_content__row__rOaR0">'
        '<div class="activity_single_line_row_content__leading__eW5Ak"><div class="activity_single_line_row_content__user__htSFv">'
        '<div class="activity_single_line_row_content__avatar__K4lfh">%s</div>'
        '<span class="activity_single_line_row_content__sender__Hv9sZ">%s</span></div>'
        '<div class="activity_single_line_row_content__status__DsKSG">%s%s</div></div>'
        '<div class="activity_single_line_row_content__center__WBCP5"><div class="activity_single_line_row_content__message__HXQKj">%s</div>'
        '<div class="activity_single_line_row_content__meta__MHnKl"><span class="activity_single_line_row_content__timestamp__CNEsv">%s</span>%s</div></div>'
        "</div></div></div>"
        % (C, GROUPED, state, inner, avatar(20, 4), sender(r, True), badge(r), icon, body(r, True), html.escape(r.get("TimeNarrow") or r["Time"]), loc))


def page(kind, dark):
    t = tokens("slackVarsDark" if dark else "slackVarsLight")
    root = ";".join("%s:%s" % kv for kv in t.items())
    rowfn = dense if kind == "dense" else detailed
    return (
        "<!doctype html><html><head><meta charset=utf-8><link rel=stylesheet href='file://%s'>"
        "<style>:root{%s;color-scheme:%s}html,body{margin:0;background:var(--dt_color-base-pry);"
        "font-family:Lato,sans-serif;font-size:15px;line-height:1.46668;-webkit-font-smoothing:antialiased}"
        # Slack's helper classes (the webapp's helpers CSS, not in
        # client-boot-styles) the rows use
        "*{box-sizing:border-box}.margin_right_25{margin-right:4px}</style></head><body class='%s'>"
        # 5465 ActivityPage's root classes (threads_v2 always: the subtext's
        # 15px), with activity_inbox on
        "<div class='p-activity_ia4_page p-activity_ia4_page--%s p-activity_ia4_page--threads_v2 p-activity_ia4_page--activity_inbox'>"
        "<div class='p-activity_ia4_list p-activity_ia4_list--bordered_separators' style='width:1100px'>%s</div></div></body></html>"
        % (os.path.abspath(css_path), root, "dark" if dark else "light", "sk-client-theme--dark" if dark else "",
           "dark" if dark else "light", "".join(rowfn(r) for r in rows)))


for prefix, data in (("", "activity-ref-rows.json"), ("rows-", "activity-ref-rows-actions.json"), ("react-", "activity-ref-rows-react.json")):
  if not os.path.exists(os.path.join(shots, data)):
    continue
  rows = json.load(open(os.path.join(shots, data)))
  for kind in ("detailed", "dense"):
    for mode in ("light", "dark"):
        clack = Image.open(os.path.join(shots, "clack-%s%s-%s.png" % (prefix, kind, mode))).convert("RGB")
        hp = os.path.join(shots, "slack-%s%s-%s.html" % (prefix, kind, mode))
        open(hp, "w").write(page(kind, mode == "dark"))
        sp = os.path.join(shots, "slack-%s%s-%s.png" % (prefix, kind, mode))
        subprocess.run(["google-chrome", "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
                        "--force-device-scale-factor=1", "--window-size=1100,%d" % clack.height,
                        "--screenshot=" + sp, "file://" + os.path.abspath(hp)], check=True,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        slack = Image.open(sp).convert("RGB")
        gap, top = 16, 28
        out = Image.new("RGB", (clack.width + slack.width + gap, max(clack.height, slack.height) + top), (128, 128, 128))
        out.paste(clack, (0, top))
        out.paste(slack, (clack.width + gap, top))
        d = ImageDraw.Draw(out)
        d.text((8, 8), "Clack (%s, %s)" % (kind, mode), fill=(255, 255, 255))
        d.text((clack.width + gap + 8, 8), "Slack markup + shipping CSS (%s, %s)" % (kind, mode), fill=(255, 255, 255))
        if prefix == "react-":
            # the reaction rows at 2x, Clack over Slack, so the text can be
            # compared glyph by glyph
            z = 2
            c2 = clack.resize((clack.width * z, clack.height * z), Image.NEAREST)
            s2 = slack.crop((0, 0, slack.width, clack.height)).resize((slack.width * z, clack.height * z), Image.NEAREST)
            out = Image.new("RGB", (max(c2.width, s2.width), c2.height + s2.height + 3 * top), (128, 128, 128))
            d = ImageDraw.Draw(out)
            d.text((8, 8), "Clack (%s, %s), 2x" % (kind, mode), fill=(255, 255, 255))
            out.paste(c2, (0, top))
            d.text((8, c2.height + top + 8), "Slack markup + shipping CSS (%s, %s), 2x" % (kind, mode), fill=(255, 255, 255))
            out.paste(s2, (0, c2.height + 2 * top))
        out.save(os.path.join(shots, "side-%s%s-%s.png" % (prefix, kind, mode)))
        print("saved side-%s%s-%s.png" % (prefix, kind, mode))
