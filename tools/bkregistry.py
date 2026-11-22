#!/usr/bin/env python3
"""Slack's Block Kit types, for internal/richtext's coverage guard.

Reads the web client's boot-shared chunk (gantry-v2-async-client-boot-
shared.<hash>.min.js, from Slack's public CDN: the chunk names are in
github.com/IzieStratt/slack-datamine-plus build/chunks.txt, the CDN base in
its build/meta.json) and slack.ts's block and element types
(github.com/jollyroger182/slack.ts src/types/blocks/{blocks,elements}.ts),
and writes internal/richtext/testdata/slack_block_types.json:

  build        the client build (meta.json's buildNumber)
  chunk        the boot-shared chunk's file name
  renderables  every type the chunk registers a SUI renderable for
               (`type:"X",component:()=>`): blocks, elements, the rich_text
               pieces and their inline elements share one map
  data_table_cells  the cell types DataTableBlock's renderCellContent draws
  chart_types  the chart types data_visualization's isSupportedChart takes
  slack_ts     slack.ts's block types and element types

usage: python3 -B -I tools/bkregistry.py BUILD BOOT_SHARED_JS BLOCKS_TS ELEMENTS_TS > out.json

After a new bundle, run it over the new chunk: TestSlackBlockTypesHaveRenderers
then fails on every type Clack has no renderer for.
"""
import json
import os
import re
import sys

RENDERABLE = re.compile(r'type:"([a-z_]+)",component:\(\)=>')
# slack.ts: `interface XBlock extends Block<'x'>`, `Element<'x'>`, and the
# @slack/types names it re-exports
TS_GENERIC = re.compile(r"(?:Block|Element)<('[a-z_]+'(?:\s*\|\s*'[a-z_]+')*)>")
SLACK_TYPES_BLOCKS = {
    "ActionsBlock": "actions", "AlertBlock": "alert", "CardBlock": "card", "CarouselBlock": "carousel",
    "ContainerBlock": "container", "ContextActionsBlock": "context_actions", "ContextBlock": "context",
    "DataTableBlock": "data_table", "DividerBlock": "divider", "FileBlock": "file", "HeaderBlock": "header",
    "ImageBlock": "image", "InputBlock": "input", "MarkdownBlock": "markdown", "PlanBlock": "plan",
    "RichTextBlock": "rich_text", "SectionBlock": "section", "TableBlock": "table", "TaskCardBlock": "task_card",
    "VideoBlock": "video",
}
SLACK_TYPES_ELEMENTS = {
    "Button": "button", "ChannelsSelect": "channels_select", "Checkboxes": "checkboxes",
    "ConversationsSelect": "conversations_select", "Datepicker": "datepicker", "DateTimepicker": "datetimepicker",
    "EmailInput": "email_text_input", "ExternalSelect": "external_select", "FeedbackButtons": "feedback_buttons",
    "FileInput": "file_input", "IconButton": "icon_button", "ImageElement": "image",
    "MultiChannelsSelect": "multi_channels_select", "MultiConversationsSelect": "multi_conversations_select",
    "MultiExternalSelect": "multi_external_select", "MultiStaticSelect": "multi_static_select",
    "MultiUsersSelect": "multi_users_select", "NumberInput": "number_input", "Overflow": "overflow",
    "PlainTextInput": "plain_text_input", "RadioButtons": "radio_buttons", "RichTextInput": "rich_text_input",
    "StaticSelect": "static_select", "Timepicker": "timepicker", "URLInput": "url_text_input",
    "UsersSelect": "users_select", "WorkflowButton": "workflow_button",
}


def ts_types(path, names):
    src = open(path, encoding="utf-8").read()
    out = set()
    for union in TS_GENERIC.findall(src):
        out.update(re.findall(r"'([a-z_]+)'", union))
    imports = re.search(r"import type \{(.*?)\} from '@slack/types'", src, re.S)
    if imports:
        for name in re.findall(r"\b([A-Z][A-Za-z]+)\b", imports.group(1)):
            if name in names:
                out.add(names[name])
    return sorted(out)


def main():
    build, chunk, blocks_ts, elements_ts = sys.argv[1:5]
    src = open(chunk, encoding="utf-8", errors="replace").read()
    renderables = sorted(set(RENDERABLE.findall(src)))
    cells = re.search(r"function renderCellContent\(.{0,800}", src)
    cell_types = sorted(set(re.findall(r'case"([a-z_]+)"', cells.group(0)))) if cells else []
    charts = re.search(r'function isCartesianChart\(e\)\{return(.*?)\}function isSupportedChart\(e\)\{return(.*?)\}', src)
    chart_types = sorted(set(re.findall(r'"([a-z_]+)"===e\.type', charts.group(1) + charts.group(2)))) if charts else []
    out = {
        "build": build,
        "chunk": os.path.basename(chunk),
        "renderables": renderables,
        "data_table_cells": cell_types,
        "chart_types": chart_types,
        "slack_ts": {
            "blocks": ts_types(blocks_ts, SLACK_TYPES_BLOCKS),
            "elements": ts_types(elements_ts, SLACK_TYPES_ELEMENTS),
        },
    }
    json.dump(out, sys.stdout, indent=1)
    sys.stdout.write("\n")


if __name__ == "__main__":
    main()
