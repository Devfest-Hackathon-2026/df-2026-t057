"""Build the two iPhone shortcuts that let DOPAGAKI's companion speak when the YouTube app opens and closes.

Usage: APP_URL=https://... python3 -I scripts/build-shortcut.py
Writes dist/shortcut/DOPAGAKI.shortcut (open) and DOPAGAKI-close.shortcut (close), unsigned and, on macOS,
signed copies for sharing. The user's connection key is asked once when each shortcut is added; it is never
stored here. The "when YouTube opens / closes" automations cannot be shipped in a file: each user adds them.
"""

import os
import plistlib
import subprocess
import uuid
from pathlib import Path

APP_URL = os.environ.get("APP_URL", "https://dopagaki-1032251723022.asia-northeast1.run.app").rstrip("/")
OUT = Path("dist/shortcut")
TOKEN = "￼"
TITLE = "相棒"


def new_id() -> str:
    return str(uuid.uuid4()).upper()


def text(value: str, attachments: dict | None = None) -> dict:
    return {"Value": {"string": value, "attachmentsByRange": attachments or {}}, "WFSerializationType": "WFTextTokenString"}


def output(name: str, action: str) -> dict:
    return {"OutputName": name, "OutputUUID": action, "Type": "ActionOutput"}


def attachment(name: str, action: str) -> dict:
    return {"Value": output(name, action), "WFSerializationType": "WFTextTokenAttachment"}


def value_for_key(key: str, request: str) -> tuple[str, dict]:
    action = new_id()
    return action, {"WFWorkflowActionIdentifier": "is.workflow.actions.getvalueforkey",
                    "WFWorkflowActionParameters": {"UUID": action, "WFDictionaryKey": key,
                                                   "WFInput": attachment("Contents of URL", request)}}


def actions_for(endpoint: str, open_when_stopped: bool) -> list[dict]:
    key, request = new_id(), new_id()
    message, show_message = value_for_key("message", request)
    actions = [
        # The connection key, filled in by the import question.
        {"WFWorkflowActionIdentifier": "is.workflow.actions.gettext",
         "WFWorkflowActionParameters": {"UUID": key, "WFTextActionText": ""}},
        {"WFWorkflowActionIdentifier": "is.workflow.actions.downloadurl",
         "WFWorkflowActionParameters": {
             "UUID": request,
             "WFURL": f"{APP_URL}/api/shortcut/{endpoint}",
             "WFHTTPMethod": "POST",
             "ShowHeaders": True,
             "WFHTTPHeaders": {"Value": {"WFDictionaryFieldValueItems": [{
                 "WFItemType": 0,
                 "WFKey": text("Authorization"),
                 "WFValue": text(f"Shortcut {TOKEN}", {"{9, 1}": output("Text", key)}),
             }]}, "WFSerializationType": "WFDictionaryFieldValue"},
             "WFHTTPBodyType": "JSON",
         }},
        show_message,
        {"WFWorkflowActionIdentifier": "is.workflow.actions.notification",
         "WFWorkflowActionParameters": {
             "WFNotificationActionTitle": TITLE,
             "WFNotificationActionBody": text(TOKEN, {"{0, 1}": output("Dictionary Value", message)}),
             "WFNotificationActionSound": True,
         }},
    ]
    if open_when_stopped:
        # With no viewing time left, open DOPAGAKI so YouTube steps back. It does not block the app.
        stop, get_stop = value_for_key("stop", request)
        link, get_link = value_for_key("url", request)
        group = new_id()
        actions += [
            get_stop,
            {"WFWorkflowActionIdentifier": "is.workflow.actions.conditional",
             "WFWorkflowActionParameters": {
                 "GroupingIdentifier": group, "WFControlFlowMode": 0, "WFCondition": 4,
                 "WFConditionalActionString": "yes",
                 "WFInput": {"Type": "Variable", "Variable": attachment("Dictionary Value", stop)},
             }},
            get_link,
            {"WFWorkflowActionIdentifier": "is.workflow.actions.openurl",
             "WFWorkflowActionParameters": {"WFInput": attachment("Dictionary Value", link)}},
            {"WFWorkflowActionIdentifier": "is.workflow.actions.conditional",
             "WFWorkflowActionParameters": {"GroupingIdentifier": group, "WFControlFlowMode": 2}},
        ]
    return actions


def shortcut(endpoint: str, open_when_stopped: bool, color: int) -> dict:
    return {
        "WFWorkflowClientVersion": "2302.0.4",
        "WFWorkflowMinimumClientVersion": 900,
        "WFWorkflowMinimumClientVersionString": "900",
        "WFWorkflowIcon": {"WFWorkflowIconStartColor": color, "WFWorkflowIconGlyphNumber": 59511},
        "WFWorkflowTypes": [],
        "WFWorkflowInputContentItemClasses": [],
        "WFWorkflowOutputContentItemClasses": [],
        "WFWorkflowHasShortcutInputVariables": False,
        "WFWorkflowImportQuestions": [{
            "ActionIndex": 0,
            "Category": "Parameter",
            "DefaultValue": "",
            "ParameterKey": "WFTextActionText",
            "Text": "DOPAGAKIの設定 →「iPhoneのショートカット」で発行した接続キーを貼り付けてください",
        }],
        "WFWorkflowActions": actions_for(endpoint, open_when_stopped),
    }


OUT.mkdir(parents=True, exist_ok=True)
for name, endpoint, open_when_stopped, color in [
    ("DOPAGAKI", "open", True, 4282601983),
    ("DOPAGAKI-close", "close", False, 4271458815),
]:
    unsigned = OUT / f"{name}-unsigned.shortcut"
    signed = OUT / f"{name}.shortcut"
    unsigned.write_bytes(plistlib.dumps(shortcut(endpoint, open_when_stopped, color), fmt=plistlib.FMT_BINARY))
    print(f"wrote {unsigned}")
    if os.uname().sysname == "Darwin":
        subprocess.run(["shortcuts", "sign", "--mode", "anyone", "--input", str(unsigned), "--output", str(signed)], check=True)
        print(f"signed {signed}")
