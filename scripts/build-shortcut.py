"""Build the two iPhone shortcuts: one records the YouTube app opening and waits to ask whether to notify, one records it closing.

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
# The open shortcut asks the server again at most this many times (each wait is at most 15 minutes).
REPEATS = 16


def new_id() -> str:
    return str(uuid.uuid4()).upper()


def text(value: str, attachments: dict | None = None) -> dict:
    return {"Value": {"string": value, "attachmentsByRange": attachments or {}}, "WFSerializationType": "WFTextTokenString"}


def output(name: str, action: str) -> dict:
    return {"OutputName": name, "OutputUUID": action, "Type": "ActionOutput"}


def attachment(value: dict) -> dict:
    return {"Value": value, "WFSerializationType": "WFTextTokenAttachment"}


def variable(name: str) -> dict:
    return {"Type": "Variable", "VariableName": name}


def action(identifier: str, **parameters) -> dict:
    return {"WFWorkflowActionIdentifier": f"is.workflow.actions.{identifier}", "WFWorkflowActionParameters": parameters}


def request(endpoint: str, key: str, body: dict | None = None) -> tuple[str, dict]:
    """POST to the shortcut API with the connection key. body maps JSON keys to variable names."""
    uid = new_id()
    parameters = {
        "UUID": uid,
        "WFURL": f"{APP_URL}/api/shortcut/{endpoint}",
        "WFHTTPMethod": "POST",
        "ShowHeaders": True,
        "WFHTTPHeaders": {"Value": {"WFDictionaryFieldValueItems": [{
            "WFItemType": 0,
            "WFKey": text("Authorization"),
            "WFValue": text(f"Shortcut {TOKEN}", {"{9, 1}": output("Text", key)}),
        }]}, "WFSerializationType": "WFDictionaryFieldValue"},
        "WFHTTPBodyType": "JSON",
    }
    if body:
        parameters["WFJSONValues"] = {"Value": {"WFDictionaryFieldValueItems": [
            {"WFItemType": 0, "WFKey": text(name), "WFValue": text(TOKEN, {"{0, 1}": variable(source)})}
            for name, source in body.items()
        ]}, "WFSerializationType": "WFDictionaryFieldValue"}
    return uid, action("downloadurl", **parameters)


def remember(response: str, key: str, name: str) -> list[dict]:
    """Take a value from the JSON response and keep it in a named variable."""
    uid = new_id()
    return [
        action("getvalueforkey", UUID=uid, WFDictionaryKey=key, WFInput=attachment(output("Contents of URL", response))),
        action("setvariable", WFVariableName=name, WFInput=attachment(output("Dictionary Value", uid))),
    ]


def when(name: str, condition: int, value: str, body: list[dict]) -> list[dict]:
    """If the named variable is (4) or is not (5) value, run body."""
    group = new_id()
    return [
        action("conditional", GroupingIdentifier=group, WFControlFlowMode=0, WFCondition=condition,
               WFConditionalActionString=value, WFInput={"Type": "Variable", "Variable": attachment(variable(name))}),
        *body,
        action("conditional", GroupingIdentifier=group, WFControlFlowMode=2),
    ]


def open_actions() -> list[dict]:
    """Record the open silently, then wait and ask the server again until it says to notify or to stop.
    Nothing is shown unless the server answers notify, so errors and rate limits stay silent."""
    key = new_id()
    opened, open_request = request("open", key)
    group = new_id()
    loop = [
        *when("action", 5, "wait", [action("exit")]),
        action("delay", WFDelayTime=attachment(variable("wait"))),
    ]
    checked, check_request = request("check", key, {"openId": "openId"})
    message, link = new_id(), new_id()
    notify = [
        action("getvalueforkey", UUID=message, WFDictionaryKey="message", WFInput=attachment(output("Contents of URL", checked))),
        action("notification", WFNotificationActionTitle=TITLE,
               WFNotificationActionBody=text(TOKEN, {"{0, 1}": output("Dictionary Value", message)}),
               WFNotificationActionSound=True),
        *remember(checked, "openApp", "openApp"),
        *when("openApp", 4, "yes", [
            action("getvalueforkey", UUID=link, WFDictionaryKey="url", WFInput=attachment(output("Contents of URL", checked))),
            action("openurl", WFInput=attachment(output("Dictionary Value", link))),
        ]),
        action("exit"),
    ]
    loop += [check_request, *remember(checked, "action", "action"), *remember(checked, "waitSeconds", "wait"),
             *when("action", 4, "notify", notify)]
    return [
        # The connection key, filled in by the import question.
        action("gettext", UUID=key, WFTextActionText=""),
        open_request,
        *remember(opened, "openId", "openId"),
        *remember(opened, "action", "action"),
        *remember(opened, "waitSeconds", "wait"),
        action("repeat.count", GroupingIdentifier=group, WFControlFlowMode=0, WFRepeatCount=REPEATS),
        *loop,
        action("repeat.count", GroupingIdentifier=group, WFControlFlowMode=2),
    ]


def close_actions() -> list[dict]:
    """Record the close. No notification."""
    key = new_id()
    _, close_request = request("close", key)
    return [action("gettext", UUID=key, WFTextActionText=""), close_request]


def shortcut(actions: list[dict], color: int) -> dict:
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
        "WFWorkflowActions": actions,
    }


OUT.mkdir(parents=True, exist_ok=True)
for name, actions, color in [
    ("DOPAGAKI", open_actions(), 4282601983),
    ("DOPAGAKI-close", close_actions(), 4271458815),
]:
    unsigned = OUT / f"{name}-unsigned.shortcut"
    signed = OUT / f"{name}.shortcut"
    unsigned.write_bytes(plistlib.dumps(shortcut(actions, color), fmt=plistlib.FMT_BINARY))
    print(f"wrote {unsigned}")
    if os.uname().sysname == "Darwin":
        subprocess.run(["shortcuts", "sign", "--mode", "anyone", "--input", str(unsigned), "--output", str(signed)], check=True)
        print(f"signed {signed}")
