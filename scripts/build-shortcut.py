"""Build the iPhone shortcut that asks DOPAGAKI's companion for a remark when the YouTube app opens.

Usage: APP_URL=https://... python3 scripts/build-shortcut.py
Writes dist/shortcut/DOPAGAKI.shortcut (unsigned) and, on macOS, a signed copy for sharing.
The user's connection key is asked once when the shortcut is added; it is never stored here.
The "when YouTube opens" automation cannot be shipped in a file: each user adds it in the Shortcuts app.
"""

import os
import plistlib
import subprocess
import uuid
from pathlib import Path

APP_URL = os.environ.get("APP_URL", "https://dopagaki-1032251723022.asia-northeast1.run.app").rstrip("/")
OUT = Path("dist/shortcut")
KEY, REQUEST, MESSAGE = (str(uuid.uuid4()).upper() for _ in range(3))
TOKEN = "￼"


def text(value: str, attachments: dict | None = None) -> dict:
    return {"Value": {"string": value, "attachmentsByRange": attachments or {}}, "WFSerializationType": "WFTextTokenString"}


def output(name: str, action: str) -> dict:
    return {"OutputName": name, "OutputUUID": action, "Type": "ActionOutput"}


actions = [
    # The connection key, filled in by the import question below.
    {"WFWorkflowActionIdentifier": "is.workflow.actions.gettext",
     "WFWorkflowActionParameters": {"UUID": KEY, "WFTextActionText": ""}},
    {"WFWorkflowActionIdentifier": "is.workflow.actions.downloadurl",
     "WFWorkflowActionParameters": {
         "UUID": REQUEST,
         "WFURL": f"{APP_URL}/api/shortcut/open",
         "WFHTTPMethod": "POST",
         "ShowHeaders": True,
         "WFHTTPHeaders": {"Value": {"WFDictionaryFieldValueItems": [{
             "WFItemType": 0,
             "WFKey": text("Authorization"),
             "WFValue": text(f"Shortcut {TOKEN}", {"{9, 1}": output("Text", KEY)}),
         }]}, "WFSerializationType": "WFDictionaryFieldValue"},
         "WFHTTPBodyType": "JSON",
     }},
    {"WFWorkflowActionIdentifier": "is.workflow.actions.getvalueforkey",
     "WFWorkflowActionParameters": {
         "UUID": MESSAGE,
         "WFDictionaryKey": "message",
         "WFInput": {"Value": output("Contents of URL", REQUEST), "WFSerializationType": "WFTextTokenAttachment"},
     }},
    {"WFWorkflowActionIdentifier": "is.workflow.actions.notification",
     "WFWorkflowActionParameters": {
         "WFNotificationActionTitle": "DOPAGAKI · 相棒",
         "WFNotificationActionBody": text(TOKEN, {"{0, 1}": output("Dictionary Value", MESSAGE)}),
         "WFNotificationActionSound": True,
     }},
]

shortcut = {
    "WFWorkflowClientVersion": "2302.0.4",
    "WFWorkflowMinimumClientVersion": 900,
    "WFWorkflowMinimumClientVersionString": "900",
    "WFWorkflowIcon": {"WFWorkflowIconStartColor": 4282601983, "WFWorkflowIconGlyphNumber": 59511},
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
    "WFWorkflowActions": actions,
}

OUT.mkdir(parents=True, exist_ok=True)
unsigned = OUT / "DOPAGAKI-unsigned.shortcut"
signed = OUT / "DOPAGAKI.shortcut"
unsigned.write_bytes(plistlib.dumps(shortcut, fmt=plistlib.FMT_BINARY))
print(f"wrote {unsigned}")
if os.uname().sysname == "Darwin":
    subprocess.run(["shortcuts", "sign", "--mode", "anyone", "--input", str(unsigned), "--output", str(signed)], check=True)
    print(f"signed {signed}")
