#!/usr/bin/env python3
"""Side-by-sides of Clack's channel and thread rows, Clean and Compact, next to
the same rows in Slack's markup under Slack's shipping CSS.

The Slack side is built from Slack's client CSS (client-boot-styles, the public
CDN file), the light and dark design tokens Clack carries
(internal/ui/slackc/css/slackvars.go) and the markup of the gantry build's
message components (build a84662d2d135, boot-shared):

  Message (NnKv)            getCompactGutter / getCompactMessageBody (dense),
                            the avatar gutter / getLightMessageBody (light)
  MessageKitGutter (R86e)   c-message_kit__gutter [--compact], __left, __right;
                            "dense" without a compact slot: __right only
  MessageIndent (lFHZ)      c-message_kit__indent under "dense"
  MessageSender (ffJE)      c-message_kit__sender--dense
  CompactBlocks (GVID)      blocks inline only when every block is rich_text
  BaseBroadcastPreamble     c-message_kit__broadcast_preamble--dense
  MessageLabels (meLQ)      c-message_kit__labels--light / --dense
  MessageTombstone (2X8J)   c-message_kit__tombstone--compact, no icon
  SystemMessage             containerCompact, no <time> under dense
  ThreadReplyGeneric (oDUT) thread replies: no compact slot, never adjacent
                            under dense
  ThreadBroadcastPreamble   (UsKL) c-message__broadcast_preamble
                            c-message_kit__broadcast_preamble__inner (margin 0),
                            in c-message__broadcast_preamble_outer under light;
                            "replied to a thread:" (…_meta) and the root's
                            SimpleMessagePreview in the ButtonLink when the
                            root has previewable text, else "replied to <a>a
                            thread</a>"

Nothing here talks to Slack beyond fetching that CSS and Slack's icon set. The
rows are the synthetic channel's (compact-ref-rows.json, written by
TestCompactShots), the pictures the ones Clack drew (fakeslack's).

Slack's virtual list positions each .c-virtual_list__item (position:
absolute) with an inline offset; here they are position: relative, which
stacks them the same way.

Two substitutions, both because headless Chrome lacks Slack's private fonts:
the bullet glyph of .p-rich_text_list__bullet (Slack v2 icon font, \\e506) is
drawn as "•", and Slack-Lato is the system Lato (fonts-lato).

Row heights: a second Chrome run of each light page (#measure) records every
row's getBoundingClientRect height into slack-row-heights.json; each is
compared with the height Clack laid the same row out at (clack-row-heights.json,
TestCompactShots), and with the checked-in heights TestRowHeightsMatchSlack
holds Clack to (internal/ui/messages/testdata/slack-row-heights.json). The
script fails when a row is more than 1px off either.

usage: compact_ref.py <shots dir> <slack css> <slackvars.go> <icons.json> [<checked-in heights>]
"""
import html
import json
import os
import re
import subprocess
import sys

from PIL import Image, ImageDraw

shots, css_path, vars_path, icons_path = sys.argv[1:5]
checked_path = sys.argv[5] if len(sys.argv) > 5 else ""
ref = json.load(open(os.path.join(shots, "compact-ref-rows.json")))
icons = json.load(open(icons_path))["icons"]


def tokens(name):
    src = open(vars_path).read()
    m = re.search(r"var %s = map\[string\]string\{(.*?)\n\}" % name, src, re.S)
    return dict(re.findall(r'"(--[^"]+)":\s*"([^"]*)"', m.group(1)))


def svg(name, size):
    body = icons.get(name, {}).get("body", "")
    return '<svg viewBox="0 0 20 20" width="%d" height="%d" fill="currentColor" style="display:block">%s</svg>' % (size, size, body)


def img(src, size, radius=None):
    style = "width:%dpx;height:%dpx;display:block" % (size, size)
    if radius:
        style += ";border-radius:%s" % radius
    return '<img src="%s" style="%s">' % (html.escape(src), style)


E = html.escape


def timestamp(t):
    return '<a class="c-link c-timestamp" data-qa="timestamp"><span class="c-timestamp__label" data-qa="timestamp_label">%s</span></a>' % E(t)


