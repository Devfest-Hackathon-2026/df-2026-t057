"""Prepare a policy file without changing cloud IAM; preserve its etag and other members."""

import argparse
import copy
import json
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("current", type=Path)
parser.add_argument("output", type=Path)
parser.add_argument("--restore-from", type=Path)
args = parser.parse_args()

project = "devfest-2026-513816"
member = f"serviceAccount:dopagaki-runtime@{project}.iam.gserviceaccount.com"
old_roles = {
    "roles/aiplatform.user",
    "roles/datastore.user",
    "roles/secretmanager.secretAccessor",
    "roles/secretmanager.secretVersionAdder",
}
new_roles = {
    f"projects/{project}/roles/dopagakiGeminiInference",
    f"projects/{project}/roles/dopagakiDataAccess",
}
managed_roles = old_roles | new_roles
policy = json.loads(args.current.read_text())
if not policy.get("etag"):
    parser.error("the current policy must include an etag")

if args.restore_from:
    previous = json.loads(args.restore_from.read_text())
    additions = [
        dict(copy.deepcopy(b), members=[member])
        for b in previous["bindings"]
        if b["role"] in managed_roles and member in b["members"]
    ]
    if not additions:
        parser.error("the restore policy has no reviewed runtime bindings")
else:
    actual = {
        b["role"] for b in policy["bindings"]
        if member in b["members"] and b["role"] in managed_roles
    }
    if actual != old_roles:
        parser.error("runtime roles differ from the reviewed baseline; inspect the current policy first")
    root = Path(__file__).resolve().parent
    additions = [{"role": role, "members": [member]} for role in sorted(new_roles)]
    for role, condition in [
        ("roles/secretmanager.secretAccessor", "secret-access-condition.json"),
        ("roles/secretmanager.secretVersionAdder", "secret-write-condition.json"),
    ]:
        additions.append({
            "role": role, "members": [member],
            "condition": json.loads((root / condition).read_text()),
        })

bindings = []
for binding in policy["bindings"]:
    if binding["role"] in managed_roles:
        binding["members"] = [m for m in binding["members"] if m != member]
    if binding["members"]:
        bindings.append(binding)
for addition in additions:
    same = next((
        b for b in bindings
        if b["role"] == addition["role"] and b.get("condition") == addition.get("condition")
    ), None)
    if same is None:
        bindings.append(addition)
    elif member not in same["members"]:
        same["members"].append(member)
policy.update(version=3, bindings=bindings)
args.output.write_text(json.dumps(policy, indent=2) + "\n")
print(f"Prepared {args.output}; cloud IAM was not changed.")
