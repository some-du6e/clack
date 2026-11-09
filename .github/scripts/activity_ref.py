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
TestActivitySideBySide).

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
rows = json.load(open(os.path.join(shots, "activity-ref-rows.json")))
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
        return ""
    return ('<div class="activity_row_content__status_indicators__aLkYD activity_row_content__inline_status__Nu_EY '
            'activity_row_content__inline_statusStandalone__CfEER"><span class="c-badge c-badge--themed c-badge--circle '
            'activity_row_content__unread_badge__uU3VT activity_row_content__unread_badgeStandalone__NOwgY">%d</span></div>'
            % max(1, r["Count"]))


def avatar(size, radius):
    return ('<span style="display:block;width:%dpx;height:%dpx;border-radius:%dpx;background:var(--dt_color-base-ter)"></span>'
            % (size, size, radius))


def body(r, dense):
    sub = ""
    if r["React"]:
        sub = "Reacted to:" if dense else "Reacted %s to:" % html.escape(r["Emoji"])
        sub = '<span class="margin_right_25">%s</span> ' % sub
    elif r["Sub"]:
        sub = html.escape(r["Sub"])
    return sub + html.escape(r["Body"])


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
        '<div class="activity_multi_line_row_content__entry_body__ei5MV">%s</div>%s</div>'
        '<div class="activity_multi_line_row_content__status__anisk">%s</div>'
        "</div></div></div>"
        % (C, GROUPED, state, inner, avatar(28, 6), html.escape(r["Name"]), html.escape(r["Time"]), body(r, False), footer(r), badge(r)))


def dense(r):
    state = UNREAD if r["Unread"] else READ_FILL
    inner = "activity_single_line_row_content__PA7hI" + (" activity_single_line_row_contentUnread__dkeKz" if r["Unread"] else "")
    icon = ""
    if r["TypeIcon"]:
        glyph = html.escape(r["Emoji"]) if r["TypeIcon"] == "reaction" else svg(r["TypeIcon"], 20)
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
        % (C, GROUPED, state, inner, avatar(20, 4), html.escape(r["Name"]), badge(r), icon, body(r, True), html.escape(r["Time"]), loc))


def page(kind, dark):
    t = tokens("slackVarsDark" if dark else "slackVarsLight")
    root = ";".join("%s:%s" % kv for kv in t.items())
    rowfn = dense if kind == "dense" else detailed
    return (
        "<!doctype html><html><head><meta charset=utf-8><link rel=stylesheet href='file://%s'>"
        "<style>:root{%s;color-scheme:%s}html,body{margin:0;background:var(--dt_color-base-pry);"
        "font-family:Lato,sans-serif;font-size:15px;line-height:1.46668;-webkit-font-smoothing:antialiased}"
        "*{box-sizing:border-box}</style></head><body class='%s'>"
        "<div class='p-activity_ia4_list p-activity_ia4_list--bordered_separators' style='width:1100px'>%s</div></body></html>"
        % (os.path.abspath(css_path), root, "dark" if dark else "light", "sk-client-theme--dark" if dark else "",
           "".join(rowfn(r) for r in rows)))


for kind in ("detailed", "dense"):
    for mode in ("light", "dark"):
        clack = Image.open(os.path.join(shots, "clack-%s-%s.png" % (kind, mode))).convert("RGB")
        hp = os.path.join(shots, "slack-%s-%s.html" % (kind, mode))
        open(hp, "w").write(page(kind, mode == "dark"))
        sp = os.path.join(shots, "slack-%s-%s.png" % (kind, mode))
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
        out.save(os.path.join(shots, "side-%s-%s.png" % (kind, mode)))
        print("saved side-%s-%s.png" % (kind, mode))