def sender(r, dense, compact_cls=False):
    cls = "c-message__sender c-message_kit__sender"
    if dense:
        cls += " c-message_kit__sender--dense"
    if compact_cls:
        cls += " p-message_pane_message__sender--compact"
    badge = ""
    if r["Bot"]:
        badge = '<span class="c-app_badge" style="margin-left:4px">APP</span>'
    status = ""
    if r.get("Status"):
        status = '<span class="c-custom_status">%s</span>' % (
            '<span class="c-emoji c-emoji__small" style="display:inline-block;vertical-align:-3px">%s</span>' % img(r["Status"], 16))
    return ('<span class="%s" data-qa="message_sender"><button class="c-link--button c-message__sender_button" '
            'data-qa="message_sender_name">%s</button>%s%s</span>' % (cls, E(r["Name"]), badge, status))


def edited(r):
    return '<span class="c-message__edited_label" data-sk="tooltip_parent"> (edited)</span>' if r["Edited"] else ""


def rich(r, inline):
    """MessageBlocks with one rich_text block (Blocks iGDn)."""
    if r["Kind"] == "list":
        inner = ('<ul data-stringify-type="unordered-list" data-indent="0" class="p-rich_text_list p-rich_text_list__bullet" data-border="0">%s</ul>'
                 % "".join('<li data-stringify-indent="0" data-stringify-border="0">%s</li>' % E(x) for x in r["List"]))
    else:
        inner = '<div class="p-rich_text_section">%s%s</div>' % (E(r["Text"]), edited(r))
    il = inline
    return ('<div class="c-message_kit__blocks c-message_kit__blocks--rich_text%s"><div class="c-message__message_blocks c-message__message_blocks--rich_text%s" data-qa="message-text">'
            '<div class="p-block_kit_renderer%s" data-qa="block-kit-renderer"><div class="p-block_kit_renderer__block_wrapper p-block_kit_renderer__block_wrapper--first">'
            '<div class="p-rich_text_block%s" dir="auto">%s</div></div></div></div></div>'
            % (" c-message_kit__blocks--inline" if il else "", " c-message__message_blocks--inline" if il else "", " p-block_kit_renderer--inline" if il else "",
               " p-rich_text_block--inline" if il else "", inner))


def section(r):
    """A Block Kit section (not rich_text: never inline)."""
    t = E(r["Text"]).replace("web 4.2.0", "<b>web 4.2.0</b>")
    return ('<div class="c-message_kit__blocks"><div class="c-message__message_blocks" data-qa="message-text"><div class="p-block_kit_renderer" data-qa="block-kit-renderer">'
            '<div class="p-block_kit_renderer__block_wrapper p-block_kit_renderer__block_wrapper--first"><div class="p-section_block"><div class="p-section_block_text_content">'
            '<div class="p-section_block__text"><span class="p-mrkdwn_element"><span dir="auto">%s</span></span></div></div></div></div></div></div></div>' % t)


def body(r, inline):
    if r["Kind"] == "bot":
        return section(r)
    return rich(r, inline)


def reactions(r):
    if not r["Reactions"]:
        return ""
    pills = "".join(
        '<button class="c-button-unstyled c-reaction%s" data-qa="reactji"><span class="c-emoji c-emoji__small c-emoji--inline">%s</span><span class="c-reaction__count">%d</span></button>'
        % (" c-reaction--reacted" if x["Self"] else "", img("emoji-%s.png" % x["Emoji"], 16), x["Count"]) for x in r["Reactions"])
    return '<div class="c-message_kit__reaction_bar c-reaction_bar" data-qa="reaction_bar">%s</div>' % pills


def reply_bar(r):
    if not r["Replies"]:
        return ""
    avs = "".join('<span class="c-avatar c-message__reply_bar_avatar" style="width:24px;height:24px;margin-right:4px;display:inline-block">%s</span>'
                  % img(a, 24, "4px") for a in r["ReplyAvatars"][:5])
    n = r["Replies"]
    return ('<div class="c-message__reply_bar c-message_kit__thread_replies" role="button">%s<button class="c-link--button c-message__reply_count" data-qa="reply_bar_count">%d %s</button>'
            '<span class="c-message__reply_bar_description"><span class="c-message__reply_bar_last_reply">Last reply today at %s</span></span></div>'
            % (avs, n, "reply" if n == 1 else "replies", E(r.get("LastReply", "9:25 AM"))))


