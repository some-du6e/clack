#!/usr/bin/env python3
"""Slack's markup for the thread pane states docs/THREADS-AUDIT.md covers,
rendered under Slack's shipping CSS (client-boot-styles, the public CDN file)
and the light and dark design tokens Clack carries
(internal/ui/slackc/css/slackvars.go), one 415px pane column per state, so
the threads-audit screenshots of Clack's pane have Slack's to sit beside.

The markup is the gantry build's (407aff01646b, client-boot-shared):

  ThreadSeparatorRowGeneric (EWXh)   p-thread_separator_row_generic,
                                     separatorType default / unread / read
                                     (the ghost), "New" in
                                     p-threads_flexpane__{type}_divider__label
  UnreadDivider (VxYf)               c-message_list__unread_divider
                                     --unread_threads, in the thread pane
                                     p-threads_flexpane__unread_divider or,
                                     marked read, __marked_as_read_divider
                                     with --ghost
  ThreadUnreadBannerGeneric (ntqA)   p-new_threads_banner (JwHt) in
                                     p-threads_view__bottom_banners
  ReplyBar (v8ac)                    c-message__reply_bar: faces, the fifth
                                     with c-avatar__additional_overlay "+2",
                                     the count, the last reply, the caret

Nothing here talks to Slack beyond fetching that CSS and Slack's icon set; the
text is made up. Slack-Lato is the system Lato (fonts-lato); the banner's
legacy icon-font glyphs are icons.json's arrow-down and, for times-small,
close at 14px.

usage: threads_ref.py <shots dir> <slack css> <slackvars.go> <icons.json>
"""
import html
import json
import os
import re
import subprocess
import sys

shots, css_path, vars_path, icons_path = sys.argv[1:5]
icons = json.load(open(icons_path))["icons"]
E = html.escape


def tokens(name):
    src = open(vars_path).read()
    m = re.search(r"var %s = map\[string\]string\{(.*?)\n\}" % name, src, re.S)
    return dict(re.findall(r'"(--[^"]+)":\s*"([^"]*)"', m.group(1)))


def svg(name, size, style=""):
    body = icons.get(name, {}).get("body", "")
    return ('<svg viewBox="0 0 20 20" width="%d" height="%d" fill="currentColor" style="display:inline-block;%s">%s</svg>'
            % (size, size, style, body))


def separator(count, kind, ghost=False):
    cls = "p-thread_separator_row_generic" + (" p-thread_separator_row_generic--ghost" if ghost else "")
    if kind == "default":
        tail = '<hr class="p-threads_flexpane__separator_line">'
    else:
        tail = ('<hr class="p-threads_flexpane__separator_line p-threads_flexpane__separator_line__%s" aria-hidden="true">'
                '<span class="p-threads_flexpane__%s_divider__label">New</span>' % (kind, kind))
    return ('<div class="%s"><div class="p-threads_flexpane__separator"><span class="p-threads_flexpane__separator_count">%s</span>%s</div></div>'
            % (cls, E(count), tail))


def unread_divider(marked=False):
    cls = "c-message_list__unread_divider c-message_list__unread_divider--unread_threads"
    cls += " c-message_list__unread_divider--ghost p-threads_flexpane__marked_as_read_divider" if marked else " p-threads_flexpane__unread_divider"
    return ('<div class="%s"><hr class="c-message_list__unread_divider__separator" aria-hidden="true">'
            '<span class="c-message_list__unread_divider__label">New</span></div>' % cls)


def reply_row(name, text):
    return ('<div class="c-message_kit__background c-message_kit__message c-message_kit__thread_message"><div class="c-message_kit__gutter">'
            '<div class="c-message_kit__gutter__left"><span class="c-avatar" style="width:36px;height:36px;display:block;background:#8d8d8d;border-radius:var(--sk_avatar-border-radius)"></span></div>'
            '<div class="c-message_kit__gutter__right"><span class="c-message__sender c-message_kit__sender"><button class="c-link--button c-message__sender_button">%s</button></span>'
            '&nbsp;&nbsp;<a class="c-link c-timestamp"><span class="c-timestamp__label">2:46 PM</span></a><br>'
            '<div class="c-message_kit__blocks c-message_kit__blocks--rich_text"><div class="p-rich_text_section">%s</div></div></div></div></div>'
            % (E(name), E(text)))


