#!/usr/bin/env python3
"""Ask the Herdr socket what the plugin sees: `tabs` or `snapshot`.

Run inside a Herdr pane (HERDR_SOCKET_PATH must be set). Standard library only.
"""

import json
import os
import socket
import sys


def call(method, params=None):
    """Send one request on its own connection, as Herdr requires."""
    path = os.environ.get("HERDR_SOCKET_PATH")
    if not path:
        sys.exit("HERDR_SOCKET_PATH is not set: run this inside a Herdr pane")
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as conn:
        conn.connect(path)
        request = {"id": "probe", "method": method, "params": params or {}}
        conn.sendall((json.dumps(request) + "\n").encode())
        reply = conn.makefile().readline()
    answer = json.loads(reply)
    if answer.get("error"):
        sys.exit(f"herdr: {answer['error']}")
    return answer["result"]


def tabs():
    snap = call("session.snapshot")["snapshot"]
    labels = {w["workspace_id"]: w.get("label") for w in snap.get("workspaces", [])}
    for tab in snap.get("tabs", []):
        print(f"[{labels.get(tab['workspace_id'])}] {tab['tab_id']:>10}  {tab.get('label')!r}")
        for pane in snap.get("panes", []):
            if pane["tab_id"] == tab["tab_id"]:
                print(f"{'':14}{pane['pane_id']:>10}  {pane.get('label')!r}  agent={pane.get('agent')}")


def snapshot():
    print(json.dumps(call("session.snapshot"), indent=2))


if __name__ == "__main__":
    commands = {"tabs": tabs, "snapshot": snapshot}
    if len(sys.argv) != 2 or sys.argv[1] not in commands:
        sys.exit(f"usage: {sys.argv[0]} {'|'.join(commands)}")
    commands[sys.argv[1]]()