def labels(r, dense):
    if not r["Pinned"]:
        return ""
    return ('<div class="c-message_kit__labels p-message_pane_message__message_label c-message_kit__labels--%s">'
            '<div class="c-message_kit__labels__label c-message_kit__labels--pinned"><span class="c-message_kit__labels__icon" style="display:flex;justify-content:flex-end">%s</span>'
            '<span class="c-message_kit__labels__text">Pinned by you</span></div></div>' % ("dense" if dense else "light", svg("thumb-tack-filled", 13)))


def gutter(left, right, dense, has_left=True):
    cls = "c-message_kit__gutter" + (" c-message_kit__gutter--compact" if dense else "")
    if not has_left:
        return '<div class="%s"><div role="presentation" class="c-message_kit__gutter__right" data-qa="message_content">%s</div></div>' % (cls, right)
    return ('<div class="%s"><div role="presentation" class="c-message_kit__gutter__left">%s</div>'
            '<div role="presentation" class="c-message_kit__gutter__right" data-qa="message_content">%s</div></div>' % (cls, left, right))


def avatar(r):
    return ('<span class="c-avatar c-base_icon c-message_kit__avatar" style="width:36px;height:36px;display:block">%s</span>'
            % img(r["Avatar"], 36, "var(--sk_avatar-border-radius)"))


def compact_ts(r, dense, adjacent):
    cls = "p-message_pane_message__compact_timestamp"
    if not dense:
        cls += " p-message_pane_message__compact_timestamp--light"
    if adjacent:
        cls += " p-message_pane_message__compact_timestamp--adjacent"
    t = r["Time"]
    if adjacent and not dense:
        t = t.split(" ")[0]  # meridiem: !(adjacent && light)
    return '<div class="%s">%s</div>' % (cls, timestamp(t))


def item(r, inner):
    """A .c-virtual_list__item (data-key: the row, for the height pass)."""
    return '<div class="c-virtual_list__item" role="listitem" style="position:relative" data-key="%s">%s</div>' % (E(r["Key"]), inner)


def wrap(r, inner, extra=""):
    return item(r, '<div class="c-message_kit__background p-message_pane_message__message c-message_kit__message%s" data-qa="message_container">'
                '<div class="c-message_kit__hover"><div class="c-message_kit__actions c-message_kit__actions--default">%s</div></div></div>' % (extra, inner))


def system_row(r, dense):
    icon = "user-plus"
    text = '<span class="text__nwvni">%s</span>' % E(r["Text"])
    if dense:
        body = ('<div class="c-message__body c-message__body--automated c-message__system_message"><div class="container__OsSxI containerCompact__TswCu">'
                '<div class="content__ocUJc"><div class="textRow__DEPF9">%s</div></div></div></div>' % text)
        return wrap(r, gutter(compact_ts(r, True, False), '<div class="c-message_kit__indent">%s</div>' % body, True))
    t = '<time class="timestamp__bfLC8">%s</time>' % E(r["Time"])
    body = ('<div class="c-message__body c-message__body--automated c-message__system_message"><div class="container__OsSxI">'
            '<div class="content__ocUJc"><div class="textRow__DEPF9">%s%s</div></div></div></div>' % (text, t))
    return wrap(r, gutter('<span class="systemIcon__mZbDL">%s</span>' % svg(icon, 16), body, False))


def tombstone_row(r, dense):
    text = '<div class="c-message_kit__tombstone__text%s">This message was deleted.</div>' % (" c-message_kit__tombstone__text--compact" if dense else "")
    right = '<div class="c-message_kit__tombstone__right">%s%s</div>' % (text, reply_bar(r))
    icon = "" if dense else '<div class="c-message_kit__tombstone__icon">%s</div>' % svg("trash", 20)
    cls = "c-message_kit__tombstone" + (" c-message_kit__tombstone--compact" if dense else "")
    return item(r, '<div class="c-message_kit__background p-message_pane_message__message c-message_kit__message">'
                '<div class="%s">%s%s</div></div>' % (cls, icon, right))


def broadcast_inner(r):
    """ThreadBroadcastPreamble's div (UsKL): the root's preview when it has
    previewable text (canRenderSimpleMessagePreview), else the link alone."""
    cls = "c-message__broadcast_preamble c-message_kit__broadcast_preamble__inner"
    if r.get("RootPreview"):
        return ('<div class="%s"><span data-qa="thread-broadcast-preamble-with-text" class="c-message__broadcast_preamble_meta">replied to a thread:</span>'
                '<button type="button" class="c-link--button c-message__broadcast_preamble_link">%s</button></div>' % (cls, E(r["RootPreview"])))
    return '<div class="%s">replied to <button type="button" class="c-link--button c-message__broadcast_preamble_link">a thread</button></div>' % cls