def banner(n):
    return ('<div class="p-threads_view__bottom_banners" style="position:absolute;left:50%%;transform:translate(-50%%);bottom:8px">'
            '<div class="p-new_threads_banner p-new_threads_banner--no-padding"><button class="c-button-unstyled">%s %d new replies</button>'
            '<button class="c-button-unstyled" style="width:36px">%s</button></div></div>'
            % (svg("arrow-down", 16, "position:relative;top:2px;margin-right:4px"), n, svg("close", 14, "position:relative;top:2px")))


def reply_bar():
    faces = "".join('<span class="c-avatar c-message__reply_bar_avatar" style="width:24px;height:24px;margin-right:4px;display:inline-block;position:relative;background:#8d8d8d;border-radius:4px">%s</span>'
                    % ('<div class="c-avatar__additional_overlay">+2</div>' if i == 4 else "") for i in range(5))
    return ('<div class="c-message__reply_bar c-message_kit__thread_replies" role="presentation">%s'
            '<button class="c-link--button c-message__reply_count">7 replies</button>'
            '<div class="c-message__reply_bar_description"><span class="c-message__reply_bar_last_reply">Last reply 16 hours ago</span>'
            '<span class="c-message__reply_bar_view_thread">View thread</span></div>'
            '<div class="c-message__reply_bar_arrow">%s</div></div>' % (faces, svg("caret-right", 18)))


STATES = {
    "separator": separator("7 replies", "default") + reply_row("Robin Example", "Reply 1 to: Photos from the trip"),
    "separator-new": separator("2 replies", "unread") + reply_row("Robin Example", "Reply 1 to: Who has the projector key?"),
    "separator-ghost": separator("2 replies", "read", ghost=True) + reply_row("Robin Example", "Reply 1 to: Who has the projector key?"),
    "new-divider": reply_row("Alex Example", "Reply 3 to: Draft agenda for the offsite") + unread_divider()
    + reply_row("Robin Example", "Reply 4 to: Draft agenda for the offsite"),
    "marked-read": reply_row("Alex Example", "Reply 3 to: Draft agenda for the offsite") + unread_divider(marked=True)
    + reply_row("Robin Example", "Reply 4 to: Draft agenda for the offsite"),
    "banner": '<div style="position:relative;height:120px">%s</div>' % banner(3),
    "reply-bar": '<div class="p-message_pane"><div style="padding:8px 20px 8px 64px">%s</div></div>' % reply_bar(),
}


def page(body, dark):
    t = tokens("slackVarsDark" if dark else "slackVarsLight")
    root = ";".join("%s:%s" % kv for kv in t.items())
    return ("<!doctype html><html><head><meta charset=utf-8><link rel=stylesheet href='file://%s'>"
            "<style>:root{%s;color-scheme:%s}html,body{margin:0;background:var(--dt_color-base-pry);color:var(--dt_color-content-pry);"
            "font-family:Lato,sans-serif;font-size:15px;line-height:1.46668;-webkit-font-smoothing:antialiased}*{box-sizing:border-box}</style>"
            "</head><body class='%s'><div class='p-ia4_client'><div class='p-flexpane p-threads_flexpane' style='width:415px;position:relative;padding-top:8px'>%s</div></div></body></html>"
            % (os.path.abspath(css_path), root, "dark" if dark else "light", "sk-client-theme--dark" if dark else "", body))


for name, body in STATES.items():
    for dark in (False, True):
        mode = "dark" if dark else "light"
        hp = os.path.join(shots, "slack-ref-%s-%s.html" % (name, mode))
        open(hp, "w").write(page(body, dark))
        out = os.path.join(shots, "slack-ref-%s-%s.png" % (name, mode))
        subprocess.run(["google-chrome", "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
                        "--force-device-scale-factor=1", "--window-size=415,200", "--screenshot=" + out, "file://" + os.path.abspath(hp)],
                       check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        print("saved", os.path.basename(out))