def channel_row(r, dense):
    if r["Kind"] == "system":
        return system_row(r, dense)
    if r["Kind"] == "tombstone":
        return tombstone_row(r, dense)
    head = r["Header"]
    extra = " c-message_kit__background--labels" if r["Pinned"] else ""
    tail = reactions(r) + reply_bar(r)
    if dense:
        # getCompactMessageBody: the sender and a no-break space, then
        # CompactBlocks (inline when every block is rich_text)
        inline = r["Kind"] in ("message", "list")
        pre = ""
        if head:
            if r["Kind"] == "broadcast":
                pre = ('<div class="c-message_kit__broadcast_preamble c-message_kit__broadcast_preamble--dense">%s&nbsp;%s</div>'
                       % (sender(r, True, True), broadcast_inner(r)))
                inline = False
            else:
                pre = sender(r, True, True) + "&nbsp;"
        right = '<div class="c-message_kit__indent">%s%s%s</div>' % (pre, body(r, inline), tail)
        return wrap(r, labels(r, True) + gutter(compact_ts(r, True, not head), right, True), extra)
    if head:
        pre = sender(r, False) + "&nbsp;&nbsp;" + timestamp(r["Time"]) + "<br>"
        if r["Kind"] == "broadcast":
            pre = ('<div class="c-message_kit__broadcast_preamble">%s<div class="c-message__broadcast_preamble_outer" data-qa="thread-broadcast-preamble">%s</div></div>'
                   % (pre, broadcast_inner(r)))
        left = avatar(r)
    else:
        pre, left = "", compact_ts(r, False, True)
    return wrap(r, labels(r, False) + gutter(left, pre + body(r, False) + tail, False), extra)


def thread_row(r, dense, root):
    cls = "c-message_kit__background c-message_kit__message c-message_kit__thread_message" + (" c-message_kit__thread_message--root" if root else "")
    head = r["Header"] or dense  # ThreadReply: adjacent only under "light"
    pre = ""
    if head:
        pre = sender(r, dense) + "&nbsp;&nbsp;" + timestamp(r["Time"]) + "<br>"
    if dense:
        g = gutter("", pre + body(r, False) + reactions(r), True, has_left=False)
    else:
        left = avatar(r) if head else ('<div class="p-thread_compact_gutter_generic p-thread_compact_gutter_generic--adjacent">%s</div>'
                                       % timestamp(r["Time"].split(" ")[0]))
        g = gutter(left, pre + body(r, False) + reactions(r), False)
    return item(r, '<div class="%s" data-qa="message_container"><div class="c-message_kit__hover">'
                '<div class="c-message_kit__actions c-message_kit__actions--default">%s</div></div></div>' % (cls, g))


def page(rows, dense, dark, width, thread):
    t = tokens("slackVarsDark" if dark else "slackVarsLight")
    root = ";".join("%s:%s" % kv for kv in t.items())
    if thread:
        n = len(rows) - 1
        sep = ('<div class="c-virtual_list__item" role="listitem" style="position:relative"><div class="p-threads_flexpane__separator">'
               '<span class="p-threads_flexpane__separator_count">%d %s</span><hr class="p-threads_flexpane__separator_line"></div></div>'
               % (n, "reply" if n == 1 else "replies"))
        content = "".join(thread_row(r, dense, i == 0) + (sep if i == 0 else "") for i, r in enumerate(rows))
        content = ('<div class="p-flexpane p-threads_flexpane"><div class="c-message_list c-virtual_list" role="list">%s</div></div>' % content)
    else:
        content = '<div class="p-message_pane"><div class="c-message_list c-virtual_list" role="list">%s</div></div>' % "".join(
            channel_row(r, dense) for r in rows)
    return (
        "<!doctype html><html><head><meta charset=utf-8><link rel=stylesheet href='file://%s'>"
        "<style>:root{%s;color-scheme:%s}html,body{margin:0;background:var(--dt_color-base-pry);color:var(--dt_color-content-pry);"
        "font-family:Lato,sans-serif;font-size:15px;line-height:1.46668;-webkit-font-smoothing:antialiased}"
        ".p-rich_text_list__bullet li:before{content:'\\2022'!important;font-family:Lato!important}"
        "*{box-sizing:border-box}</style></head><body class='%s'><div style='width:%dpx'>%s</div>%s</body></html>"
        % (os.path.abspath(css_path), root, "dark" if dark else "light", "sk-client-theme--dark" if dark else "", width, content, MEASURE))


# #measure: every row's laid-out height, for --dump-dom
MEASURE = ("<script>if(location.hash=='#measure'){const o={};"
           "document.querySelectorAll('[data-key]').forEach(e=>{o[e.dataset.key]=e.getBoundingClientRect().height});"
           "const p=document.createElement('pre');p.id='heights';p.textContent=JSON.stringify(o);document.body.appendChild(p)}</script>")


def measure(hp, width):
    out = subprocess.run(["google-chrome", "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
                          "--force-device-scale-factor=1", "--window-size=%d,2000" % width, "--dump-dom",
                          "file://" + os.path.abspath(hp) + "#measure"], check=True, capture_output=True, text=True).stdout
    m = re.search(r'<pre id="heights">(.*?)</pre>', out, re.S)
    return {k: round(v, 2) for k, v in json.loads(html.unescape(m.group(1))).items()}


def side(name, clack_png, rows, dense, dark, width, thread):
    clack = Image.open(os.path.join(shots, clack_png)).convert("RGB")
    hp = os.path.join(shots, "slack-%s.html" % name)
    open(hp, "w").write(page(rows, dense, dark, width, thread))
    sp = os.path.join(shots, "slack-%s.png" % name)
    subprocess.run(["google-chrome", "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
                    "--force-device-scale-factor=1", "--window-size=%d,%d" % (width, clack.height + 200),
                    "--screenshot=" + sp, "file://" + os.path.abspath(hp)], check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    slack = Image.open(sp).convert("RGB")
    gap, top = 16, 28
    out = Image.new("RGB", (clack.width + slack.width + gap, max(clack.height, slack.height) + top), (128, 128, 128))
    out.paste(clack, (0, top))
    out.paste(slack, (clack.width + gap, top))
    d = ImageDraw.Draw(out)
    what = "%s, %s" % ("compact" if dense else "clean", "dark" if dark else "light")
    d.text((8, 8), "Clack (%s)" % what, fill=(255, 255, 255))
    d.text((clack.width + gap + 8, 8), "Slack markup + shipping CSS (%s)" % what, fill=(255, 255, 255))
    out.save(os.path.join(shots, "side-%s.png" % name))
    print("saved side-%s.png" % name)
    return hp


measured = {}
for dense in (False, True):
    for dark in (False, True):
        k = "%s-%s" % ("compact" if dense else "clean", "dark" if dark else "light")
        ch = side("channel-" + k, "clack-%s.png" % k, ref["channel"], dense, dark, 1100, False)
        th = side("thread-" + k, "clack-thread-%s.png" % k, ref["thread"], dense, dark, 520, True)
        if not dark:
            layout = "compact" if dense else "clean"
            measured["channel-" + layout] = measure(ch, 1100)
            measured["thread-" + layout] = measure(th, 520)
open(os.path.join(shots, "slack-row-heights.json"), "w").write(json.dumps(measured, indent=1, sort_keys=True) + "\n")

# Clack's rows against Slack's, and the checked-in table against this run
clack = json.load(open(os.path.join(shots, "clack-row-heights.json")))
checked = json.load(open(checked_path)) if checked_path else {}
bad = []
print("%-16s %-6s %8s %8s %8s" % ("list", "row", "Slack", "Clack", "checked"))
for lst in sorted(measured):
    for key, want in measured[lst].items():
        got = clack.get(lst, {}).get(key)
        chk = checked.get(lst, {}).get(key) if checked_path else None
        flag = ""
        if got is None or abs(got - want) > 1:
            flag += " CLACK"
        if checked_path and (chk is None or abs(chk - want) > 0.5):
            flag += " TESTDATA"
        if flag:
            bad.append((lst, key))
        print("%-16s %-6s %8s %8s %8s%s" % (lst, key, want, got, chk, flag))
if bad:
    print("row heights off by more than 1px from Slack's markup (CLACK), or the checked-in table is stale (TESTDATA: copy slack-row-heights.json over it):", bad)
    sys.exit(1)
